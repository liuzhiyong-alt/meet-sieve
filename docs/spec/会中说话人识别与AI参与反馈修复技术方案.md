# 会中说话人识别与 AI 参与反馈修复技术方案

## 1. 文档信息

| 项目 | 内容 |
| --- | --- |
| 状态 | 已确认需求的实施方案，供后续会话开发 |
| 日期 | 2026-08-06 |
| 范围 | 火山 ASR 说话人标签、声纹匹配、AI 语音唤醒、AI 提问者身份、会中说话人列表 |
| 相关方案 | [技术方案](./技术方案.md)、[Codex 语音唤醒与检测修复技术方案](./Codex语音唤醒与检测修复技术方案.md)、[Step5 说话人识别方案](./Step5-说话人识别未知聚类与人工校对技术方案.md) |

本文档是本轮问题反馈的统一修复入口。与旧方案冲突时，以本文以下两项已确认行为为准：

1. Codex 回答期间暂停实时转写输入和时间线新增，但本地会议录音持续保存，不能丢弃 PCM；
2. 火山实时 ASR 不再被假定为一定返回 speaker 标签，系统必须提供本地、可降级的说话人处理链路。

## 2. 已确认的问题与成功标准

| 编号 | 问题 | 直接原因 | 修复后的成功标准 |
| --- | --- | --- | --- |
| P1 | 刘志勇已录入声纹，仍无法识别 | 本场没有 `speaker_track`；运行时又缺少与当前 CAM++ 模型匹配的正式校准档案 | 有正式档案且音频证据达标时能关联刘志勇；证据或置信度不足时明确显示未知，不误认 |
| P2 | AI 唤醒反馈慢，处理期间会议消息继续滚动 | 唤醒只检查 final，并额外等待较长静音；AI busy 后没有把“录音”和“转写输入”分开控制 | partial 命中后立即出现临时反馈；final 确认后尽快创建正式问题；AI busy 期间本地录音继续、实时转写暂停，终态后恢复 |
| P3 | AI 问题固定显示“你” | `TurnService` 固定写入 `Speaker: "你"`，前端对 `ai_question` 也固定返回“你” | 语音问题显示触发者的成员名或稳定未知编号；手动提问仍显示“你” |
| P4 | 全部显示“未识别说话人”，没有 speaker1/2 | 当前实时接口实测未返回标签；文件接口请求未开启 speaker，且解析了错误字段 | 文件接口正确接收 1/2；实时无标签时也能通过本地声纹/未知聚类形成稳定说话人，不能把所有无标签 utterance 合成一个人 |
| P5 | 右侧说话人列表拉长整页 | 列表没有独立高度和滚动容器 | 默认窗口最多展示 6 行，超出后卡片内部滚动；页面总高度不被列表撑开 |

## 3. 已取得的诊断证据

### 3.1 会议与声纹事实

诊断对象为会议 `20260806-HAQR-03`：

- 15 条 `utterance` 的 `asr_speaker_label` 全部为空；
- `speaker_tracks` 为 0，因此声纹 Runner 根本没有获得可处理的音频轨道；
- 刘志勇已有一条 60 秒、状态为 `ready/accepted` 的声纹样本；
- 声纹 embedding 为当前 CAM++ 模型的 192 维结果；
- 当前模型和 ONNX Runtime 均存在，但正式 `models/voice-matching-profile.json` 不存在。

因此，“已录入声纹”只证明成员候选资料存在，不代表会议音频已被切成说话人轨道，也不代表匹配阈值已经校准。这是两个独立门禁。

### 3.2 火山接口实测

用户已授权将 `20260806-HAQR-03` 的指定 20 秒录音片段上传火山 ASR 做 speaker 标签测试，结果为：

- 实时 `bigmodel_async`：产生 7 条 final，未返回 speaker 标签；
- 普通实时 endpoint：约 8 秒后断开，不能作为当前稳定方案；
- 文件极速识别在加入 `enable_speaker_info=true` 后：返回 2 段，原始字段均为 `result.utterances[].additions.speaker=1`；
- 本地拼接的两人测试音频：原始 speaker 序列为 `1,1,2,2,1,1,2,2`；
- 当前代码只解析 `result.utterances[].speaker_id`，所以即使 provider 返回 1/2，规范化结果仍全部丢失。

