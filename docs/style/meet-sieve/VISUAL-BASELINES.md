# MeetSieve 视觉基准

## 1. 金标来源

页面级视觉金标只来自：

- `docs/UI/index.html`
- `docs/UI/start.html`
- `docs/UI/live.html`
- `docs/UI/records.html`
- `docs/UI/detail.html`
- `docs/UI/people.html`
- `docs/UI/voice-enrollment.html`
- `docs/UI/settings.html`
- `docs/UI/model-management.html`
- `docs/UI/transcript-editor.html`
- `docs/UI/onboarding.html`
- `docs/UI/guest.html`
- `docs/UI/assets/meetsieve.css`
- `docs/UI/assets/meetsieve.js`

截图、`.artifact.json`、`meetsieve-product.html` 和 UI 目录中的旧产品文档不是金标。

## 2. 验证尺寸

| Surface           | 尺寸       | 用途                   |
| ----------------- | ---------- | ---------------------- |
| 桌面宽设计        | 1440 × 900 | 与当前主要设计空间对比 |
| 默认 Wails        | 1280 × 800 | 主要验收               |
| 最小 Wails        | 1024 × 720 | 最小窗口可用性         |
| LAN Desktop       | 1366 × 768 | Step 6 访客页主要验收  |
| LAN Mobile Compat | 390 × 844  | 手机扫码兼容           |

Step 6 LAN 页面级金标为 `docs/UI/step6-proposal/` 下六个已确认 HTML；原
`guest.html` 的 430px 单栏仅保留为早期视觉证据。

Step 7 Codex 页面级金标为 `docs/UI/step7-proposal/` 下四个已确认 HTML，覆盖会中忙碌、
主持人审批、失败和设置状态。原 `live.html`、`settings.html` 仍提供基础布局证据；权限、
审批和公开范围冲突时以 Step 7 金标与技术方案为准。

Step 8 页面级金标中的收尾处理中、收尾失败和补转写冲突继续有效。纪要当前版本与历史
版本页面仅保留为历史证据；2026-08-06 确认的单份纪要交互以技术方案、`MinuteDocument`
组件契约和运行时页面为准。

Step 9 页面级金标为 `docs/UI/step9-proposal/` 下九个已确认 HTML，覆盖中断恢复、删除失败
恢复、工作目录与存储诊断、会议记录分页、附件异常、未保存离开、小组详情和
成员详情。与旧 `records.html`、`detail.html`、`people.html`、`settings.html` 的批量、标题栏
删除或目录迁移行为冲突时，以 Step 9 金标与技术方案为准。

## 3. 功能例外

视觉金标不能覆盖更高优先级的技术方案。当前页面存在以下明确例外：

- `live.html` 的“最小化到托盘”不进入实现，技术方案明确不提供托盘驻留；
- `guest.html` 的颜色、组件和内容是证据，最终布局改为电脑 Web 优先并先补设计；
- 会中页当前未单列实时转写状态，实现时按 `PRODUCT-PATTERNS.md` 补齐；
- 缺失的崩溃恢复页面不能因金标中没有而省略；收尾和补转写冲突按 Step 8 已确认金标
  实现，纪要按 2026-08-06 简化后的单份文档交互实现。
- `transcript-editor.html` 是校对工作台金标；真实数据、错误和冲突状态按
  `PRODUCT-PATTERNS.md` 派生，不得用静态文字证明声纹链路完成。
- Windows 安装和卸载使用 NSIS 平台原生页面，不建立 MeetSieve 页面级视觉金标。
- 会议详情的 Codex 接续为 `derived` Modal：以 `detail.html` 页头居中的单一操作为入口证据，页头不再展示最高状态，并验证有 thread、无 thread、加载和错误四种局部状态。

## 4. 比较内容

必须保持：

