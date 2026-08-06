# Codex 语音唤醒 ASR 乱序与延迟修复技术方案

<!-- markdownlint-disable MD013 -->

## 1. 文档信息

| 字段 | 内容 |
| --- | --- |
| 文档状态 | 待评审 |
| 创建时间 | 2026-08-07 |
| 最后更新时间 | 2026-08-07 |
| 需求名称 | Codex 语音唤醒后 ASR partial 重复、恢复延迟与样本乱序修复 |
| 后端项目 | MeetSieve（`meet-sieve`） |
| 后端开发分支 | `main`，实施分支待确认 |
| 前端项目 | MeetSieve Vue/Wails 前端 |
| 前端开发分支 | `main`，实施分支待确认 |
| 后端入口 | `internal/service/meeting/`、`internal/service/transcript/`、`internal/service/agent/` |
| 前端入口 | `frontend/src/stores/timeline.ts`、`frontend/src/features/meeting/LiveMeetingView.vue`、`frontend/src/features/meeting/components/MeetingTimelinePanel.vue` |
| 涉及外部依赖 | 火山引擎实时 ASR、用户本机 Codex app-server |
| 数据库 | SQLite `asr_sessions`、`asr_gaps`、`meeting_media_pauses`、`agent_voice_command_utterances` |
| 配置项 | 沿用现有 ASR connect timeout、PCM queue、final queue 与重连退避配置 |
| MQ / 定时任务 | 无 |
| 上线窗口 | 待确认 |
| 回滚方式 | 应用代码回滚；本方案默认不新增 migration |
| 相关文档 | [Codex 检测、语音指令消费与媒体暂停技术方案](./Codex语音唤醒与检测修复技术方案.md)、[技术方案](./技术方案.md)、[产品状态模式](../style/meet-sieve/PRODUCT-PATTERNS.md)、[内容规范](../style/meet-sieve/CONTENT.md) |

## 2. 结论

本次按以下产品语义修复：

1. “你好，会议助手，……？”识别为语音 AI 指令后，只保留一条 `ai.question`，不显示为普通会议发言；
2. Codex 处理期间，本地录音继续写入并可靠落盘；
3. Codex 处理期间，实时 ASR 停止接收新 PCM，会话页不显示旧 partial 或新的 ASR partial；
4. Codex 成功、失败、取消、超时或审批结束后，从恢复时的真实当前 PCM 边界建立全新 ASR session；
5. Codex 处理期间保存到本地录音的音频不进入实时 ASR，也不登记为异常 `asr.gap`，不会被会后缺口补转写自动补回；
6. 本方案只处理语音唤醒 turn。手动“请 AI 参与”保持现有录音和转写行为。

这里的“暂停录音转文字”专指暂停实时 ASR，不停止本地 WAV 保存。如果产品要求 AI 期间连本地录音也停止，需要重新采用录音 runner pause 与逻辑样本压缩方案，不属于本次修复。

## 3. 问题现象与取证

### 3.1 用户可见现象

- 唤醒词和问题触发 Codex 后，会话页仍保留两条或多条“正在识别”；
- Codex 完成后 ASR 长时间停在“正在连接”，新说话不能及时显示；
- 随后出现迟到 partial、重复截断文本或看似乱序的识别行；
- 正式 `ai.question` 通常只有一条，重复主要发生在瞬时 partial 层。

### 3.2 真实会议证据

会议 `20260807-HAQR-01` 的两次语音 turn 数据如下：

| 次数 | ASR 暂停起点 | AI 完成时录音位置 | AI 处理区间 | 错误恢复起点 |
| --- | ---: | ---: | ---: | ---: |
| 第一次 | 9,417,120 | 9,676,320 | 259,200 样本 / 16.20 秒 | 9,417,120 |
| 第二次 | 9,905,760 | 10,130,880 | 225,120 样本 / 14.07 秒 | 9,905,760 |

恢复后的新 PCM 已从当前录音位置开始，但新 ASR session 仍以暂停起点作为 `input_start_sample`，触发：

```text
实时转写 PCM session 已停止或样本不连续
```

数据库随后出现多条 failed/connecting session，并把有意暂停区间错误登记为 `disconnected` gap。

### 3.3 已确认的三个根因

#### 根因一：恢复边界使用了暂停起点

`RuntimeService.ResumeAfterTurn` 当前先读取 `meeting_media_pauses.logical_sample`，调用 `MeetingRuntime.Resume`，之后才读取当前录音边界。