结论：火山文件接口具备说话人分离能力；当前 bug 是“请求参数缺失 + 返回字段解析错误”。不能据此推断当前实时接口也承诺返回 speaker 标签。

参考：[火山录音文件识别接口文档](https://www.volcengine.com/docs/6561/1631584?lang=zh)。

## 4. 总体设计

```text
麦克风 PCM
  ├─ 本地录音：始终写入 WAV
  ├─ 实时 ASR：AI 空闲时发送；AI busy 时暂停发送
  │    ├─ partial → 唤醒预览（仅内存，不写正式会议记录）
  │    └─ final   → 普通发言或 AI 指令候选
  └─ 说话人处理
       ├─ provider 有标签 → session + provider label 建 track
       └─ provider 无标签 → 本地 utterance 音频证据 → 成员匹配/未知聚类

AI 指令 final 确认
  → 创建 ai.question（带触发 utterance 和提问者快照）
  → 暂停实时 ASR 输入，不暂停本地录音
  → Codex 回答/停止/失败/超时
  → 恢复新的实时 ASR session
```

设计原则：

- provider speaker 标签只是匿名分轨证据，不等于成员身份；
- 成员身份必须由本地声纹匹配或人工校对决定；
- UI 不直接展示 provider 原始 `speaker_1`，统一展示成员名、`未知说话人 N` 或 `未识别说话人`；
- partial 只用于即时反馈，不作为正式会议事实，不触发 Codex；
- 本地录音、实时转写、说话人识别和 Codex 状态分别表达，不能用一个“暂停”状态混在一起。

## 5. 详细修复方案

### 5.1 P0：修复火山文件接口 speaker 请求与解析

修改入口：

- `internal/adapter/asr/volcano/fileflash/adapter.go`
- `internal/adapter/asr/volcano/fileflash/adapter_test.go`
- `tests/e2e/asr/`

请求体的 `request` 增加：

```json
{
  "show_utterances": true,
  "enable_speaker_info": true
}
```

返回结构新增官方路径：

```go
type providerUtterance struct {
    Text       string            `json:"text"`
    StartTime  int64             `json:"start_time"`
    EndTime    int64             `json:"end_time"`
    SpeakerID  flexibleSpeakerID `json:"speaker_id"`
    Additions  struct {
        Speaker flexibleSpeakerID `json:"speaker"`
    } `json:"additions"`
}
```

`flexibleSpeakerID` 同时接受 JSON number 和 string。归一化优先级为：

1. `additions.speaker`；
2. 兼容旧响应的 `speaker_id`；
3. 两者均为空时保持空值，不伪造 speaker。

内部只保存规范化标签值，例如 `"1"`、`"2"`。展示层继续通过 `speaker_track` 或 `speaker_cluster` 生成稳定编号，禁止直接将 provider 标签当成员姓名。

### 5.2 P1：解除 Speaker Observer 对 provider 标签的硬依赖

当前 `internal/service/speaker/observer.go` 在 `asr_speaker_label` 为空时直接 `Skipped`，这是本场 `speaker_tracks=0` 的直接代码原因。

修改后采用双入口：

#### 路径 A：provider 有 speaker 标签

保留当前 `ASRSessionID + ASRSpeakerLabel` 唯一键，同一实时或文件识别 session 的同标签 utterance 进入同一 track，累计证据后执行 CAM++。

#### 路径 B：provider 无 speaker 标签

不能再直接跳过。为已持久化 final 创建“本地候选 track”，规则如下：

1. 从 `start_sample/end_sample` 和已落盘录音提取 utterance 音频；
2. 执行现有音频质量、重叠风险和最短证据门禁；
3. 证据达标后使用当前 CAM++ encoder 生成 embedding；
4. 先调用现有 `MatchMember` 匹配会议成员；
5. 未达到成员阈值时调用现有 `UnknownAssigner` 合并到会议级未知 cluster；
6. 证据不足时保持 `unresolved`，UI 显示“未识别说话人”，不得为每条短句伪造一个稳定人物。

为了保持表语义清晰，`speaker_tracks` 增加来源字段：

```text
source = provider_label | local_utterance
```

`provider_label` 继续要求 `asr_speaker_label`；`local_utterance` 使用 `source_utterance_id` 作为幂等键。不要把 `local:<uuid>` 塞进 `asr_speaker_label` 冒充 provider 返回值。

建议新增 migration（使用下一个可用版本号），包含：

- `speaker_tracks.source`，非空且受 CHECK 约束；
- `speaker_tracks.source_utterance_id`，仅 `local_utterance` 可用；
- `UNIQUE(source_utterance_id)`；
- 将旧 track 回填为 `provider_label`。

文件 ASR 的 speaker 标签用于两类补偿：会后缺口补转写直接保留标签；必要时可增加“已完成录音片段的延迟分轨”。但首期不要循环上传重叠窗口来伪装实时 speaker，因为不同文件请求中的 1/2 只在各自请求内有效，跨请求编号可能交换。

### 5.3 P1：补齐正式声纹校准档案

代码修复不会自动产生可信阈值。正式匹配还需补齐校准数据：

- 刘志勇：现有 1 段录入音频可作为 enrollment，另录 2 段不同会话的 evaluation；
- 第二位真实说话人：1 段 enrollment + 2 段不同会话的 evaluation；
- 最少还需 5 段 WAV；每段需人工确认说话人，不能复制或改名复用同一音频；
- 建议覆盖安静、正常会议距离和轻度噪声条件，但不额外引入未确认的模型或阈值。

按 `docs/calibration/README.md` 建立 manifest，并执行：

```sh
make calibrate-voice \
  VOICE_CALIBRATION_MANIFEST=/absolute/path/to/voice-manifest.json \
  VOICE_MODEL_PATH=/absolute/path/to/model.onnx \
  VOICE_RUNTIME_PATH=/absolute/path/to/libonnxruntime.dylib
```

校准工具必须满足：

- 所有 evaluation 均正确匹配；
- out-of-set 测试零误认；
- 未知聚类无误合并、无误拆分；
- 输出档案的 model id、version、SHA-256 和 dimension 与运行时锁定模型完全一致。

档案生成后执行 `make verify-voice-profile`。只有验证通过，UI 才能显示“说话人识别：可用”。

### 5.4 P2：提前显示 AI 唤醒反馈

当前正式指令分类在 final 前后完成，partial 命中时用户看不到反馈。增加一个只存在于内存的唤醒预览状态：

```text
idle
  → previewing       partial 已命中唤醒词，显示临时 AI 问题卡
  → waiting_command  final 只有唤醒词，等待指令
  → collecting       已有指令文本，等待 final/端点确认
  → codex_busy       ai.question 已提交且 Codex 已开始
  → idle | failed
```

实现要求：

- 在实时 partial 发布链路调用 `WakeObserver.ObservePartial`；
- partial 命中只发布 `meeting.agent.wake.preview`，携带当前预览文本、result id、revision 和可用的说话人投影；
- 预览不写 `meeting_events`、不进入原始记录、不进入 Codex 上下文；
- 同一 result id 的新 partial 原位覆盖旧预览，不能不断追加卡片；
- final 未命中或候选释放时撤销预览；
- final 事务提交后用正式 `ai.question` 替换预览，避免重复显示。

端点策略调整为：

- “唤醒词和问题在同一 final”时，不再额外固定等待 3 秒；final 提交后即可创建问题；
- “final 只有唤醒词”时保留最多 6 秒等待下一句；
- 跨 final 收集到问题后，以本地 VAD 短防抖确认结束，目标不超过 800 ms；
- 60 秒指令上限和候选释放机制保留；
- 具体防抖值写为常量并由测试固定，不能散落在业务代码中。

性能验收目标：

- 首个命中唤醒词的 partial 到预览可见：P95 ≤ 300 ms；
- 指令 final 提交到正式 `ai.question` 可见：P95 ≤ 1 s；
- `ai.question` 提交到 Codex turn 开始：P95 ≤ 800 ms，不含 Codex 首 token 延迟。

### 5.5 P2：Codex busy 期间暂停转写，不暂停本地录音

这是本方案对旧“媒体暂停”实现的行为修正。

`PauseForTurn` 的控制边界改为实时转写运行时：

1. AI 问题事务提交成功；
2. 在完整 PCM frame 边界关闭“发送到实时 ASR”的门；
3. 结束或冻结当前 ASR session，并等待边界前 final 落库；
4. 本地录音继续写 WAV，录音时长继续增长；
5. busy 期间不产生实时 partial/final，也不为主动暂停创建 `asr.gap`；
6. Codex 成功、停止、失败、审批结束或 10 分钟超时后，创建新 ASR session 并恢复发送；
7. 恢复失败时本地录音仍继续，状态明确显示“实时转写恢复失败”，可走已有缺口补转写流程。

`meeting_media_pauses` 保留审计事实，但注释和字段语义必须改为“ASR 投递暂停范围”：

- `logical_sample`：正式问题提交时的会议录音样本位置；
- `physical_start_sample/physical_end_sample`：未投递给实时 ASR、但已经写入本地录音的样本范围；
- `discarded_samples` 固定为 0，不再表示从本地录音丢弃；
- 主动 AI 暂停范围不得生成 `asr.gap`，否则会在会后把 Codex 回答期间的讨论自动补回时间线，违背用户预期。

前端分别展示：

- 录音：`录音中`；
- 本地保存：`正在保存`；
- 实时转写：`AI 回答期间已暂停`；
- Codex：`AI 正在回答`。

现有“停止回答”按钮保持不变。点击后必须触发 turn 取消、写入终态、恢复 ASR；重复点击幂等。

### 5.6 P2：AI 问题使用真实提问者身份

现有 `ai.question` payload 已有 `speaker`，但服务端和前端都忽略了真实来源。升级 payload 为 v3：

```json
{
  "v": 3,
  "text": "……",
  "trigger": "wake_word",
  "trigger_utterance_id": "……",
  "trigger_utterance_ids": ["……"],
  "speaker_key_snapshot": "participant:… 或 cluster:… 或 track:…",
  "speaker_label_snapshot": "刘志勇 或 未知说话人 1 或 未识别说话人"
}
```

身份选择顺序：

1. 触发 utterance 已关联正式成员：成员显示名和头像；
2. 已进入会议级未知 cluster：`未知说话人 N`；
3. 只有 track：该 track 的稳定会议编号；
4. 暂无证据：`未识别说话人`；
5. 手动点击“请 AI 参与”：`你`。

统一时间线查询应使用 `trigger_utterance_id` 关联触发 utterance 的当前说话人投影，优先展示校正后的成员/cluster；无法关联时回退到 payload 快照。这样人工校对后 AI 问题也会同步到正确说话人，同时 payload 仍保留创建当时的审计快照。

需要修改：

- `internal/service/agent/turn_service.go`：移除固定 `Speaker: "你"`；
- `internal/repository/agent/turn_transactions.go`：写入 v3 身份快照；
- `internal/repository/content/timeline.go`、`internal/service/content/service.go`：投影 AI 问题身份；
- `internal/transport/wails/content_binding.go` 和前端 DTO：透传 `display_name/speaker_key`；
- `MeetingTimelinePanel.vue`：移除 `ai_question => 你` 的固定分支。

### 5.7 P3：右侧说话人列表限高并内部滚动

当前 `visibleSpeakers` 已按 key 去重，继续保留该逻辑，但 key 必须是稳定的 participant/cluster/track；无标签 utterance 不能各自长期占一行。

UI 规则：

- 默认 `1280 × 800` 窗口最多可见 6 行说话人；
- `1024 × 720` 下至少保证系统状态、录音状态和结束会议入口不被挤出；
- 超过可见数量后，只有说话人列表区域 `overflow-y: auto`；
- 卡片本身和整页不随人数继续增高；
- 滚动容器可键盘聚焦，提供可访问名称；
- 使用设计 token 和已有滚动条样式，不新增近似卡片变体；
- 成员名变化或 speaker 校正后保持稳定 key，避免列表闪烁或重复。

实现入口：

- `frontend/src/features/meeting/LiveMeetingView.vue`
- `frontend/src/styles/index.css`

## 6. 实施顺序与代码边界

### 阶段 A：修复真实数据链路

1. 修复文件 ASR speaker 请求和解析；
2. 增加本地候选 track，使实时无标签 final 不再全部跳过；
3. 补齐正式 voice matching profile；
4. 验证刘志勇和第二位校准成员的匹配与 unknown 降级。

阶段 A 未通过前，不应只改前端把“未识别说话人”替换成 speaker1，这会用展示伪装真实识别成功。

### 阶段 B：修复 AI 参与流程

1. partial 唤醒预览；
2. final 快速提交和状态机收口；
3. ASR 输入暂停/恢复，本地录音持续；
4. AI 问题身份投影；
5. 保持停止按钮并覆盖全部终态。

### 阶段 C：UI 收口

1. 说话人列表限高滚动；
2. 状态文案、ARIA 和键盘操作；
3. 三档窗口视觉对比。

每个阶段独立提交和验证，避免把外部接口、声纹算法、Agent 生命周期与 CSS 混成一个不可回滚变更。

## 7. 验证方案

### 7.1 单元测试

#### 文件 ASR

- 请求体断言 `enable_speaker_info=true`；
- `additions.speaker` 为 number 和 string 时均解析；
- `speaker_id` 旧字段兼容；
- `additions.speaker` 优先于旧字段；
- 无标签保持空，不生成 `0` 或 `unknown` 假标签；
- 片段时间、文本和 speaker 同时规范化正确。

#### 说话人处理

- provider 标签存在时沿用 session/label track；
- provider 标签缺失时创建唯一 `local_utterance` track，不再 `Skipped`；
- 重复 Observe 不重复建 track/evidence；
- 证据不足保持未识别；
- 达标 embedding 分别覆盖成员匹配、unknown cluster、歧义拒绝；
- profile 缺失/不匹配时不关联成员并返回稳定错误状态。

#### AI 唤醒

- partial 仅更新同一预览，不落正式事件；
- 同 final 含唤醒词和问题时无 3 秒固定等待；
- 只有唤醒词时 6 秒释放；
- final 事务失败会回滚预览和候选；
- AI busy 忽略新唤醒；
- 成功、停止、失败、审批和超时均恢复 ASR；
- busy 期间录音 writer 持续收到连续 PCM，ASR writer 不收到帧；
- 主动暂停不创建 `asr.gap`。

#### AI 提问者

- voice + member、voice + cluster、voice + track、voice + unresolved、manual 五种投影；
- v2 历史 payload 仍可读取；
- speaker 人工校正后 AI 问题投影更新；
- AI 回答始终显示“AI 助手”。

### 7.2 集成测试

至少增加以下场景：

1. ASR final 无 speaker → 本地 track → CAM++ → 刘志勇 participant → 时间线显示刘志勇；
2. 两位未登记说话人交替发言 → 形成两个稳定未知 cluster，不按 utterance 无限增长；
3. 语音唤醒 → 正式 AI 问题 → ASR 暂停 → Codex 完成 → 新 session 恢复；
4. 暂停期间 WAV 样本数持续增长，时间线无新增 partial/final；
5. 点击停止回答后恢复；
6. 应用重启遇到未完成 pause 时优先恢复 ASR，并保留可审计失败状态；
7. 时间线分页、原始记录和 Codex Context 均不重复展示已消费语音指令。

### 7.3 前端测试

- `MeetingTimelinePanel` 的 AI 问题显示后端身份，不再固定“你”；
- wake preview 与正式问题只出现一张卡，revision 乱序不回退；
- busy 时停止按钮可见、可键盘触发；
- 说话人 1、6、7、20 人时列表高度符合约束；
- 说话人更新/校正后不重复；
- `aria-live` 只播报有效状态变化，partial 高频更新不造成重复噪音。

### 7.4 真实链路验收

真实网络测试必须显式使用授权音频和测试凭据，不能作为默认单元测试：

1. 用已授权的 20 秒片段执行文件 ASR，断言至少一个 segment 的 `SpeakerID` 非空；
2. 用两位真实说话人的测试音频断言 provider 至少返回两个 speaker 值；
3. 启动新会议，由刘志勇和第二位成员交替发言，确认时间线先显示稳定未知编号，再在声纹证据达标后显示成员名；
4. 刘志勇说出唤醒词和问题，记录 partial、final、question commit、Codex start 四个时间点；
5. Codex busy 期间继续讲话，确认 WAV 仍增长、实时消息不新增；
6. Codex 完成和点击停止各测试一次，确认转写恢复且生成新的 ASR session。

运行基础验证：

```sh
mise exec -- go test ./internal/adapter/asr/volcano/fileflash ./internal/service/speaker ./internal/service/agent ./internal/service/content -count=1
mise exec -- pnpm --dir frontend test
make test
make test-race
make verify-voice-profile
```

外部接口验收使用项目现有 `make test-asr-real` 入口，并增加文件 speaker 的真实测试用例；缺少凭据或未授权音频时必须明确 skip，不能伪造通过。

### 7.5 视觉验证

按 `docs/style/meet-sieve/VISUAL-BASELINES.md` 在以下尺寸截图比对：

- `1440 × 900`；
- `1280 × 800`；
- `1024 × 720`。

重点检查：AI 预览/问题/回答层级、停止按钮、四项独立状态、右侧列表内部滚动、结束会议入口始终可见。

## 8. 验收清单

- [ ] 火山文件 ASR 的真实响应可读出 speaker 1/2；
- [ ] 实时 ASR 无 speaker 标签时不会跳过全部说话人处理；
- [ ] 正式 profile 与当前模型完全匹配；
- [ ] 刘志勇在真实会议音频中达到阈值后显示为刘志勇；
- [ ] 低置信度音频不会误认刘志勇；
- [ ] 未登记多人显示为稳定的“未知说话人 1/2”，不是一串“未识别说话人”；
- [ ] partial 命中唤醒词后 300 ms 目标内出现反馈；
- [ ] 正式 AI 问题不重复普通发言，且显示真实触发者；
- [ ] Codex busy 期间本地录音持续、实时转写暂停；
- [ ] 成功、停止、失败、审批和超时后均能恢复转写；
- [ ] 现有“停止回答”按钮保留；
- [ ] 右侧说话人超过 6 行后内部滚动，不拉长页面；
- [ ] Go、前端、race、migration、真实 ASR 和视觉验证均有记录。

## 9. 风险与回滚

- 火山文件 speaker 编号只在单次请求内稳定，不允许直接跨请求合并；跨请求身份由本地 embedding/cluster 决定；
- 声纹阈值必须由真实校准生成，禁止为了让刘志勇“识别成功”手工放宽阈值；
- 本地无标签 utterance 太短时应保持未识别，宁可少识别也不能误认；
- partial 预览不得写入正式事实，避免 ASR 修订造成脏记录；
- ASR 恢复失败不能停止本地录音；会议结束必须优先收尾录音；
- 各阶段使用独立开关或可回滚提交。回滚 AI 预览不影响 final 指令；回滚本地 speaker fallback 时仍保留文件接口修复和原始 utterance。

## 10. 后续开发会话的交付要求

后续会话开始时应先读取本文、项目 `AGENTS.md`、`CLAUDE.md`（如存在）及 MeetSieve UI 规范。实施时：

1. 先建立失败测试或真实基线，再修改代码；
2. 不修改测试来迁就错误实现，不用假 speaker 数据证明真实链路完成；
3. 不顺手重构无关模块；
4. 每完成一个阶段，记录修改文件、数据库迁移、测试结果和仍未完成的真实验收；
5. 只有全部验收项真实通过后，才能把问题标记为完成。
