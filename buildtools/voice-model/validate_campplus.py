"""用 3D-Speaker 正式 FBank 链路验证 CAM++ ONNX 与官方示例音频。"""

from __future__ import annotations

import argparse
import hashlib
import json
import time
from pathlib import Path

import numpy as np
import onnxruntime as ort
import torch
import torchaudio
import torchaudio.compliance.kaldi as kaldi


EXPECTED_WAV_HASHES = {
    "speaker1_a_cn_16k.wav": "5f20ce0ddc378ca3239d3ce864b1142726a46a1221ae553912e4e142045df58b",
    "speaker1_b_cn_16k.wav": "20745dc08a4281894d146140b99b9ef7417ac681119b7f7202f553cdf1a85f65",
    "speaker2_a_cn_16k.wav": "8a6cffa452df32ef10503f7992f22ffcdd7f16c4e0273d13311bc5cdcb13abf4",
}


def parse_args() -> argparse.Namespace:
    """解析 ONNX 和三个已锁定官方示例 WAV。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--wav-dir", type=Path, required=True)
    parser.add_argument("--runs", type=int, default=20)
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    """计算验证输入文件 SHA-256。"""
    return hashlib.sha256(path.read_bytes()).hexdigest()


def extract_feature(path: Path) -> np.ndarray:
    """按 3D-Speaker infer_sv.py 的正式参数提取并均值归一化 FBank。"""
    expected_hash = EXPECTED_WAV_HASHES.get(path.name)
    if expected_hash is None or sha256_file(path) != expected_hash:
        raise ValueError(f"官方示例 WAV 身份不匹配：{path.name}")
    waveform, sample_rate = torchaudio.load(path)
    if sample_rate != 16000 or waveform.shape[0] != 1:
        raise ValueError(f"官方示例 WAV 格式不正确：{path.name}")
    feature = kaldi.fbank(
        waveform,
        num_mel_bins=80,
        sample_frequency=16000,
        dither=0,
    )
    feature = feature - feature.mean(0, keepdim=True)
    return feature.unsqueeze(0).numpy()


def cosine(left: np.ndarray, right: np.ndarray) -> float:
    """计算两个一维 embedding 的余弦相似度。"""
    return float(np.dot(left, right) / (np.linalg.norm(left) * np.linalg.norm(right)))


def main() -> None:
    """执行重复推理并输出同人、异人、确定性和耗时证据。"""
    args = parse_args()
    if args.runs < 2:
        raise ValueError("重复次数至少为 2")
    session = ort.InferenceSession(str(args.model), providers=["CPUExecutionProvider"])
    features = {
        name: extract_feature(args.wav_dir / name)
        for name in EXPECTED_WAV_HASHES
    }
    embeddings: dict[str, np.ndarray] = {}
    latencies: list[float] = []
    repeats: list[np.ndarray] = []
    for name, feature in features.items():
        start = time.perf_counter()
        embeddings[name] = session.run(["embedding"], {"feature": feature})[0][0]
        latencies.append((time.perf_counter() - start) * 1000)
    for _ in range(args.runs):
        start = time.perf_counter()
        value = session.run(
            ["embedding"], {"feature": features["speaker1_a_cn_16k.wav"]}
        )[0][0]
        latencies.append((time.perf_counter() - start) * 1000)
        repeats.append(value)
    baseline = repeats[0]
    result = {
        "same_speaker_cosine": cosine(
            embeddings["speaker1_a_cn_16k.wav"], embeddings["speaker1_b_cn_16k.wav"]
        ),
        "different_speaker_cosine": cosine(
            embeddings["speaker1_a_cn_16k.wav"], embeddings["speaker2_a_cn_16k.wav"]
        ),
        "repeat_min_cosine": min(cosine(baseline, value) for value in repeats[1:]),
        "embedding_norms": {
            name: float(np.linalg.norm(value)) for name, value in embeddings.items()
        },
        "latency_ms": {
            "median": float(np.median(latencies)),
            "maximum": float(np.max(latencies)),
        },
        "runs": args.runs,
    }
    print(json.dumps(result, ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    main()
