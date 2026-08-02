# MeetSieve 设计与前端质量门禁

页面或组件只有通过本清单，才能声明设计还原或 UI 实现完成。

## 1. 权威与范围

- [ ] 功能和状态符合 `docs/spec/技术方案.md`。
- [ ] 已有页面以权威 HTML/CSS 为视觉金标。
- [ ] 未使用截图、`.artifact.json` 或 `meetsieve-product.html` 替代金标。
- [ ] 未设计功能已在 `PRODUCT-PATTERNS.md` 登记。
- [ ] `design-required` 已先完成设计确认。
- [ ] 没有实现 `blocked` 功能。

## 2. Token

- [ ] 业务组件没有原始 Hex/RGB/HSL。
- [ ] 没有自创阴影、圆角和动效曲线。
- [ ] 间距优先使用 token；非 token 值只存在于已登记组件内部。
- [ ] 没有复制 Apple 未使用的营销 token。
- [ ] 没有新增暗色 token 或自动暗色主题。

允许的例外必须有注释和设计系统变更记录。

## 3. Components

- [ ] 优先复用 `COMPONENTS.md` 中的组件。
- [ ] 新组件已登记到 `components.manifest.json`。
- [ ] 没有通过页面局部覆盖制造近似变体。
- [ ] 异步组件覆盖 Loading、Success、Error、Disabled。
- [ ] Primary Button 在同一页面或操作区保持唯一。
- [ ] Danger Action 明确对象和永久后果。

## 4. Product States

- [ ] 录音、本地保存、实时转写、Codex 和 LAN 独立显示。
- [ ] 处理中没有表述为已完成。
- [ ] 外部依赖失败没有掩盖本地事实状态。
- [ ] 缺口、冲突、部分删除和上传失败有持续处理入口。
- [ ] 错误发生位置附近能看到原因和恢复动作。
- [ ] 页面刷新后状态可由后端重建，前端状态不是事实源。

## 5. Layout

- [ ] 1440 × 900 无明显偏差。
- [ ] 1280 × 800 主任务完整可见。
- [ ] 1024 × 720 可操作且无核心横向滚动。
- [ ] 录音状态和结束会议入口始终可见。
- [ ] Sidebar、Titlebar、Content 符合 `LAYOUT.md`。
- [ ] LAN Web 以电脑浏览器布局为主，390px 下仍可用。
- [ ] Windows 没有假的 macOS 窗口控件。

## 6. Typography 与 Content

- [ ] 页面只有一个 H1。
- [ ] 时间、会议号和路径使用 Mono 与 tabular numbers。
- [ ] 按钮文案单行且动作明确。
- [ ] 使用统一产品术语。
- [ ] 成功、处理中、失败和停止没有混用。
- [ ] AI 内容与人类事实有明确标签。
- [ ] 长主题、长成员名、长文件名不会破坏布局。

## 7. Accessibility

- [ ] 新界面满足 WCAG 2.2 AA 对比度。
- [ ] 所有操作支持键盘。
- [ ] Focus 可见。
- [ ] IconButton 和 Switch 有可访问名称。
- [ ] Dialog 有焦点锁定、背景 inert 和焦点恢复。
- [ ] 动态状态有适当 Live Region。
- [ ] 状态不只靠颜色。
- [ ] 支持减少动态效果。

## 8. Visual Regression

- [ ] 使用 `VISUAL-BASELINES.md` 规定尺寸。
- [ ] 固定夹具覆盖正常、空、加载、失败和禁用。
- [ ] 截图差异经过人工检查，不能只看像素阈值。
- [ ] 平台字体差异未被误判为布局正确或错误。
- [ ] 视觉夹具没有被用来证明真实业务链路通过。

## 9. Design System Change

如果本次修改设计系统：

- [ ] 更新版本号。
- [ ] 更新 Markdown 规则。
- [ ] 更新 `tokens.css` 和 `design-tokens.json`。
- [ ] 更新 `components.manifest.json`。
- [ ] 更新对应 Preview。
- [ ] 更新视觉基准。
- [ ] 用户可感知变化已取得确认。

## 10. 明确禁止

- [ ] 未使用 Tailwind。
- [ ] 未引入通用 UI 组件库。
- [ ] 未混用多个图标家族。
- [ ] 未使用 Emoji 或 Unicode 充当正式图标。
- [ ] 未为视觉丰富添加无业务意义动画。
- [ ] 未使用假数据伪装真实链路完成。
