# MeetSieve 产品模式与状态映射

<!-- markdownlint-disable MD013 -->

## 1. 成熟度

| 标记              | 含义                           | 开发权限         |
| ----------------- | ------------------------------ | ---------------- |
| `existing`        | 当前权威 UI 已有明确设计       | 可直接高保真实现 |
| `derived`         | 可由现有组件无歧义组合         | 可按本文直接实现 |
| `design-required` | 涉及新页面、关键流程或复杂决策 | 先补设计并确认   |
| `blocked`         | 产品或技术行为尚未确定         | 不得设计或开发   |

未登记模式默认 `design-required`。

## 2. 会议生命周期

| 后端状态        | UI 表达                                                                   | 主操作        | 成熟度          |
| --------------- | ------------------------------------------------------------------------- | ------------- | --------------- |
| `preparing`     | 创建会议页；检查目录、麦克风、转写、Codex、LAN                            | 开始会议      | existing        |
| `recording`     | LiveStage + 持续录音状态 + 时间线                                         | 结束会议      | existing        |
| `finalizing`    | 会中页面切为 OperationSteps；停止 LAN、尾部 final、合并录音、刷新原始记录 | 查看失败/等待 | design-required |
| `ended`         | 会议详情；显示本地保存、缺口、纪要和同步状态                              | 会后处理      | existing        |
| `interrupted`   | 中断恢复页；说明可恢复内容、缺口和不能继续原录音                          | 恢复已有数据  | design-required |
| `deleting`      | 页面或 Modal 持续显示删除中，禁止重复操作                                 | 等待          | derived         |
| `delete_failed` | Danger Notice + 未删除文件 + 重试                                         | 重试删除      | existing        |

### 约束

- 正在录音关闭应用时显示 `继续会议 / 结束会议并退出`，成熟度 `derived`。
- `finalizing` 不能用全屏通用 Spinner 代替步骤。
- 崩溃恢复禁止提供“继续原会议录音”。

## 3. 本地保存

| 状态      | 文案基线                         | 组件                  | 成熟度   |
| --------- | -------------------------------- | --------------------- | -------- |
| `pending` | 等待写入                         | Neutral Status        | derived  |
| `saving`  | 正在本地保存                     | Info/Neutral Status   | existing |
| `saved`   | 本地已保存 / 2 秒前              | Success Status        | existing |
| `failed`  | 本地保存失败；明确哪些数据受影响 | Danger Notice + Retry | derived  |

本地保存失败优先级高于 ASR、Codex 和 LAN 状态。不能因为数据库仍有部分数据就显示
“已保存”。

## 4. 实时转写

| 状态           | 文案基线                   | 组件                    | 成熟度   |
| -------------- | -------------------------- | ----------------------- | -------- |
| `idle`         | 尚未启动                   | Neutral Status          | derived  |
| `connecting`   | 正在连接实时转写           | Info Status             | derived  |
| `streaming`    | 实时转写正常               | Success Status          | derived  |
| `reconnecting` | 实时转写中断，录音仍在保存 | Warning Status + Notice | derived  |
| `unavailable`  | 实时转写暂不可用           | Warning Modal / Notice  | existing |
| `stopped`      | 实时转写已停止             | Neutral Status          | derived  |

### 会前失败

必须提供：

- `仅录音继续`；
- `检查设置`；
- `取消会议`。

当前 `start.html` 为金标，成熟度 `existing`。

### 会中断线

- 录音状态保持 Success/Recording；
- ASR 单独变为 Warning；
- 时间线登记缺口起点；
- 重连后显示恢复，缺口仍保留到补转写完成；
- 不用全局 Danger 让用户误以为录音失败。

## 5. 补转写缺口

| 状态         | UI 表达                   | 主操作       | 成熟度          |
| ------------ | ------------------------- | ------------ | --------------- |
| `none`       | 不显示缺口提示            | 无           | existing        |
| `pending`    | 缺口范围 + Warning Status | 立即处理     | existing        |
| `processing` | 范围 + Progress/阶段      | 停止或等待   | derived         |
| `completed`  | 已补齐并标记来源          | 查看原始记录 | derived         |
| `failed`     | 保留缺口范围和失败原因    | 重试补转写   | derived         |
| `conflict`   | 同一时间范围保留两份结果  | 进入人工校对 | design-required |

冲突结果不得自动覆盖已有 final。补转写失败时允许生成纪要，但生成确认必须持续显示
缺口范围。

## 6. Codex

