# MeetSieve Step 10 验证记录

<!-- markdownlint-disable MD013 -->

> 状态：当前环境实施与验证完成，待 Windows 真机验证
>
> 日期：2026-08-07
>
> 分支：`main`
>
> 编码前提交：`296651ca279a36fbb22275c196dc4240415d164a`
>
> 技术方案：[Step 10 技术方案](Step10-macOSWindows构建安装与卸载技术方案.md)
>
> 实施计划：[Step 10 实施计划](../plan/Step10-macOSWindows构建安装与卸载实施计划.md)

## 1. 验证诚信边界

- 自动化测试、静态产物、macOS 当前设备和 Windows 真实设备分别记录；
- 交叉编译、PE/NSIS 静态检查和 Docker 构建不替代 Windows 真机；
- 临时目录和脚本 fixture 只验证安全契约，不冒充真实安装；
- 不记录凭据、会议正文、附件正文、用户模型路径或工作目录内容；
- Windows 真机未执行前，Step 10 保持“开发中”或“验证中”。

## 2. 环境与编码前基线

| 项目 | 值 |
| --- | --- |
| mise | `2026.4.11 macos-arm64` |
| Go | `go1.25.9 darwin/arm64` |
| Node.js | `v24.18.0` |
| pnpm | `11.4.0` |
| Wails | `v2.13.0` |
| macOS 目标 | `darwin/arm64` |
| Windows 目标 | `windows/amd64` |

编码前结果：

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `make test` | 通过 | Go 全包及前端 32 个测试文件、109 个测试通过 |
| `go test ./buildtools/cmd/verifywindows -count=1` | 通过 | 现有 Windows 校验器基线通过 |
| `make verify-package` | 失败 | 旧 `windows-resources` 缺少正式声纹 profile；需重新打包后复验 |

## 3. 增量验证记录

### 3.1 构建与安装器契约

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| `go test ./buildtools/... ./tests/contract/build ./tests/contract/installer -count=1` | 通过 | 构建工具与目录阻断、组件、mutex、防火墙和安全卸载契约通过 |
| `git diff --check` | 通过 | 针对 Step 10 代码范围执行，无空白错误 |

新增构建与安装器契约均先得到 RED，再实现到 GREEN。

### 3.2 macOS arm64 真实产物

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| `make build-macos-arm64` | 通过 | Wails production `.app` 构建成功，最后执行 ad-hoc codesign |
| `make package-macos` | 通过 | 创建只读压缩 DMG，并完成挂载与 `hdiutil verify` |
| `make verify-macos-package` | 通过 | arm64、资源哈希、许可证、profile、无模型权重和签名通过 |
| 从 DMG 安装至 `/Applications` | 通过 | 安装前确认目标不存在，挂载 DMG 后复制应用 |
| 安装后启动和签名检查 | 通过 | 应用进程已运行，`codesign --verify --deep --strict` 成功 |
| 删除应用本体 | 通过 | 只删除本轮 `MeetSieve.app`，应用数据和日志目录卸载后仍存在 |

macOS DMG SHA-256：

```text
6591422b0e853c0d2f124f25a3d2bebfa86118717e3a849056a289ae6bd36d6c
```

### 3.3 Windows amd64 交叉构建与静态产物

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| `make package-windows` | 通过 | 固定 Docker 工具链构建 amd64 GUI PE，NSIS 简体中文安装器编译成功 |
| 安装包解包校验 | 通过 | 容器内用 7-Zip 解包，四个嵌入资源逐文件比对一致，无 `.onnx` |
| `make verify-windows-package` | 通过 | PE、NSIS、资源锁、许可证、profile、安装标识和文件清单通过 |
| `make verify-package` | 通过 | macOS 与 Windows 产物门禁同时通过 |
| `file` | 通过 | 主程序为 PE32+ GUI x86-64，安装器为 NSIS PE32 GUI |

Windows NSIS SHA-256：

```text
97c51184578f02ef36980828c71433477619631dfa30879794f0e03c312a7823
```

Windows 构建时的 miniaudio C 代码产生 GCC `-Wstringop-overflow` 告警，但未中断编译；
该告警不替代 Windows 真机运行验证，已作为剩余风险保留。

### 3.4 全量质量门禁

| 命令 | 结果 | 说明 |
| --- | --- | --- |
| `make test` | 通过 | Go 全包以及前端 32 个测试文件、109 个测试通过 |
| `make test-race` | 通过 | 首次并行运行受沙箱端口权限和 Codex 并发干扰；沙箱外单独复跑通过 |
| `make test-contract` | 通过 | 全部契约测试通过 |
| `go vet ./...` | 通过 | `make lint` 中 Go 阶段通过 |
| 前端 ESLint | 通过 | `make lint` 中 ESLint 阶段通过 |
| `make lint` | 未通过 | Prettier 报告 5 个非 Step 10 文件的已有或并行格式差异，本次未越界修改 |
| `make typecheck` | 通过 | Vue 与 TypeScript 类型检查通过 |
| `pnpm --dir frontend build` | 通过 | 桌面和 Guest 生产构建通过；保留既有大 chunk 告警 |
| Step 10 文档 markdownlint | 通过 | 技术方案、计划、验证记录、总纲和用户指南 0 issue |

## 4. 当前未验证项

- 当前工作树包含用户其他未提交改动，因此本轮包只作为本地验证产物；发给测试者前必须从
  已记录的干净 commit 重建并重新生成 SHA-256；
- 全量 `make lint` 仍被 Step 10 范围外的 5 个前端 Prettier 差异阻塞；
- Windows 简体中文安装器真实页面；
- Windows 默认/自定义目录、非法目录阻断和覆盖升级；
- Windows 快捷方式、防火墙、mutex 和 WebView2；
- Windows 缺损动态库错误；
- Windows 安全卸载与未知文件、配置、日志、模型、会议工作目录保留。
