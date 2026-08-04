# 火山实时 ASR 协议 fixture

本目录只保存脱敏且可提交的协议形状，不保存密钥、真实 Header、PCM 或用户转写正文。

- `legacy_final_response.json`：依据火山官方“大模型流式语音识别 API”响应字段整理，文本与标识均为测试值；
- 二进制 Header、sequence、payload size 与 gzip 由契约测试按官方 Seed V1 格式现场编码；
- 2026-08-04 已验证新版实时 WebSocket 使用 `X-Api-Key`；自动化握手测试只使用脱敏本地
  fixture，不保存真实 APP Key、请求 Header 或完整 provider log ID。

来源：

- https://www.volcengine.com/docs/6561/1354869
- https://www.volcengine.com/docs/6561/1631584
- https://github.com/volcengine/ai-app-lab/tree/main/demohouse/live_voice_call
