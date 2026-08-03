package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
	"meet-sieve/models"
)

const (
	maxQuestionBytes   = 10_000
	defaultTurnTimeout = 10 * time.Minute
	busyLongAfter      = 30 * time.Second
)

// RawRecordFlusher 保证 provider 读取前 Markdown 已追上 SQLite，并在提交后标脏。
type RawRecordFlusher interface {
	Flush(ctx context.Context, meetingID string) error
	MarkDirty(meetingID string) error
}

// TurnEventSink 接收不含 prompt/snapshot/tool output 的轻量运行事件。
type TurnEventSink interface {
	PublishAgentEvent(event port.AgentEvent)
}

// TurnEventSinkFunc 让装配层以函数实现轻量事件发布边界。
type TurnEventSinkFunc func(event port.AgentEvent)

// PublishAgentEvent 发布一条不含敏感上下文的运行事件。
func (publisher TurnEventSinkFunc) PublishAgentEvent(event port.AgentEvent) {
	if publisher != nil {
		publisher(event)
	}
}

// TurnServiceDependencies 描述问答编排所需真实依赖。
type TurnServiceDependencies struct {
	Repository *agentrepository.Repository
	Context    *ContextBuilder
	Provider   port.AgentProvider
	RawRecord  RawRecordFlusher
	IDs        identity.Generator
	Clock      clock.Clock
	Events     TurnEventSink
	Timeout    time.Duration
}

// TurnService 编排 create question → flush → ingest → answer → validate → commit。
type TurnService struct {
	repository *agentrepository.Repository
	context    *ContextBuilder
	provider   port.AgentProvider
	rawRecord  RawRecordFlusher
	ids        identity.Generator
	clock      clock.Clock
	events     TurnEventSink
	timeout    time.Duration
	mu         sync.Mutex
	current    *activeJob
	state      AgentRuntimeState
}

type activeJob struct {
	meetingID      string
	sessionID      string
	localTurnID    string
	providerTurnID string
	cancel         context.CancelFunc
	interruptOnce  sync.Once
}

// AgentRuntimeState 是页面重载可恢复的最小智能体状态。
type AgentRuntimeState struct {
	State          string
	MeetingID      string
	SessionID      string
	TurnID         string
	ProviderTurnID string
	Partial        string
	Approval       *port.PendingAgentApproval
	ErrorCode      string
	Revision       uint64
}

// AskInput 描述主持人机器提交的问题或句首唤醒结果。
type AskInput struct {
	MeetingID          string
	Question           string
	Trigger            string
	TriggerUtteranceID *string
	IdempotencyKey     string
}

// AskResult 是已持久化问题和最终公开回答身份。
type AskResult struct {
	TurnID      string
	QuestionSeq int64
	Answer      string
	AnswerSeq   int64
}

// TimelineEntry 是会中页按 seq 合并统一时间线的 AI 事件。
type TimelineEntry = agentrepository.TimelineEntry

// NewTurnService 创建问答服务；构造阶段不启动 goroutine 或 provider。
func NewTurnService(dependencies TurnServiceDependencies) *TurnService {
	timeout := dependencies.Timeout
	if timeout <= 0 {
		timeout = defaultTurnTimeout
	}
	return &TurnService{
		repository: dependencies.Repository, context: dependencies.Context, provider: dependencies.Provider,
		rawRecord: dependencies.RawRecord, ids: dependencies.IDs, clock: dependencies.Clock,
		events: dependencies.Events, timeout: timeout, state: AgentRuntimeState{State: "unchecked"},
	}
}

