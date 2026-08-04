# MeetSieve Design System Changelog

<!-- markdownlint-disable MD024 -->

## 1.5.0 - 2026-08-03

### Added

- 登记中断恢复和整场会议删除失败恢复页面级金标；
- 登记会议记录游标分页、会议详情危险区、附件完整性和未保存离开状态；
- 登记设置“通用”中的工作目录待重启、存储占用和脱敏诊断；
- 登记小组详情、成员详情和独立声纹生命周期页面级金标；
- 新增 `CursorPagination`、`DangerZone`、`RecoverySummary`、
  `StorageBreakdown` 和 `FileIntegrityState` 组件契约。

### Changed

- `interrupted`、删除录音和工作目录待重启成熟度调整为 `existing`；
- 会议记录 Alpha 删除批量选择，只保留搜索、独立筛选和单场处理；
- 工作目录改为下次启动切换，不再使用迁移步骤、复制或合并语义；
- 明确部分删除不回滚、只重试原 manifest 剩余项，且不能报告完整成功。

## 1.4.0 - 2026-08-03

### Added

- 登记会议收尾处理中与收尾失败页面级金标；
- 登记补转写冲突独立校对工作台及三种人工决策；
- 登记纪要当前版本、只读历史版本和恢复为新版本页面级金标。

### Changed

- `OperationSteps`、补转写 `conflict` 和 `MinuteVersion` 成熟度调整为 `existing`；
- 明确本地核心保存不被补转写或 Codex 同步阻塞，也不得被外部成功状态替代；
- 明确纪要人工保存、确认、重新生成和历史恢复均遵守不可变版本规则。

## 1.3.0 - 2026-08-02

### Added

- 登记会中 AI busy、busy long、failed 和主持人原生审批页面级金标；
- 登记 Codex Native Approval Modal，固定展示工具、目标、操作摘要和风险；
- 登记 Codex 设置中的原生权限继承、协议检测与三次唤醒测试状态。

### Changed

- Codex `busy`、`busy_long`、`unavailable` 和 `approval_pending` 调整为 `existing`；
- 明确成功最终回答自动公开到 LAN，问题、partial、失败和取消不公开；
- 删除“MeetSieve 只读会议目录”旧口径，改为沿用 Codex 原生配置且审批仅由主持人处理。

## 1.2.0 - 2026-08-02

### Added

- 登记 LAN Desktop、手机兼容访客页、宿主网卡选择、会中入口与入口停止页面级金标；
- 登记上传中结束会议的“等待上传完成 / 结束并取消上传”明确后果。

### Changed

- LAN Desktop 与 UploadItem 成熟度调整为 `existing`；
- 删除无法兑现的会后恢复访客上传语义；会议结束立即撤销入口并取消未完成上传；
- LAN 页面以 1366 × 768 为主要验收，390 × 844 为手机兼容验收。

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
