# Windows amd64 交叉构建

Step 0 使用固定 `golang:1.25.9-bookworm` 基础镜像，在 Linux amd64 容器内安装
MinGW-w64、NSIS 和 Wails v2.13.0。macOS arm64 通过 Docker 的 amd64 模拟执行
同一构建链。

稳定入口：

```text
make build-windows-amd64
make package-windows
make verify-package
```

构建会把已由 `third_party/assets.lock.json` 校验的 `onnxruntime.dll` 和许可证加入
安装程序。静态校验只证明 PE 架构、GUI subsystem、CGO 依赖和 NSIS 结构正确，
不等价于 Windows 实机运行验证。
