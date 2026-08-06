# 声纹匹配校准

正式声纹档案不能由单人录入样本或未标注会议录音生成。校准集至少包含两位人工确认的匿名说话人；每人至少一段录入音频、两段评估音频，并且录入和评估必须来自不同会话。同一音频即使改名，也会因规范化 WAV 摘要重复而被拒绝。

复制 `voice-manifest.example.json` 后补齐真实 WAV 和显式候选阈值。示例中的零值只是未填写占位，不是推荐阈值。音频路径相对清单文件解析，`calibration_record` 相对仓库根目录解析。

```sh
make calibrate-voice \
  VOICE_CALIBRATION_MANIFEST=/absolute/path/to/manifest.json \
  VOICE_MODEL_PATH=/absolute/path/to/model.onnx \
  VOICE_RUNTIME_PATH=/absolute/path/to/libonnxruntime.dylib
```

工具调用与应用相同的 CAM++ encoder、成员 matcher 和匿名 clusterer。每段评估音频还会移除其真实成员后再跑一次，用于验证“不在候选成员中的人”不会被误认。只有评估样本全部正确识别、out-of-set 测试零误认，且匿名聚类没有误合并和误拆分时，才会写入：

- `models/voice-matching-profile.json`：运行时正式档案；
- 清单指定的 Markdown：模型、阈值、指标和规范化音频 SHA-256 审计记录。

`make build`、`make build-windows-amd64` 和 `make package-windows` 都会验证档案与锁定模型完全匹配，并确认校准记录存在。开发模式允许档案缺失，但会明确显示“缺少校准档案”，不会自动关联成员。