// Ask 同步执行一次用户任务；Wails 可并发调用 Interrupt 和 RespondApproval。
func (service *TurnService) Ask(ctx context.Context, input AskInput) (AskResult, error) {
	question, err := validateQuestion(input.Question)
	if err != nil {
		return AskResult{}, err
	}
	if service == nil || service.repository == nil || service.context == nil || service.provider == nil || service.rawRecord == nil || service.ids == nil || service.clock == nil || input.MeetingID == "" || input.IdempotencyKey == "" {
		return AskResult{}, fmt.Errorf("智能体问答服务未初始化")
	}
	session, err := service.repository.GetActiveSessionByMeeting(ctx, input.MeetingID)
	if err != nil {
		return AskResult{}, err
	}
	now := service.clock.Now().UnixMilli()
	created, err := service.repository.CreateQuestion(ctx, agentrepository.CreateQuestionInput{
		Turn: models.AgentTurn{
			ID: service.ids.New(), MeetingID: input.MeetingID, AgentSessionID: session.ID,
			Kind: "answer", State: "pending", IdempotencyKey: input.IdempotencyKey,
			CreatedAt: now, UpdatedAt: now,
		},
		Event: models.MeetingEvent{ID: service.ids.New(), MeetingID: input.MeetingID, OccurredAt: now, CreatedAt: now, UpdatedAt: now},
		Text:  question, Trigger: normalizeTrigger(input.Trigger), UtteranceID: input.TriggerUtteranceID,
		Speaker: "你", UpdatedAt: now,
	})
	if err != nil {
		if errors.Is(err, agentrepository.ErrConflict) {
			return AskResult{}, apperr.Biz(apperr.CodeAgentBusy, apperr.WithOp("agent.turn.create_question"))
		}
		return AskResult{}, err
	}
	if created.Existing {
		return AskResult{TurnID: created.Turn.ID, QuestionSeq: created.Event.Seq}, nil
	}
	jobContext, cancel := context.WithTimeout(ctx, service.timeout)
	job := &activeJob{meetingID: input.MeetingID, sessionID: session.ID, localTurnID: created.Turn.ID, cancel: cancel}
	if err := service.beginJob(job); err != nil {
		cancel()
		return AskResult{}, err
	}
	defer service.endJob(job)
	service.startBusyLongTimer(jobContext, job)

	result, runErr := service.runQuestion(jobContext, job, session, created, question)
	if runErr != nil {
		service.settleFailure(job, runErr)
		return AskResult{}, runErr
	}
	return result, nil
}

// Interrupt 幂等停止当前主持人任务，不建立队列。
func (service *TurnService) Interrupt(ctx context.Context, meetingID string, turnID string) error {
	service.mu.Lock()
	job := service.current
	if job == nil || job.meetingID != meetingID || job.localTurnID != turnID {
		service.mu.Unlock()
		return nil
	}
	providerTurnID := job.providerTurnID
	service.mu.Unlock()
	var result error
	job.interruptOnce.Do(func() {
		job.cancel()
		result = service.provider.InterruptTurn(ctx, port.InterruptAgentTurnRequest{SessionID: job.sessionID, TurnID: providerTurnID})
	})
	return result
}

// InterruptMeeting 停止指定会议当前会中 turn；没有活动 turn 时幂等成功。
func (service *TurnService) InterruptMeeting(ctx context.Context, meetingID string) error {
	if service == nil || meetingID == "" {
		return nil
	}
	service.mu.Lock()
	job := service.current
	service.mu.Unlock()
	if job == nil || job.meetingID != meetingID {
		return nil
	}
	return service.Interrupt(ctx, meetingID, job.localTurnID)
}

// RespondApproval 只允许当前主持人任务响应内存态审批。
func (service *TurnService) RespondApproval(ctx context.Context, request port.RespondAgentApprovalRequest) error {
	service.mu.Lock()
	job := service.current
	valid := job != nil && job.sessionID == request.SessionID && job.providerTurnID == request.TurnID
	service.mu.Unlock()
	if !valid {
		return apperr.Biz(apperr.CodeAgentApprovalExpired, apperr.WithOp("agent.turn.approval"))
	}
	return service.provider.RespondApproval(ctx, request)
}

// State 返回页面重载可恢复的运行态副本。
func (service *TurnService) State() AgentRuntimeState {
	service.mu.Lock()
	defer service.mu.Unlock()
	state := service.state
	if state.Approval != nil {
		copy := *state.Approval
		state.Approval = &copy
	}
	return state
}

