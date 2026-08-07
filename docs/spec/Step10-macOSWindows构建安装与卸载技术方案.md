# MeetSieve Step 10 macOS / Windows 构建、安装与卸载技术方案

<!-- markdownlint-disable MD013 -->

> 状态：当前环境实施与验证完成，待 Windows 真机验证
>
> 日期：2026-08-07
>
> 上位方案：[技术方案](技术方案.md)
>
> 开发总纲：[开发步骤总纲](开发步骤总纲.md)
>
> 前置步骤：[Step 9 技术方案](Step9-完整UI页面与数据生命周期收口技术方案.md)
>
> 实施计划：[Step 10 实施计划](../plan/Step10-macOSWindows构建安装与卸载实施计划.md)

## 1. 结论

Step 10 复用现有 Wails v2、Makefile、mise、固定 Docker、MinGW-w64 和 NSIS 构建链，
不更换桌面框架，不引入新的发布平台。当前工程已经能够产出 macOS arm64 `.app`、
Windows amd64 GUI PE 和 NSIS 壳；本步骤只补齐测试者真正安装所需的最小闭环：

1. macOS 将 production `.app` 制作为标准 DMG，提供未签名、未公证应用的打开和卸载说明；
2. Windows 将现有 NSIS 壳收口为标准简体中文安装器，支持安全目录选择、覆盖升级、可选
   桌面快捷方式和专用网络防火墙规则；
3. Windows 卸载器只删除当前安装明确登记的内容，始终保留未知文件、用户模型、配置、日志
   和会议工作目录；
4. 双平台构建统一版本、commit、时间、模式和资源身份，并通过自动化静态门禁验证；
5. macOS 当前设备和 Windows amd64 真实设备只执行安装层首轮验证，完整会议、ASR、Codex、
   LAN 和长时间运行验收仍由 Step 11 负责。

本期声纹模型权重不进入 DMG 或 NSIS。安装包只携带 ONNX Runtime、其许可证、锁定资源清单
以及正式声纹匹配 profile；模型继续由设置页下载或离线导入到用户应用数据目录。

## 2. 目标与成功标准

### 2.1 目标

- 从同一已记录 commit 可重复生成 macOS arm64 DMG 和 Windows amd64 NSIS；
- 测试者无需开发工具即可安装、双击启动、手动覆盖升级和卸载；
- 安装目录校验阻止高风险目标，升级只覆盖产品清单内文件；
- 应用运行时安装、升级和卸载均被阻止，不强制结束进程；
- 卸载后用户数据和安装目录中的未知文件保持不变；
- 动态库缺失或损坏时，应用显示已有的明确资源错误，不崩溃或静默降级；
- 产物、资源、许可证、版本与 commit 可追溯。

### 2.2 成功标准

Step 10 只有同时满足以下条件才能完成：

- macOS 当前 arm64 设备能够从 DMG 安装并启动；
- macOS 主程序和内嵌 ONNX Runtime dylib 均为 arm64；
- Windows 安装器为标准简体中文，可选择合法自定义目录并恢复旧安装位置；
- Windows 磁盘根目录、公共软件目录本身和未知非空目录被阻止；
- Windows 主程序为 amd64 GUI subsystem，双击不出现终端；
- 应用运行时安装、升级和卸载均被 mutex 阻止；
- 桌面快捷方式和专用网络防火墙规则分别验证勾选和取消；
- 卸载保留未知文件、配置、日志、用户模型和会议工作目录；
- ONNX Runtime、profile、许可证和资源哈希通过自动化校验；
- Windows 真实设备完成安装、启动、覆盖升级和卸载首轮验证。

## 3. 范围与非目标

### 3.1 本步骤范围

- macOS arm64 production `.app` 和 DMG；
- macOS ad-hoc codesign、架构检查、DMG 挂载检查和 Gatekeeper 操作说明；
- Windows amd64 固定 Docker CGO 构建；
- Windows GUI subsystem 和版本资源；
- Wails + NSIS 标准简体中文安装器、卸载器和组件页；
- 默认安装目录 `C:\Program Files\MeetSieve`；
- 新目录、空目录、已识别旧版目录和未知非空目录判断；
- 卸载注册表中的安装位置和版本记录；
- 产品标识、安装文件清单和安全覆盖升级；
- 开始菜单快捷方式；
- 可选且默认勾选的桌面快捷方式；
- 可选且默认勾选的 Private profile 程序级 TCP 入站规则；
- 安装、升级和卸载前共享 mutex 检查；
- ONNX Runtime、许可证、声纹 profile 和资源哈希；
- 双平台产物静态校验和 SHA-256；
- macOS 本机与 Windows 真机安装层首验；
- Step 10 验证记录和测试者操作清单。

