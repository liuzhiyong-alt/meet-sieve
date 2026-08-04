# MeetSieve 组件规范

<!-- markdownlint-disable MD013 -->

## 1. 组件原则

- 组件从当前 UI 反向提取，不从 Apple 组件库复制。
- 业务页面组合组件，不通过局部 CSS 创建近似变体。
- 每个异步组件覆盖 Default、Loading、Success、Error、Disabled。
- 组件只处理呈现和交互契约，业务状态映射见 `PRODUCT-PATTERNS.md`。
- 未登记的新组件家族默认 `design-required`。

## 2. 组件清单

| 组件               | 当前 CSS / 页面证据                        | 成熟度                 |
| ------------------ | ------------------------------------------ | ---------------------- |
| AppShell           | `.app`、`.main`                            | existing               |
| Sidebar            | `.sidebar`、`.nav`、`.sidebar-foot`        | existing               |
| Titlebar           | `.titlebar`                                | existing               |
| PageHeader         | `.page-head`、`.eyebrow`                   | existing               |
| Button             | `.btn` 及变体                              | existing               |
| IconButton         | `.btn-square`                              | existing，图标体系待选 |
| Status             | `.status` 及语义变体                       | existing               |
| Card               | `.card`、`.card-head`                      | existing               |
| FormField          | `.field`、`.select`、`.textarea`、`.label` | existing               |
| Choice             | `.choice`                                  | existing               |
| Switch             | `.toggle`                                  | existing               |
| Tabs               | `.tabs`、`.tab`、`.tab-panel`              | existing               |
| List               | `.list`、`.list-item`                      | existing               |
| Progress           | `.progress`                                | existing               |
| Timeline           | `.timeline`、`.event`                      | existing               |
| AudioMeter         | `.audio-bar`                               | existing               |
| Notice             | `.notice`                                  | existing               |
| EmptyState         | `.empty`                                   | existing               |
| Modal              | `.modal-backdrop`、`.modal`                | existing               |
| Toast              | `.toast`                                   | existing               |
| Avatar             | `.avatar`、`.avatar-group`                 | existing               |
| LiveStage          | `.live-stage`、`.recording-line`           | existing               |
| OperationSteps     | Step 8 会议收尾与失败页面                  | existing               |
| UploadItem         | Step 6 访客附件与结束会议提案              | existing               |
| TranscriptEditor   | `transcript-editor.html`                   | existing               |
| MinuteVersion      | Step 8 纪要当前与历史版本页                | existing               |
| CursorPagination   | Step 9 会议记录                            | existing               |
| DangerZone         | Step 9 会议、小组与成员详情                | existing               |
| RecoverySummary    | Step 9 中断与删除恢复                      | existing               |
| StorageBreakdown   | Step 9 通用设置                            | existing               |
| FileIntegrityState | Step 9 附件异常                            | existing               |

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
- 会议记录 Alpha 不提供批量选择；需要批量能力的其他列表必须先登记业务模式。

## 13. Progress 与 OperationSteps

### Progress

- 高度 7px，胶囊形；
- 确定进度暴露当前值；
- 不确定任务使用阶段文案，不伪造百分比；
- 成功后转换成明确完成状态，不永久停留 100%。

### OperationSteps

用于会议收尾、崩溃恢复和删除恢复：

- 展示当前步骤、已完成步骤和等待步骤；
- 失败发生在哪一步必须明确；
- 工作目录切换是下次启动生效的单次设置，不使用 OperationSteps 伪装迁移流程；
- 多阶段长流程优先页面或大面板，简单三步可使用 Modal。
- 收尾流程不提供取消或跳过本地保存；失败后只允许安全重试或复制诊断编号；
- 会议核心保存与补转写、Codex 同步分别表达，不能合并为一个总进度。

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

### Native Approval 变体

- 只用于 Codex 原生审批请求，不承担 MeetSieve 自定义权限判断；
- 标题直接说明请求能力，例如“Codex 请求控制浏览器”；
- 依次显示工具、目标、操作摘要和原生风险说明，长内容允许换行但不展示原始参数；
- 操作固定为“拒绝”和“允许本次操作”，不提供永久允许或本轮缓存；
- Modal 打开时会议录音、实时转写和本地保存继续，正文明确等待计入任务超时；
- 过期、取消或 turn 终结时自动关闭，并将焦点恢复到原触发上下文。

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

