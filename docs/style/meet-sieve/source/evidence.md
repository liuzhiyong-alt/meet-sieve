# MeetSieve Design System 证据

## 1. 权威来源

### 功能和状态

- `docs/spec/技术方案.md`
- 重点：会议正交状态、会中流程、补转写、Codex、LAN、删除语义和 Alpha 验收标准。

### 已确认视觉

- `docs/UI/index.html`
- `docs/UI/start.html`
- `docs/UI/live.html`
- `docs/UI/records.html`
- `docs/UI/detail.html`
- `docs/UI/people.html`
- `docs/UI/settings.html`
- `docs/UI/onboarding.html`
- `docs/UI/guest.html`
- `docs/UI/step6-proposal/`
- `docs/UI/step7-proposal/`
- `docs/UI/step8-proposal/`
- `docs/UI/assets/meetsieve.css`
- `docs/UI/assets/meetsieve.js`

### 基础参考

- `docs/style/apple/DESIGN.md`
- `docs/style/apple/tokens.css`
- `docs/style/apple/components.manifest.json`

Apple 来源用于解释中性色、蓝色强调、字体职责、圆角、克制阴影和动效，不构成
MeetSieve 页面或组件权威。

## 2. 非权威文件

以下内容只保存生成过程、历史或辅助信息：

- `docs/UI/*.artifact.json`
- `docs/UI/meetsieve-product.html`
- `docs/UI/ms8hmvoz-image.png`
- `docs/UI/ms8hof4r-image.png`
- `docs/UI/critique.json`
- `docs/UI/plugin-source/`
- `docs/UI` 内附带的 PRD、用户旅程、信息架构和技术方案副本

这些文件存在旧需求口径、深浅色或布局差异，不能用于覆盖当前分页面 HTML。

## 3. Token 来源映射

| MeetSieve 领域 | 当前 UI 证据                          | Apple 基础         |
| -------------- | ------------------------------------- | ------------------ |
| 白/浅灰表面    | `--bg`、`--surface`、`--surface-warm` | Neutral canvas     |
| 近黑正文       | `--fg`、`--fg-2`                      | Near-black ink     |
| 单一蓝色       | `--accent` 及 Hover/Active            | Apple action blue  |
| 状态色         | `--success`、`--warn`、`--danger`     | MeetSieve 产品补充 |
| 字体           | Display/Body/Mono stacks              | SF Pro 职责分离    |
| 间距           | 4–48px                                | 8px 主节奏 + 精调  |
| 圆角           | 8/12/18/Pill                          | 分级几何           |
| 阴影           | Flat/Ring/Raised                      | 克制深度           |
| 动效           | 150/220ms + 标准曲线                  | 快速减速、无弹跳   |

## 4. MeetSieve 独有证据

以下不是 Apple 通用组件，而是从当前产品形成的语言：

- AppShell：232px Sidebar + 52px Titlebar；
- 会中 LiveStage；
- 红色 Recording Dot + Mono Clock + AudioMeter；
- 独立系统状态面板；
- Unified Timeline；
- Meeting/ASR/Codex/LAN 状态标签；
- 工作目录迁移三阶段；
- 会议详情的纪要、原始记录、会议消息三视图；
- 永久删除整场会议的会议号确认；
- 会议收尾 OperationSteps 与独立系统状态；
- 补转写冲突双证据工作台；
- 不可变纪要版本工作区与历史恢复。

## 5. 已确认推导边界

- 局部状态可使用现有组件直接组合；
- 新页面、新信息架构、关键流程和复杂不可逆操作先补设计；
- 用户可见变化先确认；
- 无视觉变化的工程与可访问性改进可直接实施；
- macOS/Windows 共用设计语言，有限平台适配；
- LAN Web 电脑优先、手机兼容；
- v1 仅浅色；
- 使用单一跨平台图标家族；
- 当前页面进入视觉回归。

## 6. 当前 UI 的已知例外

以下内容不应被“静态页面完全复制”带入业务实现：

- `live.html` 中的“最小化到托盘”与技术方案非目标冲突，不实现；
- `guest.html` 的 430px 单栏不符合已确认的电脑 Web 主要场景，需要先补桌面设计；
- 会中系统状态缺少独立实时转写项，按产品模式补为 `derived`；
- 崩溃恢复属于技术方案范围，不能因现有页面缺失而省略；会议收尾、补转写冲突和纪要
  版本使用 Step 8 已确认页面级金标；
- 当前 `--meta` 在 12px 普通文字上的对比度不足，新设计不继续扩散，既有调整需确认。
- 旧 `installer.html`、`uninstall.html` 已退出权威视觉范围；Windows 安装和卸载使用 NSIS 平台原生页面。
