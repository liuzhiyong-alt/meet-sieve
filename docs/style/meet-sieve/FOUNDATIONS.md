# MeetSieve 基础视觉

`tokens.css` 是实现值的唯一事实源；本文解释这些值的使用边界。设计稿、预览页和
Vue 组件都应引用相同 token。

## 1. Color

### 1.1 表面

| 语义 token         | 用途                                    |
| ------------------ | --------------------------------------- |
| `--ms-bg-page`     | 应用外层和页面衬底                      |
| `--ms-bg-canvas`   | 主工作画布、普通卡片、字段              |
| `--ms-bg-sidebar`  | 侧栏、轻强调卡片、会中状态台            |
| `--ms-bg-subtle`   | Tabs、Avatar、空闲 Status、次级分组     |
| `--ms-bg-emphasis` | 激活导航、深色 Toast 等高对比小面积元素 |

不通过增加更多浅灰制造层级。优先顺序是留白、分隔线、背景变化，最后才是阴影。

### 1.2 文字

| 语义 token            | 用途                           |
| --------------------- | ------------------------------ |
| `--ms-text-primary`   | 标题、正文重点、关键数值       |
| `--ms-text-secondary` | 字段标签、说明中的重点         |
| `--ms-text-muted`     | 帮助、页面说明、非关键正文     |
| `--ms-text-meta`      | 时间、会议号、路径等辅助元数据 |

`--ms-text-meta` 沿用当前 UI，但在 12px 普通文字上的对比度不足。现有页面暂不静默
改色；新界面应优先使用 `--ms-text-muted`，或在确认后调整 token。详见
`ACCESSIBILITY.md`。

### 1.3 操作色

- 蓝色只表示主要操作、选中、焦点、链接和 AI 参与。
- 同一视区不应出现多个竞争性的蓝色主按钮。
- Hover 使用更亮蓝，Active 使用更深蓝。
- 蓝色不等于成功。

### 1.4 状态色

| 状态                 | 色彩           | 允许范围                     |
| -------------------- | -------------- | ---------------------------- |
| 健康、完成           | Success Green  | 圆点、Status、Notice、小图标 |
| 需要关注、可继续     | Warning Yellow | Status、Notice、进度阶段     |
| 录音、失败、永久危险 | Danger Red     | 录音点、错误、危险按钮       |
| 处理中、AI、信息     | Action Blue    | Progress、AI 事件、信息状态  |
| 空闲、未知、停止     | Neutral Gray   | 默认 Status 和辅助文字       |

必须同时提供文本或图标，不能只靠颜色。

## 2. Typography

### 2.1 字体

- Display：页面标题、卡片标题、状态台标题。
- Body：正文、控件、导航、帮助信息。
- Mono：会议号、时间、路径、版本、技术状态和稳定标识。

macOS 使用系统 SF 系列；Windows 使用 Segoe UI Variable/Segoe UI。不得为了两端
字形完全相同而内置无许可证字体。

### 2.2 层级

| 角色               | Token / 尺寸 | 典型用途                     |
| ------------------ | ------------ | ---------------------------- |
| Page Heading       | 32–40px      | 页面唯一 H1                  |
| Section Heading    | 28px         | 主要内容区 H2                |
| Dialog/Panel Title | 21px         | Modal、重要面板标题          |
| Body Emphasis      | 17px         | 页面说明、时间线正文         |
| Control / Default  | 14px         | 按钮、导航、普通业务内容     |
| Micro / Meta       | 12px         | 状态、字段标签、会议号和帮助 |

- 页面只能有一个 H1。
- Display 标题使用紧凑行高与负 tracking；正文不复制该 tracking。
- 12px 只能用于辅助信息，不能承载关键操作或长段正文。
- 时间、路径和数值使用 tabular numbers。

## 3. Spacing

基础节奏是 4px，正式 token 为 `4 / 8 / 12 / 16 / 20 / 24 / 32 / 48`。

常用规则：

- 同一控件内部：8px；
- 相关字段或列表内容：12px；
- 组件内部 Padding：16px 或 20px；
- 重要面板和 Modal：24px；
- 页面内容 Padding：32px；
- 页面主要区域间距：24px 或 32px。

当前 UI 中的 `7px`、`10px`、`13px`、`15px` 等属于已有组件精调值，由组件规范
持有。业务页面不得因此新增通用 spacing token。

## 4. Radius

| Token                 | 用途                                   |
| --------------------- | -------------------------------------- |
| `--ms-radius-sm` 8px  | Tab、导航项、紧凑控件                  |
| `--ms-radius-md` 12px | 字段、Notice、Toast、Switch 分组       |
| `--ms-radius-lg` 18px | Card、Modal、会中状态台                |
| `--ms-radius-pill`    | Button、Status、Progress、Switch Track |
| `50%`                 | Avatar、IconButton、录音点             |

圆角按组件职责选择，不允许在业务页面随意创造 6px、14px、20px 等近似变体。

## 5. Border

- 默认 1px。
- 普通卡片和行分隔使用 Soft Border。
- 输入框、Quiet Button 和重要卡片使用 Default Border。
- Focus、Error、Selected 由状态样式叠加，不通过加粗边框导致布局抖动。

## 6. Elevation

| 层级   | 用途                              |
| ------ | --------------------------------- |
| Flat   | 页面、卡片、侧栏                  |
| Ring   | 选中 Tab 等需要一层精确边界的控件 |
| Raised | Modal、Toast、浮动菜单            |

禁止为普通 Card 增加阴影。禁止 Material 式多层阴影、外发光和拟物悬浮。

## 7. Motion

- Fast `150ms`：Hover、Focus、颜色和边框变化；
- Base `220ms`：Toast、Modal 和小范围位移；
- Easing：`cubic-bezier(0.28, 0, 0.22, 1)`；
- Active：按钮可缩放到 `0.98`；
- 录音脉冲约 1.8 秒，必须支持 reduced motion；
- 进度来自真实状态，不用循环动画伪装未知任务。

## 8. Iconography

v1 统一使用单一跨平台线性图标来源，具体依赖在 Vue 组件实施前验证。

- 常规图标 16px；
- 重要操作 18px；
- 默认视觉线宽 1.5–2px；
- 图标与文字间距 8px；
- 图标按钮必须有可访问名称和 Tooltip；
- 不混用 SF Symbols、Emoji、Unicode 符号和多个图标库；
- 不为了装饰给当前纯文字导航新增图标；
- 若不引入运行时依赖，只打包实际使用且许可证清晰的 SVG。

## 9. Control Size

- 紧凑 Tab：36px；
- 普通按钮和 IconButton：40px；
- Choice 和重要点击行：至少 44px；
- Switch：42 × 24px；
- Avatar：36px；
- Status 圆点：7px；
- 录音点：9px，属于产品模式而非通用 Status。

## 10. 浅色主题

v1 只支持浅色主题。不得根据系统偏好自动反转颜色。未来深色模式必须单独完成
token、组件状态、对比度和页面视觉基准设计。