本地录音在 AI 期间持续推进，因此恢复起点必须是 AI 完成后的当前边界，而不是 AI 开始前的暂停边界。

#### 根因二：partial 清理没有跨越 Wails 边界

后端 `PartialProjector.Clear/ClearAll` 只删除后端内存状态，没有发布前端可消费的 tombstone/clear 事件。前端 `timeline.partials` 只能新增和基于可见 final 样本范围做兜底删除。

语音指令 final 被标记为 `consumed` 后不会出现在普通时间线，前端无法依靠该 final 清理对应 partial，所以旧“正在识别”会持续显示。

#### 根因三：partial 身份没有包含 ASR session

前端使用 `result_id` 作为唯一 key，并拒绝不高于旧值的 `revision`。火山新 session 会重新开始 provider sequence，`result_id` 也可能复用；当前 Wails partial DTO 又没有 `session_id`。

因此新 session 的合法 partial 可能被旧 session 状态覆盖或拒绝。

## 4. 修复目标与非目标

### 4.1 修复目标

- 暂停和恢复都有唯一、可证明的 PCM 边界；
- AI 期间本地录音继续，ASR 不接收、不缓存、不追赶这段 PCM；
- 恢复后的第一帧与新 session 的 `input_start_sample` 严格一致；
- 旧 session 的 partial 在进入 AI busy 前从 UI 清除；
- 新旧 session 即使产生相同 provider `result_id` 和较小 revision，也不会互相污染；
- 有意暂停不生成 `asr.gap`，异常断线仍按原规则生成 gap；
- 所有 Codex 终态都能恢复 ASR，不因取消 context 阻止恢复；
- 页面刷新或漏事件后仍可从稳定状态恢复“AI 回答期间已暂停”。

### 4.2 非目标

- 不停止或裁剪 AI 处理期间的本地录音；
- 不把 AI 期间音频送入会后补转写；
- 不改变普通网络断线、背压或 provider 故障的 gap 语义；
- 不修改火山 ASR 的识别算法、VAD 或说话人模型；
- 不通过文本相似度合并 partial；
- 不用前端隐藏替代后端 session 生命周期修复；
- 不改动手动“请 AI 参与”的媒体策略。

## 5. 目标时序

```text
ASR final 命中“唤醒词 + 指令”
  → 原子创建 ai.question，并消费全部指令 utterance
  → MeetingRuntime.PauseForAgentTurn
      → 原子关闭 ASR PCM 接收门
      → 取得最后一帧已接受 PCM 的 end_sample，记为 pause_at
      → 关闭旧 PCM queue
      → 排空 pause_at 之前的发送与 final
      → 终结旧 ASR session
      → 发布 partial session clear
      → 发布 paused_for_ai
  → 调用 Codex
  → 提交 ai.answer / ai.failed / ai.cancelled / timed_out
  → MeetingRuntime.ResumeAfterAgentTurn
      → 进入 resuming，准备全新 coordinator
      → 第一帧恢复期 PCM 到达，记其 start_sample 为 resume_at
      → 以 resume_at 创建新 ASR session
      → 同一帧进入新 PCM queue，不丢首帧
      → 完成 meeting_media_pauses，记录 physical_end_sample=resume_at
      → 发布 connecting；建连后发布 streaming
  → 重新开放下一次语音唤醒
```

关键顺序约束：

1. `ai.question` 成功提交后才暂停 ASR，避免误唤醒中断转写；
2. 关闭 ASR PCM 接收门后才能停止旧 session，保证 pause cutoff 不继续前移；
3. 旧 session 完全收口并清除 partial 后才能调用 Codex；
4. 新 session 的起点由恢复后第一帧决定，不能由 AI 开始时的边界推断；
5. 新 coordinator 必须接收用于确定 `resume_at` 的同一帧，禁止先观察后丢弃；
6. turn 终态提交后执行恢复；恢复使用独立有界 context，不复用已经取消的 turn context。

## 6. 后端设计

### 6.1 MeetingRuntime 状态机

为会议级转写运行时增加显式内存状态：

```text
idle → running → pausing → paused_for_ai → resuming → running
                    │               │
                    └──── failed ───┘
```

运行时在同一把锁下维护：

- 当前 meeting ID；
- 当前 ASR coordinator 与 session generation；
- PCM 接收门状态；
- 最后一帧已接受 PCM 的结束样本；
- 当前 voice turn owner；
- 恢复首帧 ack channel；
- 当前 partial session identity。

