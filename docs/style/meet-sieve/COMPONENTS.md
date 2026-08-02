# MeetSieve 组件规范

<!-- markdownlint-disable MD013 -->

## 1. 组件原则

- 组件从当前 UI 反向提取，不从 Apple 组件库复制。
- 业务页面组合组件，不通过局部 CSS 创建近似变体。
- 每个异步组件覆盖 Default、Loading、Success、Error、Disabled。
- 组件只处理呈现和交互契约，业务状态映射见 `PRODUCT-PATTERNS.md`。
- 未登记的新组件家族默认 `design-required`。

## 2. 组件清单

| 组件             | 当前 CSS / 页面证据                        | 成熟度                 |
| ---------------- | ------------------------------------------ | ---------------------- |
| AppShell         | `.app`、`.main`                            | existing               |
| Sidebar          | `.sidebar`、`.nav`、`.sidebar-foot`        | existing               |
| Titlebar         | `.titlebar`                                | existing               |
| PageHeader       | `.page-head`、`.eyebrow`                   | existing               |
| Button           | `.btn` 及变体                              | existing               |
| IconButton       | `.btn-square`                              | existing，图标体系待选 |
| Status           | `.status` 及语义变体                       | existing               |
| Card             | `.card`、`.card-head`                      | existing               |
| FormField        | `.field`、`.select`、`.textarea`、`.label` | existing               |
| Choice           | `.choice`                                  | existing               |
| Switch           | `.toggle`                                  | existing               |
| Tabs             | `.tabs`、`.tab`、`.tab-panel`              | existing               |
| List             | `.list`、`.list-item`                      | existing               |
| Progress         | `.progress`                                | existing               |
| Timeline         | `.timeline`、`.event`                      | existing               |
| AudioMeter       | `.audio-bar`                               | existing               |
| Notice           | `.notice`                                  | existing               |
| EmptyState       | `.empty`                                   | existing               |
| Modal            | `.modal-backdrop`、`.modal`                | existing               |
| Toast            | `.toast`                                   | existing               |
| Avatar           | `.avatar`、`.avatar-group`                 | existing               |
| LiveStage        | `.live-stage`、`.recording-line`           | existing               |
| OperationSteps   | 迁移目录 Modal                             | derived                |
| UploadItem       | List + Progress + Status + Button          | derived                |
| TranscriptEditor | `transcript-editor.html`                   | existing               |
| MinuteVersion    | 纪要页入口                                 | design-required        |

## 3. AppShell

### AppShell Anatomy

```text
AppShell
├── Sidebar
└── Main
    ├── Titlebar
    └── Content
```

- 使用 `LAYOUT.md` 的固定尺寸；
- 当前路由必须在 Sidebar 有 `aria-current="page"`；
- 会议进行中时第一导航项动态显示会议状态；
- Sidebar Foot 只显示当前最重要的一个持久状态；
- 不在业务页面嵌套第二套 AppShell。

## 4. PageHeader

### PageHeader Anatomy

- 可选 Eyebrow；
- 唯一 H1；
- 可选说明；
- 一个主操作或一个页面状态。

### PageHeader 规则

- 不在每个子区块重复 Eyebrow；
- 页面说明最多 66ch；
- 同一 Header 不同时放多个主按钮；
- 关键状态与主操作冲突时，状态进入内容区，主操作保留。

## 5. Button

### Button Variants

| Variant | 用途                         |
| ------- | ---------------------------- |
| Default | 低强调页内动作               |
| Primary | 当前页面唯一主动作           |
| Quiet   | 有边框的次操作               |
| Danger  | 永久删除、结束会议等危险动作 |
| Icon    | 单一图标动作，必须有名称     |

### 尺寸

- 默认高度 40px；
- IconButton 40 × 40px；
- 文字单行；
- 水平 Padding 15px；
- 图标与文字 Gap 8px。

### States

