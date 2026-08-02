"""生成仅包含四个受控文件的可复现 MeetSieve CAM++ 官方模型包。"""

from __future__ import annotations

import argparse
import hashlib
import json
import zipfile
from pathlib import Path


MODEL_ID = "iic/speech_campplus_sv_zh-cn_16k-common"
MODEL_VERSION = "1.0.0-ms1"
MODEL_SHA256 = "57f6b2439b06fc453ed36159a44b97693610fb0a67c0dafd696d54e24d2b1ae1"
MODEL_SIZE = 28243826
CHECKPOINT_SHA256 = "3388cf5fd3493c9ac9c69851d8e7a8badcfb4f3dc631020c4961371646d5ada8"
SOURCE_REVISION = "065629c313eaf1a01c65c640c46d77e61e9607b4"
LICENSE_SHA256 = "c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4"
ZIP_TIMESTAMP = (2026, 8, 1, 0, 0, 0)


def parse_args() -> argparse.Namespace:
    """解析已验证 ONNX、上游许可证和目标 ZIP 路径。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", type=Path, required=True)
    parser.add_argument("--license", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    return parser.parse_args()


def sha256_bytes(content: bytes) -> str:
    """计算内存内容 SHA-256。"""
    return hashlib.sha256(content).hexdigest()


def read_verified(path: Path, expected_size: int | None, expected_sha: str) -> bytes:
    """读取并校验包输入，拒绝未经门禁的模型或许可证。"""
    content = path.read_bytes()
    if expected_size is not None and len(content) != expected_size:
        raise ValueError(f"{path.name} 大小不匹配")
    actual_sha = sha256_bytes(content)
    if actual_sha != expected_sha:
        raise ValueError(f"{path.name} SHA-256 不匹配：{actual_sha}")
    return content


def build_manifest() -> bytes:
    """生成运行时与离线导入共同校验的固定模型清单。"""
    manifest = {
        "schema_version": 1,
        "model_id": MODEL_ID,
        "model_version": MODEL_VERSION,
        "upstream_model_version": "v1.0.0",
        "upstream_source_revision": SOURCE_REVISION,
        "upstream_checkpoint_sha256": CHECKPOINT_SHA256,
        "model_file": "model.onnx",
        "model_sha256": MODEL_SHA256,
        "model_size_bytes": MODEL_SIZE,
        "license": "Apache-2.0",
        "license_file": "LICENSE",
        "notice_file": "NOTICE",
        "packaged_at": "2026-08-01",
        "opset": 11,
        "input": {"name": "feature", "dtype": "float32", "shape": ["batch", "frames", 80]},
        "output": {"name": "embedding", "dtype": "float32", "shape": ["batch", 192]},
        "audio": {
            "sample_rate": 16000,
            "feature": "fbank",
            "mel_bins": 80,
            "frame_length_ms": 25,
            "frame_shift_ms": 10,
            "dither": 0,
            "use_power": True,
            "pre_emphasis": 0.97,
            "window_type": "povey",
            "snip_edges": True,
            "low_frequency_hz": 20,
            "high_frequency_hz": 0,
            "mean_normalization": True,
        },
    }
    return (json.dumps(manifest, ensure_ascii=False, indent=2) + "\n").encode()


def build_notice() -> bytes:
    """生成模型来源及 MeetSieve 转换说明。"""
    return (
        "MeetSieve CAM++ voice model package\n\n"
        "This package contains a converted form of the CAM++ speaker embedding model:\n"
        f"- Model: {MODEL_ID}\n"
        "- Upstream model revision: v1.0.0\n"
        f"- 3D-Speaker source revision: {SOURCE_REVISION}\n"
        f"- Upstream checkpoint SHA-256: {CHECKPOINT_SHA256}\n\n"
        "MeetSieve converted the PyTorch checkpoint to ONNX opset 11 and added model metadata.\n"
        "The upstream model and 3D-Speaker source are identified as Apache License 2.0.\n"
    ).encode()


def write_entry(archive: zipfile.ZipFile, name: str, content: bytes) -> None:
    """使用固定时间、权限和压缩参数写入一个可复现 ZIP 条目。"""
    info = zipfile.ZipInfo(name, ZIP_TIMESTAMP)
    info.compress_type = zipfile.ZIP_DEFLATED
    info.external_attr = 0o100644 << 16
    archive.writestr(info, content, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)


def main() -> None:
    """校验输入并按固定顺序生成官方模型包。"""
    args = parse_args()
    model = read_verified(args.model, MODEL_SIZE, MODEL_SHA256)
    license_content = read_verified(args.license, None, LICENSE_SHA256)
    entries = {
        "LICENSE": license_content,
        "NOTICE": build_notice(),
        "manifest.json": build_manifest(),
        "model.onnx": model,
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(args.output, "w") as archive:
        for name in sorted(entries):
            write_entry(archive, name, entries[name])
    print(
        json.dumps(
            {
                "package_sha256": sha256_bytes(args.output.read_bytes()),
                "package_size_bytes": args.output.stat().st_size,
            },
            sort_keys=True,
        )
    )


if __name__ == "__main__":
    main()