`TryAcceptFrame` 的行为调整为：

| 状态 | 行为 |
| --- | --- |
| `running` | 把 frame 交给当前 coordinator，并推进 `lastAcceptedEnd` |
| `pausing` / `paused_for_ai` | 返回成功但有意丢弃 ASR 投递，不创建 gap |
| `resuming` | 第一帧原子创建新 coordinator，并把同一 frame 放入新队列；后续按 `running` 处理 |
| `failed` | 保持本地录音，按现有不可用状态处理 |

禁止在持有运行时 mutex 时执行网络连接、等待 final 或数据库长事务。

### 6.2 ASR 专用 Pause

新增或收敛为语义明确的接口：

```go
// PauseForAgentTurn 关闭 ASR PCM 接收门，并有界排空暂停边界前的旧 session。
func (runtime *MeetingRuntime) PauseForAgentTurn(
    ctx context.Context,
    meetingID string,
    ownerID string,
) (ASRPauseBoundary, error)
```

`ASRPauseBoundary` 至少包含：

```go
type ASRPauseBoundary struct {
    SessionID string
    Sample    int64
    Generation uint64
}
```

执行步骤：

1. 校验 meeting 和 owner；同一 owner 重试幂等，不同 owner 返回冲突；
2. 在锁内把 PCM 门切为 `pausing`，冻结 `lastAcceptedEnd` 为 `pause_at`；
3. 锁外调用旧 coordinator 的专用 `PauseAt(ctx, pause_at)`；
4. 只排空已接受且不超过 `pause_at` 的 PCM/final；
5. 终结旧 session 为 `stopped`，不走异常断线重连，也不创建 tail gap；
6. 发布当前 session 的 partial clear；
7. 状态切为 `paused_for_ai`。

暂停不能继续调用通用异常 `Stop` 流程。通用 `Stop` 可以为异常尾部建立 gap，而业务暂停必须明确禁止该行为。

### 6.3 ASR 专用 Resume

推荐接口：

```go
// ResumeAfterAgentTurn 用恢复后的第一帧创建新 ASR session，并返回真实恢复边界。
func (runtime *MeetingRuntime) ResumeAfterAgentTurn(
    ctx context.Context,
    meetingID string,
    ownerID string,
) (ASRResumeBoundary, error)
```

执行步骤：

1. 校验当前 owner 和 `paused_for_ai` 状态；
2. 创建全新的 coordinator、PCM queue、final processor、context 和 generation，但暂不假设起始样本；
3. 状态切为 `resuming`；
4. 下一帧持久化 PCM 到达时，以 `frame.StartSample` 作为 `resume_at`；
5. 使用 `resume_at` 启动新 session，并把该帧交给新 queue；
6. 通过 ack 把 `resume_at` 返回给调用方；
7. `meeting_media_pauses.physical_end_sample=resume_at`、`discarded_samples=0`、状态更新为 `completed`；
8. 发布 `connecting`，成功建连后发布 `streaming`。

恢复 ack 必须有界等待。若等待失败：

- 本地录音继续；
- ASR 状态进入 `unavailable` 或可重试失败；
- `meeting_media_pauses` 标记稳定错误码；
- 不再次关闭本地录音，也不把暂停区间补成 gap。

### 6.4 RealtimeCoordinator session 隔离

每次 Resume 必须创建全新的：

- `context.Context` / cancel；
- `PCMQueue`；
- `FinalProcessor`；
- `PartialProjector`；
- failure channel 与 done channel；
- local session ID 和 generation。

`readEvents` 在处理 partial/final 前校验 session ID 和 generation 仍为当前值。旧 session 迟到事件直接丢弃，不允许：

- 发布到 Wails；
- 写入新 session；
- 推进新 session 的 `last_final_sample`；
- 触发新一轮 reconnect。

### 6.5 有意暂停与 gap 语义

业务暂停区间由 `meeting_media_pauses` 记录，不属于 ASR 故障：

- 不创建 `asr_gaps`；
- 不进入 gap 补转写列表；
- 不影响 `gap_state`；
- 仍可通过录音文件审计实际音频；
- 会议时间、录音时间和 ASR 内容范围保持各自真实语义。

普通断网、provider 错误、队列背压和 final 持久化失败继续使用现有 `asr_gaps` 与重连机制。

## 7. partial 生命周期协议

### 7.1 事件契约