// StateFor 优先返回当前内存态；页面重载后从 SQLite 恢复稳定状态和活动 turn 身份。
func (service *TurnService) StateFor(ctx context.Context, meetingID string) (AgentRuntimeState, error) {
	state := service.State()
	if state.MeetingID == meetingID && state.State != "unchecked" {
		return state, nil
	}
	meeting, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return AgentRuntimeState{}, err
	}
	restored := AgentRuntimeState{State: meeting.AgentState, MeetingID: meetingID}
	session, err := service.repository.GetActiveSessionByMeeting(ctx, meetingID)
	if err == nil {
		restored.SessionID = session.ID
	} else if !errors.Is(err, agentrepository.ErrNotFound) {
		return AgentRuntimeState{}, err
	}
	turn, err := service.repository.GetActiveTurnByMeeting(ctx, meetingID)
	if err == nil {
		restored.TurnID = turn.ID
		restored.ProviderTurnID = stringValue(turn.ProviderTurnID)
	} else if !errors.Is(err, agentrepository.ErrNotFound) {
		return AgentRuntimeState{}, err
	}
	return restored, nil
}

// ListTimeline 返回可恢复的持久 AI 事件；partial 仅存在于 State 和持续事件。
func (service *TurnService) ListTimeline(ctx context.Context, meetingID string, afterSeq int64, limit int) ([]TimelineEntry, error) {
	if service == nil || service.repository == nil {
		return nil, fmt.Errorf("智能体问答服务未初始化")
	}
	return service.repository.ListTimeline(ctx, meetingID, afterSeq, limit)
}

// runQuestion 强制刷新记录、切批、顺序 ingest，并只提交最终合法回答。
func (service *TurnService) runQuestion(ctx context.Context, job *activeJob, session models.AgentSession, created agentrepository.CreateQuestionResult, question string) (AskResult, error) {
	if err := service.rawRecord.Flush(ctx, job.meetingID); err != nil {
		return AskResult{}, apperr.Dependency(apperr.CodeAgentContextFlushFailed, err, apperr.WithOp("agent.turn.flush"))
	}
	meeting, err := service.repository.GetMeeting(ctx, job.meetingID)
	if err != nil {
		return AskResult{}, err
	}
	snapshot, throughSeq, err := service.currentSnapshot(ctx, session.ID)
	if err != nil {
		return AskResult{}, err
	}
	request := BuildContextRequest{
		MeetingID: job.meetingID, SessionID: session.ID, MeetingNo: meeting.MeetingNo, Subject: meeting.Subject,
		ThroughSeq: throughSeq, CutoffSeq: created.Event.Seq, Purpose: "answer", Question: question,
		SnapshotJSON: snapshot,
	}
	built, err := service.context.Build(ctx, request)
	if err != nil {
		return AskResult{}, err
	}
	batches, err := service.persistBatches(ctx, job.meetingID, session.ID, built.Batches)
	if err != nil {
		return AskResult{}, err
	}
	for index := 0; index+1 < len(batches); index++ {
		validated, providerTurnID, executeErr := service.executeBatch(ctx, job, batches[index], port.AgentTurnIngest, built)
		if executeErr != nil {
			return AskResult{}, executeErr
		}
		snapshot = validated.SnapshotJSON
		request.SnapshotJSON = snapshot
		if err := service.repository.CommitIngest(ctx, job.localTurnID, providerTurnID, batches[index].ID,
			service.snapshotModel(session, job.localTurnID, batches[index].Context.ToSeq, validated), service.clock.Now().UnixMilli()); err != nil {
			return AskResult{}, err
		}
	}
	if len(batches) == 0 {
		return AskResult{}, fmt.Errorf("问题 cutoff 没有对应会议事件")
	}
	last := batches[len(batches)-1]
	request.SnapshotJSON = snapshot
	last.Context.Prompt = buildPrompt(request, last.Context.Content, question)
	validated, providerTurnID, err := service.executeBatch(ctx, job, last, port.AgentTurnAnswer, built)
	if err != nil {
		return AskResult{}, err
	}
	answerEvent := models.MeetingEvent{
		ID: service.ids.New(), MeetingID: job.meetingID, OccurredAt: service.clock.Now().UnixMilli(),
		CreatedAt: service.clock.Now().UnixMilli(), UpdatedAt: service.clock.Now().UnixMilli(),
	}
	if err := service.repository.CommitTurnSuccess(ctx, agentrepository.CommitTurnSuccessInput{
		TurnID: job.localTurnID, ProviderTurnID: providerTurnID, AnswerEvent: &answerEvent,
		AnswerText: validated.Answer, Snapshot: service.snapshotModel(session, job.localTurnID, last.Context.ToSeq, validated),
		BatchIDs: []string{last.ID}, UpdatedAt: service.clock.Now().UnixMilli(),
	}); err != nil {
		return AskResult{}, err
	}
	_ = service.rawRecord.MarkDirty(job.meetingID)
	service.updateState(func(state *AgentRuntimeState) {
		state.State, state.Partial, state.Approval, state.ErrorCode = "available", "", nil, ""
	})
	service.publishTimelineChanged(job)
	return AskResult{TurnID: job.localTurnID, QuestionSeq: created.Event.Seq, Answer: validated.Answer, AnswerSeq: answerEvent.Seq}, nil
}

