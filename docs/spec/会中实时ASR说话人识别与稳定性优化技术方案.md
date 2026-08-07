# 会中实时 ASR 说话人识别与稳定性优化技术方案

> 文档状态：实施中
>
> 最后更新：2026-08-06
>
> 事实来源：MeetSieve 现有代码与会议数据、`ai-discussion` 火山 ASR 适配实现、用户提供的火山大模型流式 ASR WebSocket 文档
>
> 核心决策：火山 ASR 负责会话内匿名说话人分轨，CAM++ 负责把匿名轨道映射为正式成员或会议级未知说话人。

> 2026-08-07 真实双人会议发现厂商同一 speaker label 内仍可能发生真实换人。说话人
> 投影刷新和同 label 本地分轨的后续修复，以
> [《会中说话人投影刷新与错误分轨修复技术方案》](会中说话人投影刷新与错误分轨修复技术方案.md)
> 为准；本文其他实时转写、录音保存、重连和 final 幂等约束继续有效。

## 0. 执行约束与进度追踪

本文档是本次长任务的唯一实施基线。代码修改必须能追溯到本文档的问题、决策、实施项或验收项；如需降级、改变阈值、更换模型或引入新基础设施，必须先征得用户确认。

### 0.1 不可违背的原则

1. ASR final 是文本分句边界，不是换人边界。
2. 只有厂商真实返回的 Speaker 字段才能建立 provider track；缺失时不伪造 `speaker_0`。
3. `speaker_0` 只在单个 ASR WebSocket session 内有效，不直接作为会议级或跨会议身份。
4. CAM++ 按轨道累计有效音频，不要求每条短句单独通过声纹识别。
5. 不降低声纹阈值，不通过继承上一发言人伪造识别成功。
6. 本地录音、实时转写、说话人识别是三条独立状态轴，任一外部依赖失败不得停止本地录音。
7. 不记录转写正文、API Key、声纹向量或完整厂商响应到诊断日志。

### 0.2 当前代码实施状态

| 能力 | 状态 | 当前事实 / 剩余工作 |
| --- | --- | --- |
| 火山优化双向端点与 ASR 2.0 请求 | 已实现，待真实接口验收 | 已使用 `bigmodel_async`、`enable_nonstream=true`、`enable_speaker_info=true`、`ssd_version=200` |
| Speaker 多形态字段解析 | 已实现，待真实接口验收 | 已支持根 `speaker_id` / `spk_id` / `speaker`、object/字符串 `additions`、嵌套 `speaker_info.speaker_id` |
| Speaker 缺失不伪造 | 已实现 | 空字段保持无标签，不借鉴 `ai-discussion` 的默认 `speaker_0` |
| session-scoped provider track | 已实现 | 使用 `asr_session_id + asr_speaker_label` 查找轨道 |
| provider track 跨 final 累计证据 | 已实现，待回归 | `EvidenceBuilder.Build` 按 session/label 累计且排除重叠风险 |
| CAM++ 异步处理 | 已实现，待回归 | SQLite 为任务事实源，有界 worker 不阻塞 final 主链路 |
| 成员匹配后历史/未来短句继承 | 已实现，待真实音频验收 | track 归属会投影到全部 utterance；已新增多条不足 3 秒短句累计到 8 秒的回归测试 |
| 会议级未知 Speaker 聚类 | 已有实现，待回归 | `UnknownAssigner` 负责匹配已有 unknown cluster |
| ASR 重连 | 已有实现，待故障注入 | 协调器已具备退避和手动 Retry，需验证 final 失败、网络中断和 gap 边界 |
| final 幂等/异常修订 | 已实现隔离，待真实修订语义验收 | 完全重投幂等成功；同 key 冲突记录精确 `invalid_final` gap 并继续当前 session，不再直接 unavailable |
| final 暂时性持久化失败 | 已实现 | 在单条总超时内最多尝试 3 次，仅依据 `AppError.Retryable` 重试 |
| 200 ms PCM packetizer | 已实现 | 任意连续回调帧累计为 6,400 bytes 发包，测试已覆盖非整包输入 |
| Stop 尾包与最后 final | 已实现，待真实接口验收 | 先刷出不足 200 ms 尾音频，再发空负包并等待 server 终结 |
| 3～8 秒证据收尾匹配 | 已接入 | `lazySpeakerProcessor` 会查询 track 所属会议终态，恢复轮询时自动以 `finalizing=true` 收敛 |
| 脱敏 ASR 诊断 | 部分实现 | 已保留 `X-Tt-Logid`；待记录 Speaker 字段来源、缺失率和重投指标 |

