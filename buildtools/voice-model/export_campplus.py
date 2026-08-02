"""将固定的 CAM++ checkpoint 导出为 MeetSieve 官方 ONNX 模型。"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import subprocess
import sys
from pathlib import Path

import numpy as np
import onnx
import onnxruntime as ort
import torch


MODEL_ID = "iic/speech_campplus_sv_zh-cn_16k-common"
MODEL_VERSION = "1.0.0-ms1"
UPSTREAM_MODEL_VERSION = "v1.0.0"
UPSTREAM_CHECKPOINT_SHA256 = (
    "3388cf5fd3493c9ac9c69851d8e7a8badcfb4f3dc631020c4961371646d5ada8"
)
UPSTREAM_SOURCE_REVISION = "065629c313eaf1a01c65c640c46d77e61e9607b4"
EMBEDDING_DIMENSION = 192


def parse_args() -> argparse.Namespace:
    """解析导出所需的固定上游源码、checkpoint 和目标路径。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--upstream-source", type=Path, required=True)
    parser.add_argument("--checkpoint", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    """分块计算文件 SHA-256，避免把模型整体读入内存。"""
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def verify_inputs(source: Path, checkpoint: Path) -> None:
    """校验上游源码 revision 标记和 checkpoint 哈希。"""
    model_source = source / "speakerlab/models/campplus/DTDNN.py"
    if not model_source.is_file():
        raise ValueError(f"上游源码缺少 CAM++ 定义：{model_source}")
    revision = subprocess.run(
        ["git", "-C", str(source), "rev-parse", "HEAD"],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()
    if revision != UPSTREAM_SOURCE_REVISION:
        raise ValueError(
            "上游源码 revision 不匹配："
            f"expected={UPSTREAM_SOURCE_REVISION}, actual={revision}"
        )
    actual_checkpoint_sha = sha256_file(checkpoint)
    if actual_checkpoint_sha != UPSTREAM_CHECKPOINT_SHA256:
        raise ValueError(
            "checkpoint SHA-256 不匹配："
            f"expected={UPSTREAM_CHECKPOINT_SHA256}, actual={actual_checkpoint_sha}"
        )


def load_model(source: Path, checkpoint: Path) -> torch.nn.Module:
    """从固定上游实现加载 192 维 CAM++ 模型。"""
    sys.path.insert(0, str(source))
    from speakerlab.models.campplus.DTDNN import CAMPPlus  # pylint: disable=import-outside-toplevel

    model = CAMPPlus(feat_dim=80, embedding_size=EMBEDDING_DIMENSION)
    state = torch.load(checkpoint, map_location="cpu", weights_only=False)
    model.load_state_dict(state)
    model.eval()
    return model


def export_model(model: torch.nn.Module, output: Path) -> torch.Tensor:
    """按 3D-Speaker 官方动态帧长契约导出 ONNX。"""
    torch.manual_seed(20260801)
    example = torch.randn(1, 345, 80)
    output.parent.mkdir(parents=True, exist_ok=True)
    torch.onnx.export(
        model,
        example,
        output,
        export_params=True,
        opset_version=11,
        do_constant_folding=True,
        input_names=["feature"],
        output_names=["embedding"],
        dynamic_axes={
            "feature": {0: "batch_size", 1: "frame_num"},
            "embedding": {0: "batch_size"},
        },
    )
    return example


def add_model_metadata(output: Path) -> None:
    """写入运行时可核验的稳定模型与特征契约。"""
    model = onnx.load(output)
    metadata = {
        "meetsieve.model_id": MODEL_ID,
        "meetsieve.model_version": MODEL_VERSION,
        "meetsieve.upstream_model_version": UPSTREAM_MODEL_VERSION,
        "meetsieve.embedding_dimension": str(EMBEDDING_DIMENSION),
        "meetsieve.sample_rate": "16000",
        "meetsieve.feature": "fbank",
        "meetsieve.feature_bins": "80",
        "meetsieve.frame_length_ms": "25",
        "meetsieve.frame_shift_ms": "10",
        "meetsieve.dither": "0",
        "meetsieve.use_power": "true",
        "meetsieve.pre_emphasis": "0.97",
        "meetsieve.window_type": "povey",
        "meetsieve.snip_edges": "true",
        "meetsieve.low_frequency_hz": "20",
        "meetsieve.high_frequency_hz": "0",
        "meetsieve.mean_normalization": "true",
        "meetsieve.upstream_checkpoint_sha256": UPSTREAM_CHECKPOINT_SHA256,
        "meetsieve.upstream_source_revision": UPSTREAM_SOURCE_REVISION,
    }
    del model.metadata_props[:]
    for key, value in sorted(metadata.items()):
        item = model.metadata_props.add()
        item.key = key
        item.value = value
    onnx.save(model, output)


def verify_export(model: torch.nn.Module, output: Path, example: torch.Tensor) -> float:
    """校验 ONNX 结构、输出维度和 PyTorch/ORT 数值一致性。"""
    onnx.checker.check_model(onnx.load(output))
    with torch.no_grad():
        expected = model(example).numpy()
    session = ort.InferenceSession(str(output), providers=["CPUExecutionProvider"])
    actual = session.run(["embedding"], {"feature": example.numpy()})[0]
    if actual.shape != (1, EMBEDDING_DIMENSION):
        raise ValueError(f"ONNX 输出 shape 不正确：{actual.shape}")
    if not np.isfinite(actual).all():
        raise ValueError("ONNX 输出包含 NaN 或 Inf")
    cosine = float(
        np.dot(expected[0], actual[0])
        / (np.linalg.norm(expected[0]) * np.linalg.norm(actual[0]))
    )
    if not math.isfinite(cosine) or cosine < 0.99999:
        raise ValueError(f"PyTorch/ORT 输出不一致：cosine={cosine}")
    return cosine


def main() -> None:
    """执行输入校验、ONNX 导出和数值验收，并打印机器可读结果。"""
    args = parse_args()
    verify_inputs(args.upstream_source, args.checkpoint)
    model = load_model(args.upstream_source, args.checkpoint)
    example = export_model(model, args.output)
    add_model_metadata(args.output)
    cosine = verify_export(model, args.output, example)
    print(
        json.dumps(
            {
                "model_sha256": sha256_file(args.output),
                "model_size_bytes": args.output.stat().st_size,
                "pytorch_ort_cosine": cosine,
            },
            ensure_ascii=False,
            sort_keys=True,
        )
    )


if __name__ == "__main__":
    main()