### 3.2 非目标

- 不支持 macOS Intel、Windows arm64 或 Linux；
- 不做 macOS Developer ID 签名、公证和自动放行；
- 不做 Windows 代码签名、MSI、MSIX 或 Microsoft Store；
- 不实现自动更新、后台下载、增量升级、版本回滚或安装历史；
- 不开发自定义 Wails/Vue 安装界面或独立 macOS 卸载器；
- 不内置声纹模型权重，不改变当前模型下载和离线导入流程；
- 不自动复制、移动、合并或删除会议工作目录；
- 不增加“卸载时同时清理用户数据”选项；
- 不建设 CI/CD、公开下载站、崩溃收集或发布服务；
- 不在本步骤执行两小时会议和完整 ASR、Codex、LAN、声纹准确率验收。

## 4. 当前基线与缺口

### 4.1 可直接复用

- `Makefile` 已固定 Wails `v2.13.0`、Go `1.25.9` 和 Windows 构建镜像；
- `make build` 已生成 macOS arm64 `.app`，复制 ONNX Runtime、许可证和声纹 profile 后
  执行 ad-hoc codesign；
- `make build-windows-amd64` 已通过固定 Docker、MinGW-w64 和 CGO 生成 Windows amd64 PE；
- `make package-windows` 已使用 Wails + NSIS 生成安装包和 `uninstall.exe`；
- Windows 应用已使用 GUI subsystem；
- 应用、安装器和卸载器已共享 `Global\MeetSieve.App.Instance.v1`；
- `verifywindows` 已检查 PE 架构、GUI subsystem、关键 CGO 依赖、基础资源和 NSIS PE；
- `third_party/assets.lock.json` 已锁定双平台 ONNX Runtime 和官方声纹模型包哈希；
- 资源损坏、缺失和模型未安装已有应用层健康状态与错误展示基础。

### 4.2 本步骤必须补齐

- 缺少 `build-macos-arm64`、`package-macos` 和 macOS 产物校验入口；
- 缺少 DMG staging、`/Applications` 入口和挂载后检查；
- NSIS 当前为英文，且没有标准组件页；
- NSIS 当前默认路径会形成重复的 `MeetSieve\MeetSieve`；
- 桌面快捷方式当前强制创建，开始菜单和桌面入口没有独立组件语义；
- 缺少 Private profile 防火墙规则；
- 缺少目标目录安全校验、产品标识、安装清单和旧位置恢复；
- 当前卸载使用递归删除安装目录，会误删未知文件，必须移除；
- 当前 Windows 校验器只检查资源存在和安装包大小，没有核对锁定哈希和安全卸载契约；
- 应用内版本与 Windows 安装器版本来源尚未统一；
- 缺少 Windows 真实设备安装、升级和卸载证据。

## 5. 产物与构建身份

### 5.1 产物

```text
build/bin/
├── MeetSieve.app
├── MeetSieve-<semver>-macos-arm64.dmg
├── MeetSieve.exe
├── MeetSieve-<semver>-windows-amd64-installer.exe
└── SHA256SUMS.txt
```

`build/bin/windows-resources/` 仍作为 NSIS 输入暂存目录，不作为独立交付物。

### 5.2 版本模型

构建使用两个相关但不同的版本值：

| 字段 | 示例 | 用途 |
| --- | --- | --- |
| `BUILD_VERSION` | `0.1.0-alpha.1` | 应用内诊断、DMG/NSIS 文件名、卸载列表显示 |
| `WINDOWS_FILE_VERSION` | `0.1.0.1` | Windows PE/NSIS 四段数字版本 |

两者必须在构建入口中同时声明并校验，不能从不同文件静默取默认值。构建同时写入：

- 完整 Git commit；
- UTC 构建时间；
- `production` 构建模式；
- 目标 OS 和架构；
- ONNX Runtime 版本与库 SHA-256；
- 声纹模型包身份和 profile ID。

Step 10 测试包可以使用 Alpha 版本；Step 12 再确定最终内部发布版本。发给测试者的包必须
来自已记录 commit。工作树不干净时允许本地调试构建，但不得作为正式 Step 10 真机证据。

