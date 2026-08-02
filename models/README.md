# 声纹模型

安装包不携带声纹模型，只包含平台 ONNX Runtime 1.26.0 和模型目录/校验逻辑。

第一期固定模型为 `iic/speech_campplus_sv_zh-cn_16k-common`，官方包版本
`1.0.0-ms1`。模型 ID、输入格式、embedding 维度、许可证、SHA-256 和真实效果验证见
`docs/spec/Step2-模型选型记录.md`。

本目录始终不存放模型二进制。不得加入占位模型、手工替换的 ONNX 或编造阈值；设置页下载
与离线导入只接受 `third_party/assets.lock.json` 锁定的同一官方包。