- Hover：表面或颜色轻微变化；
- Active：`scale(.98)`；
- Focus：统一 Focus Ring；
- Disabled：透明度降低，同时解释原因；
- Busy：禁用重复点击，保留按钮宽度并显示进度；
- Danger Busy：进度指示不能错误地变成蓝色成功语义。

链接触发路由，按钮触发命令。不要只为了视觉统一把所有动作都写成 `<a>`。

## 6. Status

### Status Variants

| Variant | 含义                       |
| ------- | -------------------------- |
| Neutral | 空闲、未知、停止、普通信息 |
| Success | 已验证健康或最终完成       |
| Warning | 可继续但需要关注           |
| Danger  | 失败、数据风险、永久危险   |
| Info    | 处理中、AI、当前活动       |

### Status 规则

- 12px Semibold、胶囊形、7px 圆点；
- 文案本身必须表达含义；
- 不用 Success 表示“请求已提交”；
- 同一状态在不同页面使用同一 Variant；
- 复杂错误使用 Notice，不把长文案塞进 Status。

## 7. Card

- 背景 Canvas；
- 1px Soft Border；
- 18px Radius；
- 默认 Padding 20px；
- 设置内容等重要面板可使用 24px；
- 默认无阴影；
- Card Head 使用标题和一个辅助动作；
- 不为每个列表项再嵌套 Card。

## 8. FormField

### FormField Anatomy

```text
Label
Control
Help（可选）
Error（按需）
```

### FormField 规则

- Label 永远可见，Placeholder 不替代 Label；
- Field、Select、Textarea 使用 12px Radius；
- Textarea 最小 96px；
- Readonly 使用近白表面，不能伪装 Disabled；
- Error 出现在字段下方并与字段关联；
- 保存中禁用字段时仍保持内容可读；
- 路径、会议号、命令使用 Mono。

## 9. Choice

- 最小高度 44px；
- 可包含 Checkbox/Radio、Avatar、标题和帮助文字；
- 整行可点击；
- Selected 由原生控件和边框/表面共同表达；
- 成员选择必须说明声纹状态，但无声纹不能被误认为不可参会。

## 10. Switch

- 42 × 24px；
- Track 使用胶囊形，Knob 18px；
- 必须有关联可见标签；
- 使用 Switch 语义，不使用 `aria-pressed` 模拟无标签按钮；
- 只用于立即生效或随页面保存的二态设置；
- 需要确认或产生破坏性后果的操作不能使用 Switch。

## 11. Tabs

- 容器使用 Subtle Surface 和 12px Radius；
- Tab 高度至少 36px；
- Active 使用 Canvas + Ring；
- 支持 Left、Right、Home、End；
- Tab 与 Panel 建立 `aria-controls` / `aria-labelledby`；
- 不用 Tabs 隐藏必须同时比较的内容。

## 12. List

- 行最小 64px；
- 相邻行使用 Soft Border；
- 主内容在左，状态和动作在右；
- 一行通常只有一个直接操作；
- 长标题省略时必须提供完整访问方式；
- 批量选择使用可见 Checkbox，批量操作区域随选择状态出现。

## 13. Progress 与 OperationSteps

### Progress

- 高度 7px，胶囊形；
- 确定进度暴露当前值；
- 不确定任务使用阶段文案，不伪造百分比；
- 成功后转换成明确完成状态，不永久停留 100%。

### OperationSteps

用于工作目录迁移、会议收尾和恢复：

- 展示当前步骤、已完成步骤和等待步骤；
- 失败发生在哪一步必须明确；
- 切换事实源前说明旧目录仍然有效；
- 多阶段长流程优先页面或大面板，简单三步可使用 Modal。

## 14. Timeline

### Event Types

- Human Transcript；
- Guest Message；
- Attachment；
- AI Question；
- AI Answer；
- AI Cancelled/Failed；
- ASR Gap；
- Correction。

### Timeline 规则

