# MeetSieve Step 10 macOS / Windows 构建、安装与卸载实施计划

<!-- markdownlint-disable MD013 -->

> 状态：当前环境实施与验证完成，待 Windows 真机验证
>
> 日期：2026-08-07
>
> 技术方案：[Step 10 技术方案](../spec/Step10-macOSWindows构建安装与卸载技术方案.md)
>
> 总体方案：[技术方案](../spec/技术方案.md)
>
> 开发总纲：[开发步骤总纲](../spec/开发步骤总纲.md)
>
> 验证记录：[Step 10 验证记录](../spec/Step10-验证记录.md)

## 1. 实施目标

复用现有 Wails v2、mise、Makefile、固定 Docker、MinGW-w64 和 NSIS 构建链，完成以下最小闭环：

1. macOS arm64 production `.app`、标准 DMG 和自动化产物校验；
2. Windows amd64 简体中文 NSIS、安全目录、覆盖升级、组件、防火墙和安全卸载；
3. 双平台统一构建身份、资源哈希、许可证和最终产物 SHA-256；
4. macOS 本机安装验证和 Windows 真实设备首验清单；
5. 未取得 Windows 真机证据前保持“验证中”，不以交叉编译替代。

## 2. 工作树与验证基线

- 分支：`main`；
- 编码前提交：`296651ca279a36fbb22275c196dc4240415d164a`；
- 保留用户已有 staged/untracked 业务改动，不 reset、不覆盖、不顺手重构；
- Step 10 只修改构建、安装器、产物校验和对应文档；
- 修改前 `make test` 通过；
- 修改前 `go test ./buildtools/cmd/verifywindows -count=1` 通过；
- 修改前 `make verify-package` 因旧 Windows 暂存产物缺少正式声纹 profile 失败，重新打包后复验。

## 3. 测试协议

每个安全边界按以下顺序实施：

1. 先新增或扩展最小测试，确认当前脚本缺少目标契约；
2. 只实现使该契约通过的构建或安装逻辑；
3. 运行同一测试和直接相关构建；
4. 重构后再次运行原测试；
5. 自动化、macOS 实机和 Windows 真机证据分别记录。

允许使用临时目录、临时文件、测试生成的最小 Mach-O/PE 和受控脚本文本验证静态边界；不得
用它们证明 DMG、NSIS 或 Windows 真实安装行为已经通过。

## 4. 任务与完成门

### 任务 1：状态、计划与构建身份

修改范围：

- `Makefile`；
- `cmd/meetsieve/wails.json`；
- `buildtools/windows/build.sh`；
- Step 10 文档。

行为：

- 默认 semantic version 为 `0.1.0-alpha.1`；
- Windows file version 为四段数字 `0.1.0.1`；
- 构建缺少或使用非法版本时失败；
- macOS/Windows 应用均写入同一 version、commit、time 和 mode；
- 产物文件名包含 semantic version 和目标平台。

验证：构建契约测试、buildtool 单测、`git diff --check`。

### 任务 2：macOS arm64 `.app` 与 DMG

修改范围：

- `Makefile`；
- `buildtools/cmd/verifymacos/`；
- `build/darwin/` 必要静态资源。

行为：

- `make build-macos-arm64` 生成完整 `.app` 并最后执行 ad-hoc codesign；
- `make package-macos` 使用 `hdiutil` 生成只读压缩 DMG；
- DMG staging 只有 `.app` 和 `/Applications` 入口；
- `make verify-macos-package` 校验 Mach-O arm64、资源哈希、许可证、profile 和无模型权重；
- 校验失败不生成或保留同名成功标记。

验证：verifymacos 单测、真实 `.app` 构建、DMG 创建/挂载/只读检查。

### 任务 3：Windows 简体中文、安全目录与覆盖升级

修改范围：

- `build/windows/installer/project.nsi`；
- Windows 生成资源；
- installer 契约测试。

行为：

- 简体中文 Modern UI；
- 默认目录为 `C:\Program Files\MeetSieve`；
- 从 HKLM `InstallLocation` 恢复有效旧位置；
- 新目录、空目录、有效产品目录允许；
- 驱动器根、Windows/Public Program Files 根和未知非空目录阻止；
- 安装 `meetsieve-install.json` 与 `meetsieve-files.json`；
- 覆盖升级只覆盖明确产品文件。

验证：installer 契约测试、verifywindows 单测、NSIS 编译。

### 任务 4：Windows 组件、防火墙与运行阻断

行为：

- 核心程序和开始菜单必装；
- 桌面快捷方式、Private profile TCP 程序规则可选且默认勾选；
- 防火墙规则名稳定，安装前清理同名旧规则，卸载精确删除；
- 安装、升级、卸载共用 mutex；
- 不使用 `taskkill` 或 `TerminateProcess`；
- WebView2 安装失败返回失败。

验证：installer 契约测试、NSIS 编译、Windows 真机勾选/取消矩阵。

### 任务 5：Windows 安全卸载

行为：

- 按编译期清单显式删除产品文件和快捷方式；
- 只在目录为空时删除 `models` 和安装根目录；
- 不递归删除 `$INSTDIR`、AppData、用户模型、日志或工作目录；
- 未知文件存在时保留文件和目录；
- 部分删除失败不报告完整成功。

验证：静态危险片段门禁、installer 契约测试、Windows 真机 `keep-me.txt` 验证。

### 任务 6：资源、许可证和最终产物门禁

修改范围：

- `buildtools/cmd/verifywindows/`；
- `buildtools/cmd/verifymacos/`；
- `Makefile`。

行为：

- 从 `third_party/assets.lock.json` 读取 ONNX Runtime 期望大小和 SHA-256；
- 严格解析声纹 profile 并核对模型身份；
- 双平台安装输入不得包含 `.onnx` 模型权重；
- Windows PE 为 amd64 GUI，NSIS 契约完整；
- DMG 和 NSIS 写入 `build/bin/SHA256SUMS.txt`。

验证：buildtool 单测、双平台 package、`make verify-package`。

### 任务 7：说明、实机验证与记录

产物：

- `docs/guide/安装卸载与数据保留.md`；
- `docs/spec/Step10-验证记录.md`。

验证：

- macOS 从 DMG 安装、首次打开、启动、删除 `.app` 和数据保留；
- Windows 默认目录与自定义目录两轮安装；
- Windows 快捷方式/防火墙勾选取消、运行阻断、覆盖升级、缺损资源和安全卸载；
- Windows 真机未执行时明确保持待验证。

## 5. 全量完成门

```text
make test
make test-race
make test-contract
make lint
make typecheck
mise exec -- pnpm --dir frontend build
make build-macos-arm64
make package-macos
make verify-macos-package
make build-windows-amd64
make package-windows
make verify-windows-package
make verify-package
git diff --check
```

Windows 真机安装行为无法在 macOS/Docker 中验证。所有命令通过后仍只表示开发与当前环境验证
完成；必须收到真实 Windows amd64 设备结果后，才能将 Step 10 标记为已完成并进入 Step 11。