| 状态           | 文案基线                        | 组件                         | 成熟度   |
| -------------- | ------------------------------- | ---------------------------- | -------- |
| `unchecked`    | 尚未检测                        | Neutral Status               | derived  |
| `initializing` | 正在准备 AI                     | Info Status                  | derived  |
| `available`    | AI 可参与                       | Success Status               | existing |
| `busy`         | AI 正在参与                     | Timeline Event + Stop Button | derived  |
| `busy_long`    | AI 处理时间较长                 | Warning/Info Notice + Stop   | derived  |
| `unavailable`  | AI 暂不可用，录音和转写不受影响 | Warning Notice               | derived  |
| `unsynced`     | Codex 结束同步失败，可重试      | Danger/Warning Status        | existing |

### Turn

- 提问立即写入时间线；
- Partial 流式更新当前 AI Event；
- 30 秒后显示“处理时间较长”；
- 用户可停止回答；
- 取消后显示取消状态，不保存 partial 回答；
- Busy 时忽略新唤醒，不排队；
- 10 分钟超时转换为失败并允许重新提问。

### 恢复

- thread 可恢复时提供 `codex resume <thread_id>`；
- 不可恢复时提供读取会议目录的命令；
- 复制成功使用 Toast；
- 复制失败提供可选中文本，不只显示 Toast。

## 7. 会议纪要

| 状态            | UI 表达                        | 主操作            | 成熟度          |
| --------------- | ------------------------------ | ----------------- | --------------- |
| `not_generated` | 尚未生成会议纪要               | 生成纪要          | existing        |
| `generating`    | 当前生成任务、耗时和停止入口   | 停止生成          | derived         |
| `draft`         | 最新 AI 草稿、当前版本和操作区 | 编辑/确认         | design-required |
| `confirmed`     | 当前已确认版本                 | 重新生成/查看历史 | design-required |
| `failed`        | 保留上个成功版本并说明失败     | 重试生成          | derived         |

版本比较、历史恢复、人工编辑和确认属于新关键工作区，必须先补设计。重新生成不得覆盖
人工当前版本。

## 8. LAN

| 状态       | 文案基线                     | 组件                      | 成熟度   |
| ---------- | ---------------------------- | ------------------------- | -------- |
| `disabled` | 本场未开启访客页             | Switch / Neutral Status   | existing |
| `starting` | 正在启动访客页               | Info Status               | derived  |
| `serving`  | 访客页运行中，显示在线人数   | Success Status            | existing |
| `failed`   | 访客页启动失败，录音不受影响 | Warning Notice + Retry    | derived  |
| `stopped`  | 会议已结束，访客入口已失效   | Neutral/Danger Page State | derived  |

### 访客入口

- 显示地址和二维码；
- 提醒只在可信私有网络使用；
- 自动选网卡失败时允许手动选择；
- 会议结束立即失效；
- 访客填写临时显示名称，不选择正式成员身份。

LAN Desktop 页面布局为 `design-required`；当前 `guest.html` 只作为组件和内容证据。

## 9. 附件上传

| 状态          | UI 表达                  | 成熟度  |
| ------------- | ------------------------ | ------- |
| Ready         | 文件名、大小、发送动作   | derived |
| Uploading     | Progress、已传大小、取消 | derived |
| Processing    | 正在校验并写入资料索引   | derived |
| Completed     | 文件可见、已进入本场资料 | derived |
| Cancelled     | 已取消，不保留临时文件   | derived |
| Too Large     | 单文件超过 500 MB        | derived |
| Blocked Type  | 文件类型不允许上传       | derived |
| Disk Full     | 主机可用空间不足         | derived |
| Interrupted   | 上传中断，可重新选择文件 | derived |
| Meeting Ended | 本场会议已结束，不能上传 | derived |

结束会议时存在上传任务，确认弹窗必须明确等待或结束的后果；如果承诺会后重试，详情页
必须提供真实入口。

## 10. 说话人与声纹

| 场景         | UI 表达                     | 成熟度          |
| ------------ | --------------------------- | --------------- |
| 无声纹成员   | 仍可参会，显示“未录入声纹”  | existing        |
| 样本列表     | 环境、时长、质量和删除/新增 | existing        |
| 录入新样本   | 录音、音量、时长、质量结果  | existing        |
| 删除全部声纹 | 永久删除确认，不改历史会议  | derived         |
| 模型重建     | 进度 + 自动匹配暂不可用     | derived         |
| 低置信度     | 显示未知说话人，不强行认人  | derived         |
| 单条校对     | 修改选中片段                | existing        |
| 本场批量校对 | 修改本场同一未知说话人      | existing        |
| 加入声纹     | 独立明确确认                | existing        |