扩展 `meeting.asr.partial`：

```json
{
  "meeting_id": "...",
  "session_id": "...",
  "generation": 3,
  "result_id": "speaker_1:0",
  "revision": 1,
  "text": "...",
  "start_sample": 123,
  "end_sample": 456
}
```

新增显式清理事件 `meeting.asr.partial.cleared`：

```json
{
  "meeting_id": "...",
  "session_id": "...",
  "generation": 3,
  "result_id": ""
}
```

语义：

- `result_id` 非空：清除该 session 的一条 partial；
- `result_id` 为空：清除该 session 的全部 partial；
- final 到达时发布单条 clear；
- Pause、Stop、失败和 session 替换时发布 session clear；
- clear 是幂等 tombstone，重复或乱序到达不得恢复已清除内容。

### 7.2 前端存储

前端使用以下复合 key：

```text
partial_key = session_id + ":" + result_id
```

revision 只在同一个 `session_id + result_id` 内比较。切换 session 后，即使新 revision 从 1 开始，也必须被接受。

Store 同时记录每个 session 的 cleared generation/revision，防止迟到 partial 在 clear 后重新插入。

### 7.3 会话页展示

- `paused_for_ai` 或 voice turn `codex_busy` 时，不渲染任何 ASR partial；
- 进入暂停状态时 Store 必须实际删除旧 partial，不能只用 `v-if` 隐藏；
- Codex 处理中只显示已提交的 AI 问题、AI 回答 partial/审批状态和“等待 AI 处理”状态；
- 恢复进入 `connecting` 后可以显示“实时转写正在恢复”，但不展示暂停前文本；
- 新 session 收到第一条 partial 后按正常规则显示一条“正在识别”。

## 8. 会中状态投影

页面不能只用进程内 `wakeCommand.state === codex_busy` 推断媒体暂停，因为事件可能丢失，页面也可能刷新。

`LiveMeetingStatus` 应由以下事实组合：

- 当前会议生命周期；
- 当前活动 `meeting_media_pauses`；
- MeetingRuntime 内存状态；
- `meetings.realtime_asr_state` 的持久状态。

优先级建议：

```text
active media pause
  → paused_for_ai / resuming
runtime ASR failure
  → unavailable / reconnecting
database projection
  → connecting / streaming / stopped
```

状态文案沿用已确认语义：

| 状态轴 | AI 处理中 |
| --- | --- |
| 录音 | 录音中 |
| 麦克风 | 输入正常 |
| 本地保存 | 正在保存 |
| 实时转写 | AI 回答期间已暂停 |
| Codex | 正在回答 / 等待审批 |

说明文案：

```text
AI 回答期间，本地录音继续保存；实时转写已暂停，回答结束后自动恢复。
```

## 9. 数据库与兼容性

本方案复用现有 `meeting_media_pauses`：

- `logical_sample` / `physical_start_sample`：ASR 停止接收的 `pause_at`；
- `physical_end_sample`：恢复后第一帧的 `resume_at`；
- `discarded_samples`：固定为 0，因为本地录音没有丢弃；
- `state`：沿用 `pausing/paused/resuming/completed/failed`；
- `last_error_code`：保存稳定暂停或恢复错误码。

默认不新增 migration。实施前需确认当前 migration 尚未发布时仍保持版本号唯一，不重写已被真实工作目录执行过的 migration。

现有错误生成的 `disconnected` gap 不在应用启动时自动删除或改写。是否修复历史测试会议数据应另行提供只读范围确认、备份和数据修复 SQL，不能混入本次代码发布。

## 10. 错误处理

| 场景 | 稳定错误码 | 行为 |
| --- | --- | --- |
| 无法关闭 ASR PCM 门或排空旧 session | `ASR_PAUSE_DRAIN_FAILED` | 不调用 Codex；本地录音继续；ASR 尽力恢复 |
| 无法建立恢复 session | `MEETING_MEDIA_RESUME_FAILED` | AI 终态保留；录音继续；ASR 显示不可用并允许重试 |
| 旧 generation 事件迟到 | 不新增用户错误码 | 安全丢弃并记录不含正文的诊断计数 |
| partial clear 事件丢失 | 不新增用户错误码 | 状态刷新和 session identity 变化时前端再次清理 |
| 会议在 AI 期间结束 | 沿用取消/收尾错误码 | 终结旧 pause 事实，不重新启动 ASR |