## 6. macOS 构建与 DMG

### 6.1 构建流程

新增稳定入口：

```text
make build-macos-arm64
make package-macos
make verify-macos-package
```

`build-macos-arm64` 执行：

1. 运行现有资源下载与哈希校验、声纹 profile 门禁和前端 production build；
2. 使用 Wails 生成 `MeetSieve.app`；
3. 将 arm64 ONNX Runtime dylib、MIT 许可证和声纹 profile 放入固定 Resources 路径；
4. 校验安装包不含声纹模型 `.onnx` 权重；
5. 在所有资源复制完成后执行 ad-hoc codesign；
6. 校验 codesign 结构、主程序 Mach-O 和 dylib 架构。

### 6.2 DMG 结构

DMG 使用系统 `hdiutil` 生成压缩只读镜像，不引入第三方 DMG 工具：

```text
MeetSieve <version>/
├── MeetSieve.app
└── Applications -> /Applications
```

不制作背景图、窗口坐标脚本、自定义拖拽动画和 EULA 页面。DMG 创建后重新只读挂载，检查：

- 卷内仅包含 `.app`、`Applications` 入口和系统自动元数据；
- `.app` 可复制到临时 Applications 等价目录；
- `.app` 中 ONNX Runtime、许可证和 profile 存在且哈希正确；
- 不包含模型权重、构建 cache、源码、日志或工作目录数据。

### 6.3 安装与卸载说明

测试说明采用最短可执行路径：

- 将 `MeetSieve.app` 拖入 Applications；
- 首次打开优先使用 Finder 右键“打开”；
- 系统仍阻止时，再说明在“隐私与安全性”中确认打开；
- `xattr` 只作为明确的测试兜底命令，不由应用或安装包自动执行；
- 卸载时先退出应用，再删除 `/Applications/MeetSieve.app`；
- 默认保留 `~/Library/Application Support/MeetSieve`、`~/Library/Logs/MeetSieve` 和会议工作目录。

## 7. Windows 构建与 NSIS

### 7.1 固定构建链

继续使用现有 `golang:1.25.9-bookworm` amd64 Docker 镜像、MinGW-w64、NSIS 和 Wails
`v2.13.0`。宿主完成前端构建和 Wails bindings 生成，容器使用 `-skipbindings` 完成交叉构建。

不得为了安装器功能改为宿主临时安装 Go、MinGW 或 NSIS。Dockerfile、镜像标签和构建命令
仍是唯一 Windows 构建事实源。

### 7.2 NSIS 页面与组件

使用 NSIS Modern UI 标准简体中文页面：

```text
欢迎页 -> 安装目录页 -> 组件页 -> 安装进度页 -> 完成页
卸载确认页 -> 卸载进度页 -> 完成页
```

组件分为：

| 组件 | 默认值 | 行为 |
| --- | --- | --- |
| 核心程序 | 必装 | EXE、ONNX Runtime、许可证、profile、产品标识、安装清单、卸载器 |
| 开始菜单快捷方式 | 必装 | 创建所有用户可见的 MeetSieve 开始菜单入口 |
| 桌面快捷方式 | 勾选 | 创建所有用户桌面快捷方式 |
| 局域网访客防火墙规则 | 勾选 | 创建 Private profile、程序级、入站 TCP allow 规则 |

WebView2 沿用 Wails 当前 browser bootstrapper 策略。WebView2 安装失败必须使安装器返回失败，
不得继续显示完整成功。

### 7.3 安装目录恢复与校验

默认安装目录固定为：

```text
C:\Program Files\MeetSieve
```

`.onInit` 先读取 HKLM 卸载注册表中的 `InstallLocation`。该值存在且通过产品目录校验时，
作为覆盖升级默认目录；无有效旧值时使用默认目录。

目录页离开时执行安全校验：

1. 规范化用户选择路径，拒绝空路径和相对路径；
2. 拒绝驱动器根目录、Windows 目录、System32、`Program Files` 和 `Program Files (x86)` 本身；
3. 目标不存在时允许创建；
4. 目标为空目录时允许；
5. 目标非空时，只在产品标识内容合法且产品 ID、安装 schema 匹配时允许覆盖升级；
6. 其他非空目录一律阻止，并要求用户选择新的专属目录。

本期不解析符号链接或 junction 后跨卷安装。真实设备测试发现 Windows 路径规范化无法可靠
判断时，应阻止该目录，不得放宽为任意非空目录。