### 0.3 实施顺序

1. 建立现有针对性测试基线，将未实现缺口转为失败回归测试。
2. 补齐 Speaker 响应归一化，保持缺失不伪造。
3. 实现 200 ms packetizer 和 Stop 尾包提交。
4. 验证并修复 final 重投、修订、持久化失败、自动重连和 gap 语义。
5. 验证 provider track 累计、短句回填、unknown cluster，接入 session/会议结束收尾匹配。
6. 执行针对性测试、全量 Go 测试、构建和 race 验证，把证据回写本文档。

## 1. 背景与问题边界

本方案针对会中火山实时 ASR 的三个已复现问题：

1. 同一人的发言（例如刘志勇）只有部分被声纹匹配，短句或后续句子常显示为未识别；
2. 未注册声纹的人被拆为多个“未识别说话人 N”，不能表达 ASR 已判断为同一人的事实；
3. ASR 已输出 partial 后会变为“实时转写暂时不可用”，录音仍在继续但没有自动恢复。

本方案只修复实时 ASR、说话人归属和对应会中状态。不会引入第二个识别模型、消息队列、云端状态服务或新的数据库表。

## 2. 已确认的事实与根因

### 2.1 不是用户停顿导致的全部切分

现场会议 `20260806-HAQR-05` 的原始采样范围显示，部分相邻 final 的前后采样位置相同，即音频时间轴没有空档；其中一条约 20.5 秒的 utterance 后紧接下一条。说明分段主要来自火山 ASR 的句子/VAD 输出边界，而不能把“被拆成两条”当作换人。

因此，不能通过简单加大 VAD 静音阈值来解决。会议中两人轮流发言的间隔可能很短，“是的”这类短句也不能被延迟到数秒后才处理。

### 2.2 匿名说话人标签没有进入现有归属链路

实时请求已开启 `enable_speaker_info`、`enable_nonstream` 和 `ssd_version=200`。当前火山响应映射只读取 `speaker_id` / `spk_id`，没有读取已在火山结果中使用的 `utterances[].additions.speaker`。现场保存的 final 因而全部没有 `asr_speaker_label`。

标签丢失后，系统退回到“每一条本地 utterance 创建一个 track”；短句永远不能和前后的同一人累计声纹证据，自然会连续出现“未识别说话人 6、7、9”。

### 2.3 声纹策略把短句当成独立样本

声纹匹配需要可用的累计音频时长。当前 fallback 的 local track 只使用单条 final 构建证据；例如 2 至 7 秒的短句会持续处于 collecting，不能继承已经确认的刘志勇身份。

正确关系是：ASR 在同一连接中给出的匿名标签用于“同一个人”的临时轨道；声纹只负责把该轨道映射到正式成员。轨道一旦映射成功，之前和之后属于该轨道的短句都应显示正式成员。

### 2.4 可恢复持久化失败被当成永久不可用

`FinalProcessor.OnFailure` 上报 `ASR_EVENT_PERSIST_FAILED` 时没有标记 `retryable=true`。协调器因此直接把实时转写置为 unavailable，而不是记录缺口、重连并继续发送音频。

现场故障中，最后一条 final 的结束位置早于已发送音频约 19 秒，正好表现为页面还留有 partial，但后续转写中断。

## 3. 目标与非目标

### 3.1 目标

1. 对已注册成员：同一个 ASR 说话人轨道累计到可匹配证据后，稳定映射到成员；短句不再单独造成漏匹配。
2. 对未注册成员：如果 ASR 认为是同一个人，在当前会议内稳定显示为 `说话人 1`、`说话人 2`，而不是每句一个编号。
3. 对连续发言：ASR 的句子切分只代表文本边界，不代表换人；前后无音频空档的同标签句子保持同一轨道。
4. 对实时转写故障：录音独立持续保存；可恢复错误自动重连，明确保留缺口范围，不因一条 final 的持久化失败长期停在“暂不可用”。
5. 适配会议轮流发言：短间隔换人和“是的”等短句都即时展示；不通过粗暴延长 VAD 来合并多人发言。