type persistedBatch struct {
	ID      string
	Context ContextBatch
}

// persistBatches 把确定性批次身份写入 SQLite，provider 尚未调用。
func (service *TurnService) persistBatches(ctx context.Context, meetingID string, sessionID string, batches []ContextBatch) ([]persistedBatch, error) {
	result := make([]persistedBatch, 0, len(batches))
	for _, batch := range batches {
		id := service.ids.New()
		now := service.clock.Now().UnixMilli()
		model := models.SyncBatch{
			ID: id, MeetingID: meetingID, AgentSessionID: sessionID,
			FromSeq: batch.FromSeq, ToSeq: batch.ToSeq, IdempotencyKey: batch.IdempotencyKey,
			State: "pending", CreatedAt: now, UpdatedAt: now,
		}
		if err := service.repository.CreateBatch(ctx, model); err != nil {
			if !errors.Is(err, agentrepository.ErrConflict) {
				return nil, err
			}
		}
		result = append(result, persistedBatch{ID: id, Context: batch})
	}
	return result, nil
}

// executeBatch 运行一个 provider work unit，并要求 final output 与 completed 同时存在。
func (service *TurnService) executeBatch(ctx context.Context, job *activeJob, batch persistedBatch, kind port.AgentTurnKind, built ContextBuildResult) (domainagent.ValidatedOutput, string, error) {
	if err := service.repository.MarkBatchRunning(ctx, batch.ID, service.clock.Now().UnixMilli()); err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	schema, err := domainagent.OutputSchema(kind)
	if err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	events, err := service.provider.RunTurn(ctx, port.RunAgentTurnRequest{
		SessionID: job.sessionID, TurnID: job.localTurnID, Kind: kind,
		Input: batch.Context.Prompt, OutputSchema: schema, Deadline: deadlineFromContext(ctx),
	})
	if err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	var final []byte
	providerTurnID := ""
	completed := false
	for event := range events {
		service.publishProviderEvent(job, event)
		switch event.Type {
		case port.AgentEventTurnStarted:
			providerTurnID = event.ProviderTurnID
			if err := service.recordProviderTurn(ctx, job, providerTurnID); err != nil {
				return domainagent.ValidatedOutput{}, "", err
			}
		case port.AgentEventFinalOutput:
			if event.FinalOutput != nil {
				final = append([]byte(nil), event.FinalOutput.JSON...)
			}
		case port.AgentEventCompleted:
			completed = true
		case port.AgentEventCancelled:
			return domainagent.ValidatedOutput{}, providerTurnID, apperr.Biz(apperr.CodeAgentTurnCancelled, apperr.WithOp("agent.turn.cancelled"))
		case port.AgentEventFailed:
			return domainagent.ValidatedOutput{}, providerTurnID, apperr.Dependency(apperr.CodeAgentInitializeFailed, errors.New(event.FailureCode), apperr.WithOp("agent.turn.failed"))
		}
	}
	if !completed || len(final) == 0 || providerTurnID == "" {
		return domainagent.ValidatedOutput{}, providerTurnID, apperr.Dependency(apperr.CodeAgentOutputInvalid, errors.New("final or completed missing"), apperr.WithOp("agent.turn.final"))
	}
	validated, err := domainagent.ValidateOutput(kind, final, domainagent.ReferenceAllowlist{
		Sequences: built.Sequences, URLs: built.URLs, Resources: built.Resources,
	})
	if err != nil {
		return domainagent.ValidatedOutput{}, providerTurnID, apperr.Dependency(apperr.CodeAgentOutputInvalid, err, apperr.WithOp("agent.turn.validate"))
	}
	return validated, providerTurnID, nil
}