### 7.4 产品标识与安装清单

安装目录增加两个小型 UTF-8 JSON 文件：

```text
meetsieve-install.json
meetsieve-files.json
```

`meetsieve-install.json` 至少包含：

```json
{
  "schema_version": 1,
  "product_id": "meet-sieve",
  "version": "0.1.0-alpha.1",
  "arch": "amd64"
}
```

`meetsieve-files.json` 只记录安装器创建的相对文件路径和资源 SHA-256，不能记录绝对用户目录、
`config.json`、日志、用户模型或会议工作目录。NSIS 卸载仍使用编译期显式文件列表执行删除，
JSON 清单用于识别、审计和验证，不作为可被篡改后直接执行的删除指令。

覆盖升级只覆盖当前版本清单中的文件。需要删除旧版本已废弃文件时，必须在 NSIS 中明确列出
旧相对路径；不得通过清空安装目录实现升级。

### 7.5 防火墙规则

规则使用稳定且唯一的显示名：

```text
MeetSieve LAN Private
```

规则约束：

- `dir=in`、`action=allow`、`profile=private`、`protocol=TCP`；
- program 精确指向 `$INSTDIR\MeetSieve.exe`；
- 不开放固定端口，不覆盖 Public 或 Domain profile；
- 安装前先删除同名且指向旧 MeetSieve 安装位置的规则，再按当前路径创建，避免覆盖升级重复；
- 用户取消组件时不创建规则；
- 卸载只删除该稳定规则名对应的 MeetSieve 规则；
- 创建或删除失败必须在安装详情和退出状态中可诊断，不静默写成成功。

应用继续使用动态 LAN 端口。防火墙规则只放行签名状态不稳定的 Alpha 程序路径，不改变
LAN server 的私网网卡校验、会议级 token 和结束会议立即关闭行为。

### 7.6 运行实例阻断

安装、覆盖升级和卸载在写入或删除文件前打开：

```text
Global\MeetSieve.App.Instance.v1
```

mutex 存在时显示中文提示并中止。安装器不得使用 `taskkill`、`TerminateProcess`、服务停止、
窗口消息或其他方式强制结束 MeetSieve。用户退出应用后重新运行安装器或卸载器。

## 8. Windows 安全卸载

### 8.1 删除顺序

卸载器按以下顺序执行：

1. 检查共享 mutex；
2. 删除精确防火墙规则；
3. 删除安装器创建的开始菜单和桌面快捷方式；
4. 删除编译期清单中的主程序、ONNX Runtime、许可证、profile 和产品标识；
5. 删除空的 `models` 目录；
6. 删除卸载注册表信息；
7. 删除卸载器自身；
8. 仅在安装根目录已经为空时删除该目录。

### 8.2 禁止项

卸载器明确禁止：

- `RMDir /r $INSTDIR` 或任何等价递归删除安装根目录的操作；
- 根据 `config.json`、SQLite、日志或环境变量解析会议工作目录后删除；
- 删除 `%LocalAppData%\MeetSieve`、用户模型目录或 WebView2 数据目录；
- 删除名称未知、未在编译期清单登记的文件；
- 因某一文件删除失败而继续报告完整成功；
- 为清理失败而强制结束应用或重启系统后递归删除目录。

若安装目录存在 `keep-me.txt` 等未知文件，清单文件删除后保留未知文件和安装目录。卸载器可
提示“程序已卸载，安装目录中仍有非 MeetSieve 文件”，但不提供一键清理按钮。

## 9. 资源与许可证完整性

### 9.1 安装包携带内容

双平台核心资源保持一致：

- MeetSieve 主程序；
- 当前平台 ONNX Runtime `1.26.0`；
- ONNX Runtime MIT 许可证；
- `models/voice-matching-profile.json`；
- 产品标识和安装资源清单。

声纹模型 `model.onnx`、模型 LICENSE 和 NOTICE 不进入应用安装包。它们只在用户下载或离线
导入官方模型包后进入用户模型目录，并继续使用现有 archive、manifest、model、LICENSE 和
NOTICE 哈希门禁。

### 9.2 自动化校验

校验器从 `third_party/assets.lock.json` 取得目标平台、架构、版本、文件大小和 SHA-256，
不得在测试中复制另一套期望哈希。至少校验：