### 3.2 合理性边界

这些目标合理，但有一个不可消除的物理边界：某人第一次只说 1 至 2 秒、又没有被 ASR 分到已有轨道时，声纹证据不足以可靠确认其身份。此时必须先展示为 `说话人 N` / 待识别，不能猜测成刘志勇。

`说话人 N` 是会议内匿名身份，不等于跨会议稳定身份，也不应暴露火山原始 `speaker_id`。

## 4. 设计决策

### 4.1 以 ASR 的 speaker label 建立会话内轨道

- 将 `speaker_id`、`spk_id` 和 `additions.speaker` 统一规范化为 `asr_speaker_label`；
- 同一 ASR 连接中相同 label 进入同一个 provider-label track；
- 连接重建后不假定 label 仍然代表同一个人。新的标签通过已有声纹结果或未知聚类与会议内既有 track 对齐；无法确认时创建新的匿名轨道；
- 保持 ASR 句子 final 的文本边界，不按 speaker track 合并文本，避免把两人的快速交替发言拼错。

### 4.2 累计声纹证据并回填身份

- provider-label track 按时间累计同一人的 final 音频，达到现有最小/目标证据长度后调用声纹匹配；
- 匹配到正式成员后，track 下已经保存的短句与后续句子均归属该成员；
- 未达到证据阈值时保持 `说话人 N`，不降低分数阈值、不用文本内容推断身份；
- ASR 没有返回任何 speaker label 时保留本地 fallback，但只作为兼容路径；日志会记录该比例，防止主链路静默退化。

### 4.3 实时 ASR 输入与结果模式

- 在送入 WebSocket 前将 16 kHz、单声道、PCM16 音频规整为 200 ms（3,200 samples / 6,400 bytes）包。录音落盘仍接收原始回调，不受该处理影响；
- 保留火山优化双向接口所需的 `enable_nonstream=true`、`enable_speaker_info=true`、`ssd_version=200`；
- 验证 `result_type=single` 后切换为增量结果，避免 `full` 历史回放导致重复 final 和严格幂等冲突。若真实接口验证不满足该契约，保留 `full` 并完善幂等处理，不凭猜测切换；
- 不在本次调整 VAD 阈值。默认约 800 ms 先满足快速轮流发言；后续只在带真实双人短间隔样本的验收中决定是否微调。

### 4.4 稳定性与恢复

- 所有 final 持久化错误记录会议、会话、火山 log ID、操作阶段、错误分类和可恢复性；不记录 APP Key、完整转写正文或音频；
- SQLite 短暂 busy/timeout、网络断开、服务端可重试错误：标记缺口并按现有退避策略重连；
- 重复 final：同一稳定结果按幂等成功处理；允许服务端重放历史结果，不因此终止会话；
- 单条不可解析或不可写 final：隔离该条、登记缺口并继续恢复，不能让整个 ASR 长期不可用；
- 重试耗尽才进入 `实时转写暂不可用`，界面保留“重试实时转写”入口，并清楚说明“录音仍在本地保存”。

## 5. 实施清单

| 优先级 | 改动 | 验收结果 |
| --- | --- | --- |
| P0 | 补齐火山 utterance 的 `additions.speaker` 解析和契约测试 | final 可携带统一后的 speaker label |
| P0 | 增加 200 ms PCM packetizer 并单测 | WebSocket 只接收完整 200 ms 包，尾包在停止时刷出 |
| P0 | 修复 final persist error 的 retryable 标记、结构化诊断和恢复路径 | 一次可恢复持久化错误不会直接置 unavailable |
| P0 | 让重复/回放 final 走幂等成功或隔离缺口 | 不因重复 final 中断实时会话 |
| P1 | 按 provider label 累计证据、匹配后回填同轨道记录 | 长句 + 短句统一显示刘志勇；未知人稳定编号 |
| P1 | 将匿名稳定轨道显示为 `说话人 N` | 不再显示每句递增的“未识别说话人 N” |
| P1 | 调整会中状态映射与测试 | 重连中、暂不可用、录音保存状态彼此独立且可读 |
| P2 | 使用真实火山凭证做手工验收 | 验证 speaker 字段实际形态、single 模式与双人短间隔效果 |