## 23. MinuteVersion

MinuteVersion 是会议纪要当前版本、只读历史版本和版本动作的组合组件，不负责生成任务
本身的进度展示。

### MinuteVersion Anatomy

```text
MinuteVersion
├── VersionSummary：版本号、来源、状态和保存时间
├── EditorOrPreview：当前草稿编辑或历史只读正文
├── CurrentVersionState：当前、未保存、候选或已确认
├── VersionActions：保存、确认、重新生成或恢复
├── VersionHistory：版本列表与当前标记
└── SourceAnchors：原始记录时间引用与缺口提示
```

- AI 草稿、人工草稿、已确认和历史只读必须使用文字明确区分；
- 有未保存修改时禁用确认，并通过相邻帮助文案解释原因；
- 保存人工修改创建新版本，不在原版本上覆盖正文；
- 新 AI 草稿只作为候选，不能切走人工或已确认的当前版本；
- 历史恢复必须提前说明将创建的新版本号，旧版本继续只读保留；
- 有补转写失败或冲突时持续显示范围，并明确该范围未作为会议事实生成结论；
- 来源锚点使用等宽时间，不依赖颜色表达可追溯关系；
- 默认与最小桌面尺寸都保留当前版本状态和关键版本动作。

页面级金标为 `docs/UI/step8-proposal/minutes-workspace.html` 与
`docs/UI/step8-proposal/minutes-history.html`，已于 2026-08-03 经用户确认。

## 24. CursorPagination

用于会议记录和会议详情长列表的游标分页：

- 只显示上一页、下一页和当前页信息，不查询或伪造总条数；
- 会议记录固定每页 50 场，按 `started_at DESC, meeting_no DESC`；
- 原始记录固定每页 200 条，会议消息固定每页 100 条，均按 `seq` 游标；
- 修改搜索或任一筛选后回到第一页；
- 加载期间禁用重复翻页，失败后保留当前页并允许重试；
- 不使用无限滚动替代可恢复的路由与分页位置。

## 25. DangerZone

危险区固定在详情页正文底部，不放在 Titlebar：

- 每个危险动作分别说明对象、保留内容和不可逆后果；
- 删除录音与删除整场会议是两个独立动作，不合并为“删除”；
- 删除录音使用明确 Danger Modal，无需输入会议号；
- 删除整场会议必须先扫描目录，并输入会议号确认；
- 停止后台任务或删除期间禁用同场新任务；
- 部分删除持续显示失败事实、剩余项目和原清单重试入口，不能显示完整成功。

## 26. RecoverySummary

用于中断会议和删除失败的独立恢复页：

- 先说明已经保留、已经完成和仍需处理的事实；
- 中断恢复不得提供继续原会议录音，只能恢复已有数据或基于本场创建新会议；
- 删除恢复只处理原 manifest 中的剩余项，不重新扫描扩大范围；
- 使用真实阶段或数量，不使用虚假百分比；
- 提供脱敏诊断编号和导出本场诊断入口。

## 27. StorageBreakdown

设置“通用”中的存储占用组件：

- 同时展示工作目录总占用、磁盘总量和可用空间；
- 工作目录内区分录音、附件、数据库与备份、派生与临时文件；
- 日志和声纹模型单独统计，不混入会议数据；
- 扫描只遍历规范目录，不跟随符号链接；
- 扫描是阶段状态，不伪造文件级百分比；
- 不提供自动清理或文件管理器式批量删除。

## 28. FileIntegrityState

附件和外部链接在用户明确点击后才调用系统默认程序：

- 附件打开前重新校验登记相对路径、规范路径、状态和 SHA-256；
- 文件缺失、内容变化或越出工作目录时持久显示错误并阻止打开；
- “打开”和“在文件夹中显示”是两个独立动作；
- 外部链接在操作附近显示完整域名，用户再次点击后交给默认浏览器；
- 不自动打开、不提供应用内预览。

上述 Step 9 组件页面级金标位于 `docs/UI/step9-proposal/`，已于 2026-08-03 经用户确认。