录入新样本的页面级金标为 `docs/UI/voice-enrollment.html`。设置页模型下载与离线导入的
页面级金标为 `docs/UI/model-management.html`；两者均已于 2026-08-01 经用户确认。
成员页通过“管理声纹 → 录入新样本”进入录入金标，返回时定位到成员 Tab；设置主页面
将声纹模型作为第五个设置 Tab，独立模型金标的分类导航可返回主设置页对应 Tab。

校对工作台页面级金标为 `docs/UI/transcript-editor.html`，已于 2026-08-02 经用户确认。
校对只在会议结束或中断后开放。批量修改按本场 unknown cluster 生效，覆盖当前 cluster
片段已有的单条说话人校对，并使后续归入该 cluster 的片段继承新身份；确认 Modal 必须
展示当前影响条数。原始 ASR、自动判断和每次人工修改仍保留在本地数据库中。

## 11. 工作目录

| 场景            | UI 表达                            | 成熟度   |
| --------------- | ---------------------------------- | -------- |
| 首次选择        | 目录、可写检查、即将创建的内容     | existing |
| 空目录初始化    | 首次选择页内 loading，不进入步骤页 | derived  |
| 无效非空目录    | 路径字段内联错误，保留输入         | derived  |
| 工作目录不可用  | 重新选择目录或退出应用             | derived  |
| 设置页无变更    | 保存按钮禁用                       | derived  |
| 设置页校验中    | 保存按钮 loading，禁止重复提交     | derived  |
| 已保存待重启    | 同时显示当前路径和下次启动路径     | derived  |
| 会议进行中      | 工作目录设置禁用并解释原因         | derived  |
| schema 升级中   | 启动阻断状态，不确定进度 loading   | derived  |
| schema 升级失败 | 重试、重新选择工作目录或退出       | derived  |

### 工作目录边界

- MeetSieve 不提供工作目录复制、移动、合并或迁移向导；
- 用户搬移数据时先退出应用，使用操作系统复制完整目录，再修改路径；
- 工作目录不能等于应用安装目录，也不能位于安装目录或 macOS `.app` 包内部；
- 不存在或真正空目录直接初始化；
- 非空目录只做 MeetSieve 数据库身份、schema 和可写性的轻量检查；
- 正常运行中保存的新路径在下次启动生效；
- 不展示最近目录、备份列表、目录迁移进度或恢复工具；
- 当前 `settings.html` 中“迁移工作目录”的旧文案不再是产品行为依据，后续设计或
  实现时按本节改为普通“工作目录”配置。

## 12. 删除

### 删除录音

成熟度 `derived`，使用现有 Danger Modal：

- 明确删除 `recording.wav` 和分片；
- 保留转写、附件、AI 问答和纪要；
- 删除后不能回放或再次补转写；
- 永久删除，不进入回收站；
- 按钮写 `永久删除录音`。

### 删除整场会议

当前确认 Modal 为 `existing`：

- 输入会议号确认；
- 按钮写 `永久删除整场会议`；
- 删除中阻止重复操作；
- 部分文件失败时显示 `delete_failed`，不得报告成功。

## 13. 安装、权限与退出

安装器和卸载器遵循 NSIS 与 macOS 平台原生结构，不复用 MeetSieve App Shell，
不实现品牌化 Web 安装页面。

| 场景                                   | 成熟度          |
| -------------------------------------- | --------------- |
| Windows NSIS 标准安装                  | derived         |
| Windows 自定义安装目录                 | derived         |
| Windows 无效安装目录阻断               | derived         |
| Windows 识别旧版本并沿用目录覆盖升级   | derived         |
| Windows 运行中阻止安装、升级和卸载     | derived         |
| Windows 可选桌面快捷方式               | derived         |
| Windows 可选专用网络防火墙规则         | derived         |
| Windows 标准卸载                       | derived         |
| Windows 卸载保留未知文件和全部用户数据 | derived         |
| macOS 未签名 DMG 与 Gatekeeper 指引    | design-required |
| macOS 删除 `.app` 并保留用户数据       | derived         |
| 麦克风权限拒绝/恢复                    | derived         |
| 录音中关闭应用                         | derived         |
| 正常退出                               | derived         |
