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

| Surface           | 尺寸       | 用途                      |
| ----------------- | ---------- | ------------------------- |
| 桌面宽设计        | 1440 × 900 | 与当前主要设计空间对比    |
| 默认 Wails        | 1280 × 800 | 主要验收                  |
| 最小 Wails        | 1024 × 720 | 最小窗口可用性            |
| LAN Desktop       | 1366 × 768 | 新 LAN 桌面设计确认后启用 |
| LAN Mobile Compat | 390 × 844  | 手机扫码兼容              |

当前 `guest.html` 的视觉元素是证据，但 430px 单栏布局不是 LAN Desktop 最终金标。

## 3. 功能例外

视觉金标不能覆盖更高优先级的技术方案。当前页面存在以下明确例外：

- `live.html` 的“最小化到托盘”不进入实现，技术方案明确不提供托盘驻留；
- `guest.html` 的颜色、组件和内容是证据，最终布局改为电脑 Web 优先并先补设计；
- 会中页当前未单列实时转写状态，实现时按 `PRODUCT-PATTERNS.md` 补齐；
- 缺失的收尾、崩溃恢复、校对和纪要版本不能因金标中没有而省略。
- `transcript-editor.html` 是校对工作台金标；真实数据、错误和冲突状态按
  `PRODUCT-PATTERNS.md` 派生，不得用静态文字证明声纹链路完成。
- Windows 安装和卸载使用 NSIS 平台原生页面，不建立 MeetSieve 页面级视觉金标。

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