- 时间、类型、Speaker 和正文都可读；
- AI 使用蓝色小面积强调，不使用独立大卡片；
- Partial 只更新当前行，不固化为正式事件；
- Gap 和 Conflict 使用 Notice；
- 取消的 partial 回答不显示成正式回答；
- 长列表应虚拟化或分页，但视觉结构不改变。

## 15. AudioMeter

- 只表示当前输入活动，不表示音质或保存成功；
- 条形宽 3px、总高 30px；
- 会中使用 Action Blue，普通测试使用 Meta；
- 辅助技术读取稳定状态文案，不逐帧读取电平；
- 无输入、权限拒绝和设备断开必须有文本状态。

## 16. Notice

| Variant | 用途                           |
| ------- | ------------------------------ |
| Neutral | 解释、提示、非阻塞信息         |
| Warning | 可继续但有缺口或风险           |
| Danger  | 操作失败、数据风险、需处理     |
| Info    | 处理中或需要用户了解的当前状态 |

- 标题说明事实；
- 正文说明影响；
- 操作放在 Notice 内或紧邻位置；
- Toast 不替代需要持续处理的 Notice。

## 17. EmptyState

- 说明为什么为空；
- 有可执行动作时提供一个；
- 筛选空结果与初次无数据使用不同文案；
- 不使用与产品无关的大插画；
- 空状态不能清除用户选择或数据，除非动作明确说明。

## 18. Modal

### 适用

- 二次确认；
- 少量输入；
- 单个短任务；
- 不离开当前上下文的辅助信息。

### 不适用

- 校对工作台；
- 崩溃恢复；
- 多版本比较；
- 长时间多阶段任务；
- 需要同时参考大量页面内容的操作。

### 规格

- 最大宽度 520px；
- Padding 24px；
- 18px Radius；
- Raised Elevation；
- Actions 右对齐，主操作在末端；
- 实现完整焦点锁定、背景 inert 和焦点恢复。

## 19. Toast

- 用于复制、保存设置等短暂且无需后续处理的反馈；
- 最大宽度 360px；
- 右下显示；
- 默认约 2.2 秒；
- 使用 `aria-live="polite"`；
- 错误、缺口、删除失败和上传失败不能只显示 Toast。

## 20. Avatar

- 36 × 36px；
- 无照片时显示一个清晰汉字或短缩写；
- Avatar Group 重叠 8px；
- 不能仅凭头像区分成员，旁边或可访问名称中包含姓名；
- 访客不得使用正式成员头像暗示身份已验证。

## 21. LiveStage

LiveStage 是 MeetSieve 产品识别组件，不是通用 Hero。

包含：

- 录音状态和红色录音点；
- 会议主题；
- 独立状态说明；
- 会议计时；
- 音频电平和稳定输入状态。

不得加入营销文案。录音状态、计时和结束入口在所有桌面尺寸下保持可见。

## 22. TranscriptEditor

TranscriptEditor 是结束或中断会议的独立校对工作台，不放入 Modal。

### TranscriptEditor Anatomy

```text
TranscriptEditor
├── PageHeader：会议号、页面标题、说话人处理状态
├── TranscriptList：按事件序列排列的原始记录
└── CorrectionPanel
    ├── AudioClip
    ├── OriginalASR（只读）
    ├── CurrentText
    ├── SingleSpeakerCorrection
    ├── ClusterCorrection
    └── VoiceSampleConfirm
```

- 主列保留整场上下文，侧栏只编辑当前选中片段；
- 单条与本场 cluster 批量修改使用 Tabs，不能同时提交；
- 批量确认必须显示 cluster 名称、当前影响条数和覆盖既有单条说话人校对的后果；
- 原始 ASR 永远只读，当前文字和当前说话人分别显示 revision 冲突；
- SQLite 已保存但原始记录刷新失败时使用持续 Warning，不回滚为“未保存”；
- 加入声纹是独立 Modal，不能成为保存说话人的 checkbox；
- `1024×720` 收为单列，记录列表在前、编辑面板在后，不产生横向滚动；
- 页面切换片段或离开时撤销短期音频片段。
