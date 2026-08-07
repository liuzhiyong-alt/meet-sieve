# 会意 MeetSieve

<!-- markdownlint-disable MD013 -->

会意（MeetSieve）是一款面向 2～10 人线下会议的本地优先 AI 会议助手。它把本地录音、实时转写、说话人辅助识别、局域网资料收集和 Codex 问答整理到同一条会议时间线中，并在会后生成可追溯、可人工校对的会议记录与纪要草稿。

> 当前状态：Alpha。不建议把未经复核的 AI 输出直接作为正式会议结论。

[下载 Release](https://github.com/liuzhiyong-alt/meet-sieve/releases) · [提交 Issue](https://github.com/liuzhiyong-alt/meet-sieve/issues) · [Apache-2.0 License](LICENSE)

## 关于 MeetSieve

### 功能概览

- 使用一台电脑录制线下会议，录音、数据库、附件和声纹样本保存在用户选择的本地工作目录；
- 接入火山引擎大模型语音识别，显示实时转写，并在会后补录实时转写缺口；
- 使用本地 CAM++ 模型生成声纹特征，辅助判断常用成员；
- 通过同一局域网内的访客页面收集文字、链接和附件，无需为参会者创建账号；
- 调用用户电脑上已经安装并登录的 Codex，在会中回答问题、会后整理纪要；
- 支持人工修正转写文字和说话人，会议原始记录与 AI 纪要相互独立；
- 支持存储扫描、日志打开和诊断包导出，便于排查本地问题。

MeetSieve 不提供云端账号、团队空间或自动同步。SQLite 与会议文件是唯一事实源，Codex 对话可以恢复或重新创建。

### 项目结构

```text
cmd/meetsieve/          Wails 桌面应用入口
frontend/               Vue 3 + TypeScript 前端
internal/domain/        领域模型和状态
internal/service/       会议、转写、声纹、纪要等业务编排
internal/adapter/       音频、火山 ASR、ONNX、Codex 等适配器
internal/transport/     Wails bindings 与局域网 HTTP
migrations/sqlite/      版本化 SQLite migration
models/                 声纹匹配档案与说明，不存放模型权重
third_party/            第三方运行时与模型资源锁
build/                  Wails、macOS、Windows 安装资源
buildtools/             构建、打包和产物校验工具
tests/                  单元、集成、契约和真实 E2E 测试
docs/                   产品、技术方案、设计系统与验证记录
```


## 下载安装与使用

这一部分按真实使用顺序编排。照着完成“下载 → 安装 → 初始化 → 配置 → 开始会议”即可使用。

### 第 1 步：确认系统并下载安装包

MeetSieve 当前支持：

| 平台 | 架构 | 安装包 | 系统要求 |
| --- | --- | --- | --- |
| macOS | Apple Silicon（arm64） | `MeetSieve-<version>-macos-arm64.dmg` | macOS 11 及以上 |
| Windows | 64 位 x86（amd64） | `MeetSieve-<version>-windows-amd64-installer.exe` | Windows 10 / Windows Server 2016 及以上 |

macOS Intel、Windows ARM 和 Linux 暂不支持。桌面窗口默认尺寸为 `1280 × 800`，最小尺寸为 `1024 × 720`。

打开项目的 [Releases 页面](https://github.com/liuzhiyong-alt/meet-sieve/releases)，进入需要安装的应用版本，按操作系统下载对应文件：

| Release 文件 | 用途 |
| --- | --- |
| `MeetSieve-<version>-macos-arm64.dmg` | macOS Apple Silicon 安装盘 |
| `MeetSieve-<version>-windows-amd64-installer.exe` | Windows 64 位安装程序 |
| `SHA256SUMS.txt` | 安装包 SHA-256 校验清单 |
| `meetsieve-voice-campplus-1.0.0-ms1.zip` | 跨平台声纹模型离线包，不是应用安装包 |

请优先选择正式应用版本的 Release，不要把名称以 `voice-model-` 开头的模型 Release 当作客户端版本。

#### 可选：校验下载文件

Release 同时提供 `SHA256SUMS.txt` 时，建议在安装前校验文件。

macOS：

```bash
shasum -a 256 MeetSieve-<version>-macos-arm64.dmg
```

Windows PowerShell：

```powershell
Get-FileHash .\MeetSieve-<version>-windows-amd64-installer.exe -Algorithm SHA256
```

计算结果应与 `SHA256SUMS.txt` 中同名文件的值完全一致。

### 第 2 步：安装 MeetSieve

#### macOS

1. 打开下载的 DMG；
2. 把 `MeetSieve.app` 拖到 `Applications`；
3. 在“应用程序”中右键 MeetSieve，选择“打开”；
4. Alpha 包当前没有 Apple Developer ID 签名和公证。如果 macOS 阻止打开，请进入“系统设置 → 隐私与安全性”，确认打开；
5. 第一次使用录音时，按系统提示允许 MeetSieve 访问麦克风。

只有确认安装包来自本仓库 Release、且上述方式仍无法打开时，才考虑移除隔离属性：

```bash
xattr -dr com.apple.quarantine /Applications/MeetSieve.app
```

#### Windows

1. 双击 `MeetSieve-<version>-windows-amd64-installer.exe`；
2. 确认安装目录，默认是 `C:\Program Files\MeetSieve`，安装过程需要管理员权限；
3. 按需保留“桌面快捷方式”和“局域网访客防火墙规则”；防火墙规则只放行 Private 网络中的 MeetSieve 程序；
4. 安装器会检查 Microsoft Edge WebView2 Runtime；缺少且无法安装时，安装不会报告完整成功；
5. 从开始菜单或桌面快捷方式启动 MeetSieve，并允许应用使用麦克风。

Alpha 安装程序当前没有 Windows 代码签名，SmartScreen 可能显示未知发布者。请先核对下载来源和 SHA-256，再决定是否继续。

### 第 3 步：完成首次初始化

第一次启动时，MeetSieve 会要求选择“会议工作目录”。该目录是会议数据的核心存储位置，建议创建一个空间充足、仅由当前用户使用的本地目录，例如：

- macOS：`/Users/你的名字/MeetSieve`；
- Windows：`D:\MeetSieve`。

不要选择应用安装目录、磁盘根目录、系统目录、网络盘或来历不明的非空目录。MeetSieve 会在工作目录中创建：

```text
data/meetings.db    # SQLite 数据库与业务设置
meetings/           # 每场会议的录音、记录和附件
voice-samples/      # 用户主动录入或确认加入的声纹样本
backups/            # 数据库迁移与恢复备份
```

工作目录包含会议内容和火山引擎 APP Key。当前版本把 APP Key 明文保存在工作目录的 SQLite 数据库中，但不会写入日志或会议原始记录。请勿把整个工作目录公开分享、提交到 Git，或放入权限不明确的同步目录。

如果以后要搬移数据：完全退出 MeetSieve，复制**整个**工作目录，在“设置 → 通用”选择新目录并保存，然后重启应用。不要只复制 `meetings.db`。

### 第 4 步：按需要完成设置

只想本地录音时，配置麦克风即可开始。其他能力可以按需启用：

| 想使用的能力 | 必须准备的内容 |
| --- | --- |
| 本地录音和保存 | 可用麦克风、可写的会议工作目录 |
| 实时转写 | 火山引擎新版 APP Key、“大模型流式语音识别 2.0 小时版”权限 |
| 会后缺口补录 | 同一个 APP Key、“大模型录音文件极速版”权限 |
| 声纹辅助识别 | 官方声纹模型包、成员声纹样本 |
| 会中 AI 与会后纪要 | 已安装并登录的 Codex CLI；语音唤醒还需要实时转写 |
| 局域网访客入口 | 可信私有网络；Windows 建议安装对应防火墙规则 |

设置中的每个分类独立保存。推荐按下面的顺序配置。

#### 4.1 通用与录音

在“设置 → 通用”中：

- 确认会议工作目录正确且可写；修改目录会在下次启动时生效；
- 使用“扫描存储占用”检查录音、附件和可用空间；
- 遇到问题时可打开日志目录或导出诊断包。诊断包仍应在分享前检查是否包含不希望外发的信息。

在“设置 → 录音”中：

- 选择默认麦克风并点击“测试麦克风”；
- macOS 如果没有声音，请检查“系统设置 → 隐私与安全性 → 麦克风”；
- Windows 请检查“设置 → 隐私和安全性 → 麦克风”，并允许桌面应用访问麦克风。

完成这里以后，即使不配置火山引擎、声纹模型和 Codex，也可以仅录音并在本地保存会议。

#### 4.2 火山引擎实时转写与缺口补录

MeetSieve 只接受新版豆包语音控制台的 `APP Key`，不接受旧版 `App ID + Access Token`，也不使用火山引擎通用 `AccessKey ID / Secret`。

获取 APP Key 与开通服务：

1. 注册并登录[火山引擎](https://console.volcengine.com/)，按控制台要求完成实名认证；
2. 进入[豆包语音控制台](https://console.volcengine.com/speech/app)，创建一个应用；
3. 为应用开通“大模型流式语音识别 2.0 小时版”。MeetSieve 的实时资源 ID 固定为 `volc.seedasr.sauc.duration`；
4. 如果需要会议结束后的转写缺口补录，同时开通“大模型录音文件极速版”。其资源 ID 固定为 `volc.bigasr.auc_turbo`；
5. 根据控制台的试用或正式开通流程确认计费方式、额度和并发限制；欠费或额度不足会导致转写不可用；
6. 在新版控制台复制该应用的 `APP Key`。不要复制旧版的 App ID、Access Token，也不要把 APP Key 发给其他人；
7. 在 MeetSieve 中打开“设置 → 实时转写”，粘贴 APP Key，先点击“测试连接”，再点击“保存更改”。

火山引擎官方资料：[控制台开通服务指南](https://www.volcengine.com/docs/6561/163043?lang=zh)、[语音识别产品页](https://www.volcengine.com/product/asr)、[新版 APP Key 请求头说明](https://www.volcengine.com/docs/6561/1631584?lang=zh)。

“测试连接”只验证 WebSocket 建连和初始化，不发送真实音频，也不能证明真实转写和缺口补录都已开通。保存后建议创建一次短会议，说几句话并正常结束，确认实时文字和会后收尾均可用。

没有 APP Key 时仍可仅录音，但新会议不能实时转写；录音、本地保存、实时转写、缺口补录和 Codex 是彼此独立的状态。

#### 4.3 声纹模型与成员

应用安装包包含 ONNX Runtime 和声纹匹配档案，但**不包含声纹模型权重**。当前唯一接受的官方模型包为：

| 项目 | 值 |
| --- | --- |
| Release 标签 | `voice-model-campplus-1.0.0-ms1` |
| 文件名 | `meetsieve-voice-campplus-1.0.0-ms1.zip` |
| 上游模型 | `iic/speech_campplus_sv_zh-cn_16k-common` |
| MeetSieve 包版本 | `1.0.0-ms1` |
| 压缩包大小 | `25,788,529` 字节 |
| 压缩包 SHA-256 | `a5a49b38a76f0778ddd01ee2219518aaccbe32a97966108a1a481d5aed1da45c` |
| 模型许可证 | Apache-2.0 |

模型有两种安装方式：

1. 联网安装：打开“设置 → 声纹模型”，点击“下载官方模型”；
2. 离线安装：从[模型 Release](https://github.com/liuzhiyong-alt/meet-sieve/releases/tag/voice-model-campplus-1.0.0-ms1)下载 ZIP，复制到使用 MeetSieve 的电脑，再打开“设置 → 声纹模型 → 导入离线模型包”。

模型包与操作系统无关，macOS 和 Windows 使用同一个 ZIP。不要解压 ZIP，也不要重命名包内文件。MeetSieve 会校验压缩包大小、SHA-256、manifest、模型文件和许可证；任何一项不匹配都不会安装。模型仅在本机离线推理，会议进行中不会临时下载模型。

安装模型后：

- 在“常用小组”中创建成员和小组；
- 为需要辅助识别的成员录入建议 10～30 秒的清晰语音，保持稳定距离、尽量避免重叠说话和背景噪声；
- 声纹样本只保存在本机。人工修改说话人不会自动把片段加入永久声纹，必须由用户明确确认。

没有模型时仍可录音、转写和管理成员，但不能录入声纹或使用自动说话人辅助识别。声纹结果不是身份认证，低置信度内容会保留为未知。

#### 4.4 Codex 与会议纪要（可选）

MeetSieve 不内置 Codex，也不管理 Codex 账号。要使用会中 AI 和会后纪要功能，需要先在当前系统用户下安装 Codex CLI 并登录：

```bash
npm install --global @openai/codex
codex login
codex login status
```

`codex login` 默认打开浏览器完成 ChatGPT 登录，也可按 Codex 官方支持的方式使用 API Key。详见 [Codex CLI](https://developers.openai.com/codex/cli/) 和 [Codex authentication](https://developers.openai.com/codex/auth/)。

然后在“设置 → Codex”中：

- “可执行文件路径”留空时使用系统 `PATH` 中的 `codex`；如果桌面应用找不到，可填写绝对路径；
- 点击“重新检测”，确认登录状态和协议状态；
- 设置 AI 唤醒词，默认是“AI 助手”，建议使用 3～8 个中文字符；
- 如需语音唤醒，先保存火山 APP Key 和麦克风，再完成 3 次真实唤醒测试。

MeetSieve 沿用用户原生 Codex 登录、模型、MCP、Apps、sandbox 和审批配置。工具需要审批时，请由主持人在桌面端判断是否允许。Codex 不可用不会阻止本地录音和保存。

在“设置 → 会议纪要”中可以调整内容重点、详略程度和表达方式。修改只影响后续生成的纪要；纪要必须由用户主动生成，并在发布前人工确认事实、负责人和日期。

### 第 5 步：开始第一场会议

建议第一次先创建一场 1～2 分钟的短会议，验证麦克风、实时转写和会后收尾。

1. 在首页选择常用小组或本场参会人；
2. 进入创建会议页，填写可选主题和背景，确认麦克风，并按需开启局域网访客入口；
3. 点击“开始会议”，确认界面分别显示录音、实时转写和 Codex 状态；
4. 如开启局域网入口，让同一可信局域网内的参会者扫描二维码或打开链接。访客可以发送文字、链接和附件；结束会议后入口立即失效；
5. 需要 AI 时，在保存的唤醒词后说出问题，或在桌面端输入问题。Codex 的工具调用可能要求主持人审批；
6. 点击“结束会议”，等待本地录音校验、尾部转写、缺口登记和 Codex 结束同步分别完成；
7. 在会议详情中检查原始记录，按需修正文字、说话人或时间边界；
8. 主动生成会议纪要，人工复核后再复制、导出或通过 Codex 的现有工具继续处理。

请在录音前取得参会者同意，并遵守所在地关于录音、个人信息和生物识别信息的法律与组织制度。

### 第 6 步：了解数据保存与卸载

| 数据 | 去向 |
| --- | --- |
| 原始录音、SQLite、附件、声纹样本、会议 Markdown | 用户选择的本地会议工作目录 |
| 声纹模型 | 系统当前用户的 MeetSieve 应用数据目录，本机离线推理 |
| 实时/补录音频 | 启用相应功能时发送到用户配置的火山引擎语音识别服务 |
| Codex 上下文 | 使用 AI 功能时交给用户本机 Codex，并遵循该用户的 Codex 配置与账号策略 |
| 局域网消息和附件 | 会议期间由主持人电脑在可信私有网络内接收，并写入本场会议目录 |

系统应用数据目录：

- macOS 应用数据：`~/Library/Application Support/MeetSieve`；
- macOS 日志：`~/Library/Logs/MeetSieve`；
- Windows 配置：`%APPDATA%\MeetSieve`；
- Windows 模型等应用数据：`%LOCALAPPDATA%\MeetSieve`；
- Windows 日志：`%LOCALAPPDATA%\MeetSieve\logs`。

卸载程序默认只删除应用本体、快捷方式和安装器创建的系统项，**不会删除**会议工作目录、配置、日志或用户模型。完全清理前请先备份需要保留的会议，再手动删除上述目录。完整说明见[安装卸载与数据保留](docs/guide/安装卸载与数据保留.md)。

## 从源码运行与打包

这一部分面向需要修改、验证或自行构建 MeetSieve 的开发者。

### 第 1 步：准备开发环境

项目固定使用：

- Go `1.25.9`；
- Node.js `24.18.0`；
- pnpm `11.4.0`；
- Wails `v2.13.0`；
- mise，用于安装和切换 Go、Node.js、pnpm；
- Git、Make 和可访问依赖源的网络；
- macOS 开发还需要 Xcode Command Line Tools；
- Windows 安装包构建还需要 Docker，并能运行 `linux/amd64` 容器。

当前仓库的桌面开发与 macOS 打包链以 macOS arm64 为已配置宿主；Windows amd64 安装包通过固定 Docker 工具链交叉构建。仓库尚未把 Windows 原生 PowerShell 开发流程作为发布门禁，请不要把未经验证的原生命令写成已支持流程。

### 第 2 步：拉取源码并启动

```bash
git clone https://github.com/liuzhiyong-alt/meet-sieve.git
cd meet-sieve

# 先安装 mise：https://mise.jdx.dev/getting-started.html
mise install
make bootstrap
make assets
make dev
```

`make bootstrap` 会安装项目锁定的 Wails，并按 `frontend/pnpm-lock.yaml` 恢复前端依赖；`make assets` 会下载并校验当前平台和 Windows 构建需要的 ONNX Runtime。声纹模型不会被放入源码或安装包，开发运行后仍需在设置页下载或导入。

开发模式首次启动也会要求选择会议工作目录。真实 ASR、麦克风、Codex 和声纹模型不会由测试假数据替代，需要按[普通用户的设置步骤](#第-4-步按需要完成设置)分别完成真实配置。

### 第 3 步：运行验证

常用验证命令：

```bash
make test
make test-race
make test-contract
make lint
make typecheck
```

真实 ASR 端到端测试会使用默认麦克风和真实火山凭据，必须显式开启：

```bash
MEETSIEVE_ASR_REAL=1 \
MEETSIEVE_VOLC_API_KEY='你的 APP Key' \
make test-asr-real
```

不要把 APP Key 写入脚本、提交到仓库或粘贴到 Issue 日志中。

### 第 4 步：打包 macOS arm64

必须在 macOS arm64 上构建，系统需要 `codesign` 和 `hdiutil`。`BUILD_VERSION` 使用语义化版本；`WINDOWS_FILE_VERSION` 必须是四段数字，并用于统一校验双平台构建身份。

```bash
mise install
make bootstrap
make package-macos \
  BUILD_VERSION=0.1.0-alpha.1 \
  WINDOWS_FILE_VERSION=0.1.0.1
make verify-macos-package \
  BUILD_VERSION=0.1.0-alpha.1 \
  WINDOWS_FILE_VERSION=0.1.0.1
```

产物：

```text
build/bin/MeetSieve.app
build/bin/MeetSieve-0.1.0-alpha.1-macos-arm64.dmg
build/bin/SHA256SUMS.txt
```

当前流程最后执行 ad-hoc codesign，用于本机构建完整性，不等同于 Apple Developer ID 签名和公证。正式公开分发前应另行配置开发者签名、公证和 Release 密钥管理。

### 第 5 步：打包 Windows amd64

Windows 包使用 Docker 中固定的 Go、MinGW-w64、Wails、NSIS 和 7-Zip 工具链。推荐在 macOS 或 Linux 的 POSIX shell 中运行；Apple Silicon 主机需要 Docker 支持 `linux/amd64` 模拟。

```bash
mise install
make bootstrap
make package-windows \
  BUILD_VERSION=0.1.0-alpha.1 \
  WINDOWS_FILE_VERSION=0.1.0.1
make verify-windows-package \
  BUILD_VERSION=0.1.0-alpha.1 \
  WINDOWS_FILE_VERSION=0.1.0.1
```

产物：

```text
build/bin/MeetSieve.exe
build/bin/MeetSieve-0.1.0-alpha.1-windows-amd64-installer.exe
build/bin/SHA256SUMS.txt
```

`make verify-windows-package` 会检查 PE 架构、GUI subsystem、NSIS 安装契约、ONNX Runtime、许可证、声纹匹配档案以及安装包不含模型权重。交叉构建成功不能替代 Windows 真机的安装、升级、防火墙、WebView2、启动和卸载验证。

### 第 6 步：执行发布前检查

```bash
make test
make test-race
make test-contract
make lint
make typecheck
make package-macos BUILD_VERSION=<version> WINDOWS_FILE_VERSION=<a.b.c.d>
make verify-macos-package BUILD_VERSION=<version> WINDOWS_FILE_VERSION=<a.b.c.d>
make package-windows BUILD_VERSION=<version> WINDOWS_FILE_VERSION=<a.b.c.d>
make verify-windows-package BUILD_VERSION=<version> WINDOWS_FILE_VERSION=<a.b.c.d>
make verify-package BUILD_VERSION=<version> WINDOWS_FILE_VERSION=<a.b.c.d>
```

随后必须在目标 macOS arm64 与 Windows amd64 真机上分别执行安装、首次启动、麦克风授权、短会议、覆盖升级和卸载数据保留验证，再上传 DMG、NSIS 安装器与 `SHA256SUMS.txt` 到同一个应用版本 Release。声纹模型继续使用独立模型 Release，不要重复放入每个客户端安装包。

## 参与贡献

欢迎通过 Issue 报告问题或讨论改进。提交 Pull Request 前请：

1. 保持修改范围聚焦，不提交会议数据、凭据、模型二进制或构建缓存；
2. 使用项目锁定的 mise 工具版本；
3. 为行为修改补充可重复验证，并运行相关测试、lint 和类型检查；
4. 涉及 UI 时遵循 `docs/style/meet-sieve/` 的设计契约；
5. 涉及安装包时说明目标平台，并提供相应实机验证证据；
6. 不用假数据或跳过关键门禁来宣称真实音频、ASR、Codex、声纹或跨平台链路已完成。

## 许可证

MeetSieve 源代码使用 [Apache License 2.0](LICENSE)。第三方组件和声纹模型保留各自许可证；发布安装包和模型包时必须同时保留对应的 `LICENSE`、`NOTICE` 和第三方声明。
