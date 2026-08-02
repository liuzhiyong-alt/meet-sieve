# MeetSieve Design System Changelog

<!-- markdownlint-disable MD024 -->

## 1.1.0 - 2026-08-02

### Added

- 将独立原始记录校对工作台登记为正式页面级金标；
- 登记单条文字/说话人校对、本场未知说话人批量校对、音频回放和独立加入声纹模式；
- 明确批量覆盖、revision 冲突、SQLite 已保存但 Markdown 刷新失败和最小窗口行为。

### Changed

- `TranscriptEditor`、单条/批量校对和加入声纹成熟度从 `design-required` 调整为 `existing`；
- 校对相关可见状态以 Step 5 已确认方案为事实源，不得合并成模糊成功状态。

## 1.0.1 - 2026-07-31

### Changed

- Windows 安装与卸载改用 NSIS Modern UI 平台原生页面，不再维护品牌化 Web 视觉金标；
- 登记自定义安装目录、覆盖升级、运行实例阻断、安全卸载和用户数据保留模式；
- 明确工作目录不得位于应用安装目录或 macOS `.app` 包内部；
- 统一安装位置、防火墙、运行中阻断和卸载数据保留文案。

## 1.0.0 - 2026-07-31

### Added

- 从权威 MeetSieve HTML/CSS 提炼浅色视觉语言和语义 token；
- 建立颜色、排版、间距、圆角、阴影、动效和跨平台字体规则；
- 建立桌面客户端与电脑 Web 优先 LAN 页的布局契约；
- 登记 25 个 existing、derived 或 design-required 组件；
- 建立会议生命周期、本地保存、实时转写、补转写、Codex、纪要、LAN、附件、
  声纹、工作目录、删除和安装状态模式；
- 建立中文 Content Design、WCAG 2.2 AA 基线和视觉金标；
- 建立 Foundations、Components 和 Product Patterns 静态预览；
- 建立设计与前端完成门和项目 `AGENTS.md` 使用约束。

### Known issues

- 当前 UI 的 12px Meta 颜色存在对比度不足，既有视觉调整需要用户确认；
- LAN 访客页的电脑 Web 主布局尚未设计；
- 会议收尾、崩溃恢复、校对和纪要版本仍标记为 `design-required`；
- 跨平台图标来源尚未在 Vue 实施阶段选定。