- 页面结构和信息层级；
- 表面、颜色、字号和字重；
- 组件尺寸、间距、圆角和边框；
- 主操作位置；
- 录音、保存和错误等关键状态位置；
- 内容溢出、收列和滚动行为。

允许差异：

- Vue DOM 和静态 HTML 结构；
- 真实数据长度导致的合理高度变化；
- macOS/Windows 系统字体和窗口边缘渲染；
- 不改变视觉意图的 ARIA 和焦点管理；
- 经过确认的设计系统版本变更。

## 5. 测试夹具

视觉测试可以使用固定夹具稳定截图，但夹具只能验证 UI，不得作为真实音频、ASR、
Codex、LAN 或文件上传链路通过的证据。

至少准备：

- 正常短文本；
- 接近上限的长主题、长成员名和长文件名；
- 空状态；
- 加载、失败、重试和禁用；
- 1024 × 720 下的双栏收起；
- 平台路径和字体差异。

## 6. 新页面

- `existing` 页面以当前 HTML 为金标；
- `derived` 局部状态加入对应组件或产品模式预览；
- `design-required` 必须先生成确认后的页面级金标；
- `blocked` 不创建截图。

Step 6 已确认页面按以下组合验证：

- `start-lan.html`、`live-lan.html`、`live-upload-ending.html`：三种 Wails 尺寸；
- `guest-join.html`、`guest-active.html`、`guest-ended.html`：1366 × 768 与 390 × 844。

Step 7 已确认页面按以下组合验证：

- `live-agent-busy.html`、`live-agent-approval.html`、`live-agent-failed.html`：三种 Wails 尺寸；
- `settings-codex.html`：三种 Wails 尺寸，并检查长可执行文件路径和三次唤醒进度；
- 审批夹具覆盖长工具名、长目标、长操作摘要和无参数摘要，均不得泄漏原始工具参数；
- 10,000 UTF-8 bytes 问题只验证布局与计数提示，不作为后端限制通过的证据。

Step 8 已确认页面按以下组合验证：

- `finalizing.html`、`finalizing-failed.html`、`gap-conflict.html`、
  `minutes-workspace.html`、`minutes-history.html`：三种 Wails 尺寸；
- `1024 × 720` 保留收尾独立状态、冲突决策区、纪要当前状态和历史恢复主操作；
- 长冲突文字、长缺口范围、长版本来源和长纪要正文允许纵向滚动，不产生横向滚动；
- 静态夹具只验证布局，不作为真实收尾、火山补转写或 Codex 纪要链路通过的证据。

Step 9 已确认页面按以下组合验证：

- 九个 `step9-proposal` 页面均验证 1440 × 900、1280 × 800 和 1024 × 720；
- 中断恢复、删除恢复、小组与成员危险区和未保存弹窗在 1024 × 720 保留原因、后果与主操作；
- 存储扫描使用阶段状态，不使用虚假百分比，长路径和长文件名不得产生横向滚动；
- 会议记录覆盖首、中、末页和筛选重置；详情长列表覆盖游标前后翻页；
- 静态夹具只验证 UI，不作为删除、诊断、磁盘扫描或文件完整性链路通过的证据。

Codex 接续 Modal 验证 1440 × 900、1280 × 800 和 1024 × 720：长路径、长提示词和复制失败
允许在 Body 内纵向滚动，但不得产生核心横向滚动；Header 与右下“关闭”Footer 始终可见。无 thread
不得隐藏从会议文件继续的提示词与命令，两个接续方式一次只展示一种文本。
有 thread 时两个同级复制动作保持相同的 Primary Button 样式。

实时转写设置页的 APP Key 调整继续以 `docs/UI/settings.html` 和
`docs/UI/step4-proposal/settings-asr.html` 为金标，在三种 Wails 尺寸覆盖：未配置、已保存、
旧版凭证待更新、连接失败和会议进行中禁用。长 APP Key 只显示掩码，不得产生横向滚动；
静态夹具不能作为真实火山连接或音频转写通过的证据。