日志禁止记录 partial 正文、语音指令正文、Codex prompt、回答正文或本地完整路径。诊断字段只保留 meeting ID、local ASR session ID、generation、边界、错误码和耗时。

## 11. 并发与幂等

- 同一会议最多一个 voice turn pause owner；owner 使用 `agent_turn_id`；
- 同一 owner 重复 Pause/Resume 返回相同结果；不同 owner 返回冲突；
- PCM 门、session 指针、generation 与首帧初始化在同一互斥边界内切换；
- 网络连接、provider Stop、final drain 和数据库事务在锁外执行；
- Resume 首帧只能被一个 goroutine用于初始化，并且该帧只能入队一次；
- StopMeeting 可从 `running/pausing/paused_for_ai/resuming` 任一状态幂等收口；
- turn 取消、超时、失败和审批拒绝共用一个媒体恢复 finalizer；
- 恢复失败不能覆盖已经提交的 AI 终态，也不能阻止本地会议结束。

## 12. 修改范围

### 12.1 后端

重点修改：

- `internal/service/transcript/meeting_runtime.go`
  - 显式 ASR gate 状态机；
  - Pause 返回冻结 cutoff；
  - Resume 以第一帧初始化新 session；
- `internal/service/transcript/realtime_coordinator.go`
  - 区分业务 Pause 与异常 Stop；
  - session generation 隔离；
- `internal/service/transcript/realtime_coordinator_connection.go`
  - 旧 generation 事件过滤；
  - partial clear 发布；
- `internal/service/transcript/event_processor.go`
  - partial clear sink/tombstone；
- `internal/service/meeting/runtime_service.go`
  - 调整 Pause/Resume 编排顺序；
  - 使用真实 `resume_at` 完成媒体暂停事实；
- `internal/transport/wails/asr_binding.go`、`cmd/meetsieve/main.go`
  - 扩展 partial DTO 并发布 clear 事件；
- `internal/service/content/service.go`
  - 从活动媒体暂停与运行时状态恢复会中状态。

### 12.2 前端

重点修改：

- `frontend/src/stores/timeline.ts`
  - partial 复合 key、session clear 与 tombstone；
- `frontend/src/stores/asr.ts`
  - 同步 session identity 与 clear 行为；
- `frontend/src/features/meeting/LiveMeetingView.vue`
  - 监听 clear 事件；
  - 使用稳定媒体暂停投影，不只依赖 wake event；
- `frontend/src/features/meeting/components/MeetingTimelinePanel.vue`
  - AI 处理期间不渲染 ASR partial；
  - 恢复期只显示明确状态，不回显旧文本。

### 12.3 文档

实施时同步修正以下冲突：

- 原方案中“AI 期间本地录音也暂停/丢弃 PCM”的描述；
- `PRODUCT-PATTERNS.md` 中 AI busy、ASR paused 和 resuming 状态；
- `CONTENT.md` 中录音、本地保存、实时转写与 Codex 的独立文案；
- 对应 Preview、组件清单和视觉基准。

如不改变当前已确认视觉结构，只调整真实状态绑定和清除临时行，不新增页面或组件家族。

## 13. 测试方案

### 13.1 必须先补的失败回归测试

1. 本地录音从 A 连续推进到 B，AI 期间 ASR 不接收 A～B；恢复 session 的 `input_start_sample` 必须为 B；
2. 恢复首帧必须进入新 queue，不能只用于测量后被丢弃；
3. 连续执行两次 voice turn，不产生 `PCM session 已停止或样本不连续`；
4. 有意暂停区间不创建 `asr.gap`；
5. 旧 session 发布多个 partial，Pause 后前端全部清空；
6. 新 session 复用相同 `result_id` 且 revision 从 1 开始，前端仍接受；
7. clear 后迟到的旧 generation partial 不会重新出现；
8. AI 成功、失败、取消、超时、审批拒绝和结束会议竞态都能收口。

### 13.2 单元与 Service 测试

- `MeetingRuntime`：门控、cutoff、首帧恢复、generation、失败回滚；
- `RealtimeCoordinator`：PauseAt 无 gap、旧事件过滤、partial tombstone；
- `RuntimeService`：媒体暂停事实的 start/end 边界与全终态恢复；
- Pinia：partial 复合 key、clear、迟到事件与页面重建；
- Vue：`codex_busy/paused_for_ai/resuming` 下的会话行和状态文案。

测试必须使用可控 frame/channel/clock 建立同步点，不使用 `time.Sleep` 猜测并发完成。