- ONNX Runtime 文件哈希和大小；
- ONNX Runtime 许可证存在且非空；
- 声纹 profile 可解析，模型身份与当前资源锁一致；
- 安装暂存目录和 `.app` 不含 `.onnx` 权重；
- 安装资源清单与实际必装资源一致；
- macOS Mach-O 架构、Windows PE 架构和 GUI subsystem；
- 安装器脚本不存在递归删除、强制杀进程和静默用户数据清理；
- 最终 DMG 和 NSIS SHA-256 写入 `SHA256SUMS.txt`。

## 10. Makefile 与脚本边界

建议稳定入口：

```text
make build
make build-macos-arm64
make build-windows-amd64
make package-macos
make package-windows
make verify-macos-package
make verify-windows-package
make verify-package
```

- `build` 继续表示当前宿主平台 production 构建，兼容既有调用；
- `build-macos-arm64` 明确 Step 10 目标平台；
- `package-*` 只负责从已经验证的 production 构建生成安装介质；
- `verify-*` 不修改产物，只做只读检查；
- `verify-package` 聚合两个平台的静态校验，但不伪装 Windows 真机通过；
- shell 脚本只编排系统工具，复杂 JSON、PE、Mach-O 和哈希规则放入职责单一的 Go buildtool；
- 不在 Makefile 中散落第二套版本、资源哈希和安装清单事实。

建议新增或修改：

```text
Makefile
build/darwin/
build/windows/installer/project.nsi
buildtools/cmd/verifymacos/
buildtools/cmd/verifywindows/
buildtools/windows/build.sh
docs/guide/安装卸载与数据保留.md
```

最终文件名可按现有目录风格调整，但不得把安装器策略放入业务 service 或 Wails binding。

## 11. 验证方案

### 11.1 自动化与静态验证

```text
make test
make test-race
make lint
make typecheck
make build-macos-arm64
make package-macos
make build-windows-amd64
make package-windows
make verify-package
git diff --check
```

新增测试至少覆盖：

- macOS 资源缺失、哈希不符、错误架构和意外模型权重；
- Windows NSIS 缺少中文、组件、目录校验、InstallLocation、mutex 或显式卸载清单；
- Windows NSIS 出现递归删除、强制杀进程或用户目录删除时失败；
- Windows 资源缺失、空文件、大小或哈希不符时失败；
- 版本不合法或 semantic/Windows 数字版本不匹配时失败；
- 安装清单包含绝对路径、父目录跳转或用户数据路径时失败。

### 11.2 macOS 当前设备验证

1. 挂载 DMG，将 `.app` 拖入 `/Applications`；
2. 按未签名应用说明首次打开；
3. 验证首次工作目录选择和第二次启动定位同一数据库；
4. 验证 ONNX Runtime 初始化和模型未安装状态；
5. 退出并删除 `.app`；
6. 验证应用配置、日志、用户模型和会议工作目录仍存在；
7. 重装后验证 locator 和已有工作目录可继续使用。

### 11.3 Windows amd64 真实设备首验

至少执行两轮：

#### 第一轮：默认安装与覆盖升级

- 默认目录安装；
- 勾选桌面快捷方式和防火墙；
- 从开始菜单和桌面双击启动，确认不出现终端；
- 应用运行时重新执行安装器和卸载器，确认均被阻止；
- 退出后使用新测试版本覆盖升级，确认沿用原位置且数据不变；
- 验证防火墙规则只有一条且程序路径正确；
- 在安装目录加入 `keep-me.txt`，卸载后确认文件和目录保留；
- 确认配置、日志、用户模型和会议工作目录保留。

#### 第二轮：自定义目录与取消组件

- 安装到包含空格或中文的 D 盘专属目录；
- 取消桌面快捷方式和防火墙；
- 确认开始菜单入口存在，桌面入口和防火墙规则不存在；
- 验证磁盘根目录、公共目录本身和未知非空目录被阻止；
- 双击启动并确认 SQLite、WebView2 和 ONNX Runtime 初始化；
- 临时移走或损坏 `onnxruntime.dll`，确认错误明确，再恢复文件；
- 从 Windows“已安装的应用”卸载并验证用户数据保留。

Step 10 只要求上述安装层首验。真实麦克风、火山 ASR、Codex 多轮、LAN 手机访问、模型下载
推理和长会议行为可做 smoke，但其正式跨平台结论仍进入 Step 11。

## 12. 错误处理与诊断

