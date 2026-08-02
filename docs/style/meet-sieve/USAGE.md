# MeetSieve Design System 使用说明

## 开发前阅读顺序

涉及前端页面、组件、交互、文案或视觉评审时，按顺序读取：

1. `README.md`
2. `DESIGN.md`
3. `FOUNDATIONS.md`
4. `LAYOUT.md`
5. `COMPONENTS.md`
6. 与当前功能有关的 `PRODUCT-PATTERNS.md`
7. `CONTENT.md`
8. `ACCESSIBILITY.md`
9. `QUALITY-GATES.md`

已有 UI 页面还需要查看对应 `docs/UI/*.html`。不要用截图、`.artifact.json` 或
`meetsieve-product.html` 替代权威页面。

## 实现已有页面

1. 以权威 HTML/CSS 的视觉结果为页面金标；
2. 使用 `tokens.css`，不要从 `docs/UI/assets/meetsieve.css` 复制零散数值；
3. 按 `COMPONENTS.md` 抽成 Vue 组件，不要求复制静态 DOM；
4. 后端状态必须映射到 `PRODUCT-PATTERNS.md`；
5. 在 `VISUAL-BASELINES.md` 规定的尺寸验证截图；
6. 通过 `QUALITY-GATES.md` 后才可声明页面完成。

## 实现未设计功能

先查 `PRODUCT-PATTERNS.md` 的成熟度：

- `existing`：复用现有 UI，可直接实现；
- `derived`：按指定现有组件组合，可直接实现；
- `design-required`：先补设计并由用户确认；
- `blocked`：产品或技术行为未确定，不得实现。

如果一个功能未登记，默认按 `design-required` 处理，不能自行发明新页面模式。

## 允许的工程调整

可以直接进行：

- Vue 组件拆分、DOM 语义化、状态管理和数据绑定；
- 不改变视觉的 ARIA、焦点管理、键盘操作和减少动态效果支持；
- 使用真实数据替换静态示例；
- 把重复内联样式收敛到已登记组件或 token；
- 在现有布局内补齐 `derived` 状态。

必须先确认：

- 可见颜色、字号、尺寸、间距、圆角、阴影和布局变化；
- 页面信息架构和主流程变化；
- 新页面、新组件家族和新的交互范式；
- 深色模式；
- 对现有文案语义的改变。

## 禁止事项

- 不使用 Tailwind 或通用 UI 组件库。
- 不在业务组件中硬编码原始颜色、阴影和圆角。
- 不通过页面局部覆盖制造未登记组件变体。
- 不从 Apple 设计包直接复制组件。
- 不使用假数据证明真实业务链路已经完成。
- 不因工程方便隐藏录音状态、结束会议入口或失败原因。

## 变更设计系统

变更必须同步：

1. `tokens.css` 或相应 Markdown；
2. `design-tokens.json`；
3. `components.manifest.json`；
4. `preview/`；
5. `VISUAL-BASELINES.md` 或页面金标；
6. 版本号和变更说明。

用户可感知变化必须先获得确认。