### 13.3 集成测试

构造完整链路：

```text
RecordingCoordinator
→ persisted PCM observer
→ MeetingRuntime
→ fake RealtimeTranscriber
→ voice TurnService
```

验证两次连续唤醒后的：

- ASR session 输入边界严格递增；
- `meeting_media_pauses` start/end 正确；
- 没有业务暂停 gap；
- 指令 utterance 保持 consumed；
- Timeline 只有 AI 问题和回答，没有普通指令发言或残留 partial；
- 所有 goroutine、queue 和 session 都能停止。

### 13.4 真实链路验收

必须使用真实麦克风、火山 ASR 和本机 Codex：

1. 说“你好，会议助手，我上面都说了什么？”；
2. AI 开始后继续说一段仅用于验证暂停的话；
3. 确认 AI 期间会话页没有 ASR partial，本地录音文件仍增长；
4. AI 完成后立即说新内容，首条 partial 在正常实时延迟内出现；
5. 连续执行两次，确认不进入 reconnect 循环；
6. 查询 SQLite，确认恢复 session 从实际恢复边界开始，没有 AI 区间 `asr.gap`；
7. 检查日志，没有 PCM 不连续、旧 session final 写入或持续重连；
8. 分别验证 Codex 成功、取消、失败和等待审批。

自动化 fake 只证明状态机和边界，不替代真实火山/Codex 验收。

## 14. 完成标准

- 一次唤醒只显示一条 AI 问题；
- AI 处理中会话页不显示任何 ASR partial；
- AI 处理期间本地录音持续保存；
- 恢复 session 的 `input_start_sample` 等于恢复后第一帧 `start_sample`；
- 连续两次唤醒不产生样本不连续、重连追赶或迟到旧消息；
- 新旧 session 的相同 `result_id/revision` 不冲突；
- 业务暂停不产生 `asr.gap`，真实故障 gap 行为不回退；
- 成功、失败、取消、超时、审批和结束会议均能恢复或安全收口；
- Go 单测、集成测试、race test、前端 Vitest、类型检查、lint 和生产构建通过；
- 真实 macOS 麦克风 + 火山 ASR + Codex 连续两次唤醒验收通过；
- 文档、状态文案和实现语义一致。

## 15. 实施顺序

1. 补失败回归测试，复现 A→B 边界错位和 partial 残留；
2. 实现 ASR gate 与 PauseAt，确保冻结 cutoff 后不再接收 PCM；
3. 实现 Resume 首帧建 session，消除错误恢复边界；
4. 增加 session generation 和 partial clear 契约；
5. 修改 Pinia 与会中页面，清理并隔离新旧 partial；
6. 收敛媒体暂停事实、状态恢复和结束会议竞态；
7. 同步设计系统文案与原技术方案冲突；
8. 完成自动化、race、构建和真实链路验收。

不得把步骤 2～4 降级为仅在前端过滤文本，也不得通过扩大队列或延长重连时间掩盖错误边界。

## 16. 回滚与剩余风险

### 16.1 回滚

- 本方案默认不新增表和字段，应用代码可回滚；
- 已产生的 `meeting_media_pauses` 继续保留，旧版本忽略不认识的运行时语义；
- 不自动删除历史会议中的错误 gap 或失败 session；
- 若 Wails 事件契约需要兼容旧前端，新增字段保持可选，clear 事件由新前端消费，旧前端可忽略。

### 16.2 剩余风险

- 火山建连本身仍存在真实网络延迟，但新 PCM 会从正确边界缓冲，不再回放 AI 期间内容；
- 若恢复后长时间收不到任何麦克风 frame，Resume ack 会超时并进入明确失败状态；
- 页面刷新时无法恢复 partial 正文是预期行为，partial 不是持久事实；
- 当前真实工作目录已存在由旧实现产生的错误 gap，需单独决定是否清理测试数据；
- Windows 音频帧调度和 Wails 事件顺序仍需真实设备验收。

## 17. 待评审项

1. 当前明确采用“本地录音继续、实时 ASR 暂停”；是否确认不停止 WAV 写入；
2. AI 期间录下的音频是否永远不参与转写；本方案默认不实时转写，也不自动补转写；
3. 手动“请 AI 参与”是否维持现状；本方案默认维持现状；
4. 历史测试会议中已错误生成的 gap 是否需要另立数据清理任务；本方案默认不自动修改历史数据。