## 6. 验收场景

1. **同一已注册成员连续发言**：20 秒长句、紧接 2 秒短句、再接长句。无论 ASR 分为几条，累计证据完成后均显示刘志勇。
2. **未知成员连续发言**：同一个人先说长句、再说“是的”、再说短句。若火山标签一致，三条均显示 `说话人 1`。
3. **快速轮流发言**：甲、乙间隔小于 1 秒轮流说话，分别维持各自 ASR track；不因本地 200 ms 分包或文本分句而合并两人。
4. **无 speaker label 兼容**：火山未返回标签时，系统仍保存文本并走 local fallback；状态与日志说明降级，而不是伪造身份。
5. **持久化故障恢复**：注入一次可恢复写入失败后，录音不中断、登记范围缺口、自动重连并恢复 final；不能直接停为 unavailable。
6. **重复 final**：模拟 full/重放结果，数据库不重复写入，实时会话不失败。
7. **UI**：在 1440×900、1280×800、1024×720 下，录音、本地保存、实时转写三种状态同时可见；未知轨道和“重试实时转写”可理解且可键盘操作。

## 7. 发布与观测

- 首次发布保留匿名 speaker label 是否命中、track 数、匹配耗时、重连次数、final 幂等命中和失败分类等脱敏指标；
- 不采集音频、完整转写正文、APP Key 或声纹向量；
- 真机验收分别录制：刘志勇连续长短句、一个未注册人连续长短句、两人短间隔轮流说话；
- 若火山真实返回的 speaker 字段与预期不符，先以脱敏字段形态和 `X-Tt-Logid` 定位，不调整声纹阈值掩盖问题。

## 8. 实施验证记录

### 8.1 2026-08-06 基线与第一批修复

| 验证命令 | 结果 | 覆盖范围 |
| --- | --- | --- |
| `mise exec -- go test ./internal/adapter/asr/volcano -count=1` | 通过 | WebSocket 协议、Speaker 多形态解析、200 ms 发包、尾包、等待 final |
| `mise exec -- go test ./internal/service/transcript -count=1` | 通过 | final 队列、可恢复持久化重试、幂等、协调器、gap |
| `mise exec -- go test ./internal/domain/transcript -count=1` | 通过 | `invalid_final` gap 原因与稳定 origin key |
| `mise exec -- go test ./internal/service/speaker ./tests/integration/speaker -count=1` | 通过 | session/label 轨道、短句累计、成员投影、unknown cluster、恢复任务 |
| `mise exec -- go test ./cmd/... ./internal/... ./buildtools/... ./tests/unit/... ./tests/integration/... ./tests/contract/... -count=1` | 通过 | 全量 Go 单元、集成与契约回归 |
| `mise exec -- go vet ./...` | 通过 | Go 静态检查 |
| `mise exec -- go test -race ./internal/adapter/asr/volcano ./internal/domain/transcript ./internal/service/transcript ./internal/service/speaker -count=1` | 通过 | WebSocket session、final 队列、协调器与 Speaker worker 并发安全；macOS linker 仅输出 `LC_DYSYMTAB` 警告 |
| `mise exec -- go build ./cmd/meetsieve` | 通过 | 主程序 Go 构建 |
| `mise exec -- go test -tags=asrreal ./tests/e2e/asr -run '^$' -count=1` | 通过 | 真实 ASR 取证测试编译；该测试已强制要求至少一个真实 Speaker 标签，不记录正文 |

待完成项：

1. 使用授权火山账号执行真实双人、短句、长句、断线重连与 30 分钟稳定性验收。
2. 根据真实响应确认 Speaker 字段权威路径，再决定是否收窄兼容分支。
3. 对 `result_type=single` 做独立契约验收；未验收前保持 `full`。

注：全局 `git diff --check` 发现用户现有未提交文件 `frontend/wailsjs/go/models.ts` 存在尾随空白。该文件与本方案无关，本次未擅自修改。