// recordProviderTurn 首个 work unit 切 running，后续 ingest/answer 只更新当前 provider ID。
func (service *TurnService) recordProviderTurn(ctx context.Context, job *activeJob, providerTurnID string) error {
	service.mu.Lock()
	first := job.providerTurnID == ""
	job.providerTurnID = providerTurnID
	service.state.ProviderTurnID = providerTurnID
	service.state.Revision++
	service.mu.Unlock()
	if first {
		return service.repository.MarkTurnRunning(ctx, job.localTurnID, providerTurnID, service.clock.Now().UnixMilli())
	}
	return service.repository.SetRunningProviderTurn(ctx, job.localTurnID, providerTurnID, service.clock.Now().UnixMilli())
}

// publishProviderEvent 更新可恢复状态后再交给轻量事件出口。
func (service *TurnService) publishProviderEvent(job *activeJob, event port.AgentEvent) {
	event.MeetingID = job.meetingID
	service.updateState(func(state *AgentRuntimeState) {
		switch event.Type {
		case port.AgentEventAnswerDelta:
			state.Partial += event.Delta
		case port.AgentEventApprovalRequested:
			state.Approval = event.Approval
		case port.AgentEventCompleted, port.AgentEventCancelled, port.AgentEventFailed:
			state.Approval = nil
		}
	})
	event.Revision = service.State().Revision
	if service.events != nil {
		service.events.PublishAgentEvent(event)
	}
}

// publishTimelineChanged 仅在时间线事实事务提交后通知 UI 重新读取持久化结果。
func (service *TurnService) publishTimelineChanged(job *activeJob) {
	if service.events == nil {
		return
	}
	service.events.PublishAgentEvent(port.AgentEvent{
		Type: port.AgentEventTimelineChanged, MeetingID: job.meetingID,
		TurnID: job.localTurnID, Revision: service.State().Revision,
	})
}

// currentSnapshot 返回最后成功 snapshot；首次会话从 through_seq=0 开始。
func (service *TurnService) currentSnapshot(ctx context.Context, sessionID string) ([]byte, int64, error) {
	snapshot, err := service.repository.GetSnapshot(ctx, sessionID)
	if errors.Is(err, agentrepository.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	return []byte(snapshot.ContentJSON), snapshot.ThroughSeq, nil
}

// snapshotModel 构造成功 work unit 的滚动快照事实。
func (service *TurnService) snapshotModel(session models.AgentSession, turnID string, throughSeq int64, validated domainagent.ValidatedOutput) models.ContextSnapshot {
	now := service.clock.Now().UnixMilli()
	return models.ContextSnapshot{
		ID: service.ids.New(), MeetingID: session.MeetingID, AgentSessionID: session.ID, AgentTurnID: turnID,
		ThroughSeq: throughSeq, ContentJSON: string(validated.SnapshotJSON), ContentSHA256: validated.SnapshotSHA256,
		CreatedAt: now, UpdatedAt: now,
	}
}

// beginJob 在进程内取得唯一任务指针，mutex 不包围数据库或 provider I/O。
func (service *TurnService) beginJob(job *activeJob) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.current != nil {
		return apperr.Biz(apperr.CodeAgentBusy, apperr.WithOp("agent.turn.owner"))
	}
	service.current = job
	service.state = AgentRuntimeState{
		State: "busy", MeetingID: job.meetingID, SessionID: job.sessionID, TurnID: job.localTurnID,
		Revision: service.state.Revision + 1,
	}
	return nil
}