- 构建缺少版本、commit、资源锁或正式 profile 时立即失败；
- DMG 创建、挂载或校验失败时不保留同名成功产物；
- NSIS WebView2、文件复制、注册表、快捷方式或防火墙操作失败时返回非零状态并保留详情；
- 安装目录不安全时在目录页就阻止，不进入文件写入阶段；
- mutex 存在时使用统一中文提示，不记录用户会议路径；
- 卸载部分失败时提示未删除的产品文件，不删除未知文件兜底；
- 应用运行时资源错误继续使用现有 `AppError` 和健康状态，不新增安装器专用业务错误码；
- 日志和错误提示不得包含 ASR 密钥、Codex token、会议正文或完整用户附件路径。

## 13. 风险与处理

| 风险 | 处理 |
| --- | --- |
| macOS 未签名、未公证被 Gatekeeper 阻止 | 提供 Finder 右键打开和系统设置确认说明；不伪装正式签名体验 |
| macOS arm64 宿主通过 Docker 模拟构建 Windows 较慢 | 继续复用固定镜像和 cache；不改为未锁宿主工具链 |
| NSIS 路径判断对 junction、特殊根目录不完整 | 不确定时阻止目标；只允许新、空或有效产品目录 |
| 自定义安装目录含未知文件，递归卸载会造成数据损失 | 编译期显式删除清单，禁止递归删除安装根目录 |
| 防火墙规则在自定义路径升级后重复 | 使用稳定规则名，安装前清理旧 MeetSieve 精确规则后按当前 EXE 路径创建 |
| WebView2 在线 bootstrapper 受网络影响 | 安装失败明确返回；本期不引入固定版离线 WebView2 大包 |
| 交叉编译和静态检查无法证明 Windows 运行 | Step 10 完成门保留 Windows 真实设备首验，不以 PE/NSIS 检查替代 |
| 测试包来自脏工作树无法追溯 | 正式真机包只接受已记录 commit；本地脏构建不计入验证证据 |

## 14. 实施顺序

1. **构建身份收口**：统一 semantic version、Windows 数字版本、commit 和产物命名；
2. **macOS 包装**：增加 arm64 明确入口、DMG 生成和只读校验；
3. **Windows 目录与升级**：简体中文、默认目录、InstallLocation、产品标识和非空目录阻断；
4. **Windows 组件**：开始菜单、可选桌面快捷方式和 Private firewall；
5. **Windows 安全卸载**：显式删除清单、用户数据隔离和未知文件保留；
6. **产物门禁**：资源哈希、架构、GUI subsystem、许可证、危险 NSIS 片段和 SHA-256；
7. **实机首验**：macOS DMG 和 Windows 两轮安装矩阵；
8. **记录收口**：更新开发总纲和 Step 10 验证记录，未通过项进入 Step 11 缺陷清单。

每个任务先建立可重复的静态或自动化失败用例，再修改构建脚本；实机事实单独记录，不能由
脚本测试、mock、交叉编译或安装包解包结果替代。

## 15. 完成门

进入 Step 11 前必须确认：

- [ ] Step 9 已由用户确认完成，Step 10 对应 commit 已记录；
- [x] macOS arm64 `.app`、DMG、架构、资源和 ad-hoc codesign 校验通过；
- [x] macOS 从 DMG 安装、启动、删除 `.app` 和数据保留验证通过；
- [x] Windows amd64 GUI PE 和简体中文 NSIS 可重复生成；
- [ ] Windows 默认目录、自定义目录、旧位置恢复和非法目录阻断通过；
- [ ] Windows 覆盖升级不清空安装目录且沿用原位置；
- [ ] 应用运行时安装、升级和卸载均被阻止；
- [ ] 桌面快捷方式和防火墙规则勾选、取消均通过；
- [ ] Windows 卸载保留未知文件、配置、日志、用户模型和会议工作目录；
- [ ] ONNX Runtime 缺失和损坏有明确错误；
- [x] 安装包不含声纹模型权重；
- [x] 资源哈希、许可证清单、版本、commit 和最终产物 SHA-256 一致；
- [ ] Windows 真实设备首轮安装、启动、覆盖升级和卸载证据已记录；
- [ ] 未验证项和剩余风险没有被描述为已通过。

## 16. 用户确认

用户于 2026-08-07 确认按照本技术方案执行。实施口径为：

- Step 10 按本方案执行轻量安装闭环；完整会议、真实火山、Codex、LAN、声纹推理和长时间
  稳定性继续由 Step 11 验收，不把它们重复塞入 Step 10 完成门。
