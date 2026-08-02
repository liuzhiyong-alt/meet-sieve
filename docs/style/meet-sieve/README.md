# MeetSieve Design System

MeetSieve Design System 是会意桌面客户端与局域网访客 Web 页的设计和实现契约。
它从当前已确认 UI 中提炼视觉语言、组件和布局，并使用 Apple 设计规范解释基础
原则。它不是对现有 UI 的重新设计，也不是 Apple 页面组件的复制品。

## 权威顺序

发生冲突时按以下顺序判断：

1. `docs/spec/技术方案.md`：功能、业务状态、数据语义和流程；
2. `docs/UI/*.html`、`docs/UI/assets/meetsieve.css`、
   `docs/UI/assets/meetsieve.js`：已设计页面的视觉与交互基准；
3. 本目录：后续开发使用的设计契约；
4. `docs/style/apple/`：MeetSieve 未定义部分的基础参考。

`docs/UI` 中的 `.artifact.json`、截图、独立 `meetsieve-product.html` 和生成过程文档
只作为历史或来源证据，不是视觉权威。

## 设计定位

> 安静、可靠、克制、状态优先的本地桌面生产力工具。

MeetSieve 不追求营销页式视觉戏剧性。界面必须让录音、本地保存、实时转写、
Codex、LAN 和会后处理状态比装饰更醒目，同时保持 Apple 风格的留白、排版精度、
单一蓝色强调和克制层次。

## 文件导航

| 文件                       | 用途                                         |
| -------------------------- | -------------------------------------------- |
| `DESIGN.md`                | 设计理念、风格边界和 Do/Don't                |
| `USAGE.md`                 | 开发和评审时的必读顺序与使用方法             |
| `FOUNDATIONS.md`           | 颜色、字体、间距、圆角、阴影、动效和图标规则 |
| `tokens.css`               | 前端实现唯一允许使用的基础视觉值             |
| `design-tokens.json`       | 机器可读 token                               |
| `LAYOUT.md`                | 桌面客户端和 LAN Web 布局契约                |
| `COMPONENTS.md`            | 基础组件的 Anatomy、变体、状态和可访问性     |
| `PRODUCT-PATTERNS.md`      | 会议业务状态到 UI 表达的映射                 |
| `CONTENT.md`               | 中文文案、状态命名和格式规则                 |
| `ACCESSIBILITY.md`         | WCAG 2.2 AA 基线与现有问题处理方式           |
| `VISUAL-BASELINES.md`      | 页面级视觉金标和截图验证口径                 |
| `QUALITY-GATES.md`         | 开发、设计和代码评审完成门                   |
| `CHANGELOG.md`             | 设计系统版本与变更记录                       |
| `components.manifest.json` | 机器可读组件清单                             |
| `preview/`                 | 直接引用 `tokens.css` 的静态视觉基准         |
| `source/evidence.md`       | 当前 UI、技术方案和 Apple 参考的来源映射     |

## 当前版本

- 版本：`1.1.0`
- 主题：仅浅色
- 客户端：macOS、Windows 共用设计语言，有限平台适配
- LAN 页面：电脑 Web 优先、手机兼容
- 前端方式：Vue 3 + 自有轻量组件，不使用 Tailwind 或通用 UI 组件库

## 变更原则

- 已设计页面要求视觉高保真；DOM、Vue 组件、状态管理和工程结构可以合理重构。
- 不改变视觉的可访问性和工程改进可以直接实施。
- 用户可感知的颜色、尺寸、布局、文案和流程变化必须先确认。
- 局部缺失状态可按现有组件推导；新页面、关键流程和复杂不可逆操作先补设计。
- 设计系统变更必须同步规范、token、清单、预览和视觉基准。