// endJob 只清理仍由当前调用持有的任务指针。
func (service *TurnService) endJob(job *activeJob) {
	job.cancel()
	service.mu.Lock()
	if service.current == job {
		service.current = nil
	}
	service.mu.Unlock()
}

// settleFailure 按取消、超时或失败写入无 partial 的稳定事件。
func (service *TurnService) settleFailure(job *activeJob, cause error) {
	state, reason, code := "failed", "provider_failed", apperr.CodeAgentInitializeFailed.ErrorCode
	if errors.Is(cause, context.DeadlineExceeded) {
		state, reason, code = "timed_out", "timeout", apperr.CodeAgentTurnTimeout.ErrorCode
	} else if errors.Is(cause, context.Canceled) || hasErrorCode(cause, apperr.CodeAgentTurnCancelled.ErrorCode) {
		state, reason, code = "cancelled", "user_cancelled", apperr.CodeAgentTurnCancelled.ErrorCode
	}
	now := service.clock.Now().UnixMilli()
	persistErr := service.repository.FailTurn(context.Background(), agentrepository.FailTurnInput{
		TurnID: job.localTurnID, State: state,
		Event:  models.MeetingEvent{ID: service.ids.New(), OccurredAt: now, CreatedAt: now, UpdatedAt: now},
		Reason: reason, ErrorCode: code, UpdatedAt: now,
	})
	service.updateState(func(runtime *AgentRuntimeState) {
		runtime.State, runtime.Partial, runtime.Approval, runtime.ErrorCode = "available", "", nil, code
	})
	if persistErr == nil {
		service.publishTimelineChanged(job)
	}
}

// startBusyLongTimer 只派生 UI 状态，不改变持久事实或总 deadline。
func (service *TurnService) startBusyLongTimer(ctx context.Context, job *activeJob) {
	go func() {
		timer := time.NewTimer(busyLongAfter)
		defer timer.Stop()
		select {
		case <-timer.C:
			service.mu.Lock()
			if service.current == job && service.state.State == "busy" {
				service.state.State = "busy_long"
				service.state.Revision++
			}
			service.mu.Unlock()
		case <-ctx.Done():
		}
	}()
}

// updateState 在短临界区更新运行态版本。
func (service *TurnService) updateState(update func(*AgentRuntimeState)) {
	service.mu.Lock()
	update(&service.state)
	service.state.Revision++
	service.mu.Unlock()
}

// validateQuestion 与前端共用 UTF-8 byte 语义，不静默截断。
func validateQuestion(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !utf8.ValidString(trimmed) || trimmed == "" || len([]byte(trimmed)) > maxQuestionBytes {
		return "", apperr.Biz(apperr.CodeAgentQuestionInvalid, apperr.WithOp("agent.turn.question"))
	}
	return trimmed, nil
}

// normalizeTrigger 只登记手动或已验证唤醒来源。
func normalizeTrigger(value string) string {
	if value == "wake_word" {
		return value
	}
	return "manual"
}

// deadlineFromContext 返回共享用户任务 deadline。
func deadlineFromContext(ctx context.Context) time.Time {
	deadline, _ := ctx.Deadline()
	return deadline
}

// hasErrorCode 判断错误链中的安全 AppError 码。
func hasErrorCode(err error, code string) bool {
	var appError *apperr.AppError
	return errors.As(err, &appError) && appError.ErrorCode == code
}
