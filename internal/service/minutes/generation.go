package minutes

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	domainminutes "meet-sieve/internal/domain/minutes"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
	minutesrepository "meet-sieve/internal/repository/minutes"
	"meet-sieve/models"
)

const defaultGenerationTimeout = 30 * time.Minute

// FactReader 冻结一次生成允许消费的本地事实。
type FactReader interface {
	ReadFactSnapshot(ctx context.Context, meetingID string) (domainminutes.Context, error)
}

// GenerationRawRecordFlusher 保证 provider 读取前原始记录已追上 SQLite。
type GenerationRawRecordFlusher interface {
	Flush(ctx context.Context, meetingID string) error
}

// GenerationEventSink 接收不含正文的纪要状态事件。
type GenerationEventSink interface {
	PublishMinutesChanged(meetingID string, state GenerationState)
}

// GenerationEventSinkFunc 让装配层以函数实现纪要状态事件出口。
type GenerationEventSinkFunc func(meetingID string, state GenerationState)

// PublishMinutesChanged 发布不含正文的纪要运行状态。
func (publisher GenerationEventSinkFunc) PublishMinutesChanged(meetingID string, state GenerationState) {
	if publisher != nil {
		publisher(meetingID, state)
	}
}

// GenerationSessionOwner 恢复原 thread，并在生成结束后关闭 provider。
type GenerationSessionOwner interface {
	EnsurePostMeeting(context.Context, string) (models.AgentSession, error)
	Shutdown(context.Context) error
}

// GenerationDependencies 描述纪要生成所需依赖。
type GenerationDependencies struct {
	Repository      *minutesrepository.Repository
	AgentRepository *agentrepository.Repository
	Facts           FactReader
	Provider        port.AgentProvider
	RawRecord       GenerationRawRecordFlusher
	Projector       *MinuteProjector
	IDs             identity.Generator
	Clock           clock.Clock
	Events          GenerationEventSink
	Sessions        GenerationSessionOwner
	Timeout         time.Duration
}

// GenerationService 编排 preflight → snapshot → run → validate → commit。
type GenerationService struct {
	repository      *minutesrepository.Repository
	agentRepository *agentrepository.Repository
	facts           FactReader
	provider        port.AgentProvider
	rawRecord       GenerationRawRecordFlusher
	projector       *MinuteProjector
	ids             identity.Generator
	clock           clock.Clock
	events          GenerationEventSink
	sessions        GenerationSessionOwner
	timeout         time.Duration
	mu              sync.Mutex
	active          *generationJob
	state           GenerationState
}

type generationJob struct {
	meetingID      string
	sessionID      string
	turnID         string
	providerTurnID string
	cancel         context.CancelFunc
}

// GenerationState 是 UI 可展示、可由 SQLite 补充重建的安全运行状态。
type GenerationState struct {
	State           string
	MeetingID       string
	TurnID          string
	Partial         string
	ErrorCode       string
	ProjectionState string
	Revision        uint64
}

// GenerateInput 描述主持人主动触发的一次生成。
type GenerateInput struct {
	MeetingID     string
	ShowGapNotice bool
	RequestID     string
}

// GenerateResult 返回已提交版本和独立文件投影状态。
type GenerateResult struct {
	Version    models.MinuteVersion
	Projection ProjectionState
}

// NewGenerationService 创建纪要生成服务；构造阶段不启动 goroutine。
func NewGenerationService(dependencies GenerationDependencies) *GenerationService {
	timeout := dependencies.Timeout
	if timeout <= 0 {
		timeout = defaultGenerationTimeout
	}
	return &GenerationService{
		repository: dependencies.Repository, agentRepository: dependencies.AgentRepository,
		facts: dependencies.Facts, provider: dependencies.Provider, rawRecord: dependencies.RawRecord,
		projector: dependencies.Projector, ids: dependencies.IDs, clock: dependencies.Clock,
		events: dependencies.Events, timeout: timeout, state: GenerationState{State: "idle"},
		sessions: dependencies.Sessions,
	}
}

// Generate 同步执行一次用户主动生成，Stop 可并发取消。
func (service *GenerationService) Generate(ctx context.Context, input GenerateInput) (GenerateResult, error) {
	if err := service.validate(input); err != nil {
		return GenerateResult{}, err
	}
	deadlineContext, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()
	if err := service.rawRecord.Flush(deadlineContext, input.MeetingID); err != nil {
		return GenerateResult{}, apperr.Dependency(apperr.CodeAgentContextFlushFailed, err, apperr.WithOp("minutes.flush"))
	}
	snapshot, err := service.facts.ReadFactSnapshot(deadlineContext, input.MeetingID)
	if err != nil {
		return GenerateResult{}, err
	}
	if hasProcessingGap(snapshot.Gaps) {
		return GenerateResult{}, apperr.Biz(apperr.CodeMinutesGapProcessing, apperr.WithOp("minutes.preflight.gap"))
	}
	var session models.AgentSession
	if service.sessions != nil {
		session, err = service.sessions.EnsurePostMeeting(deadlineContext, input.MeetingID)
		if err != nil {
			return GenerateResult{}, err
		}
		defer service.closeSession()
	} else {
		session, err = service.agentRepository.GetActiveSessionByMeeting(deadlineContext, input.MeetingID)
		if err != nil {
			return GenerateResult{}, err
		}
	}
	turn := models.AgentTurn{ID: service.ids.New(), MeetingID: input.MeetingID, AgentSessionID: session.ID, Kind: "minutes", State: "pending", IdempotencyKey: input.RequestID, CreatedAt: service.clock.Now().UnixMilli(), UpdatedAt: service.clock.Now().UnixMilli()}
	created, err := service.repository.BeginMinutesTurn(deadlineContext, turn)
	if err != nil {
		return GenerateResult{}, mapMinutesConflict(err)
	}
	if created.Existing {
		return service.existingResult(deadlineContext, created.Turn)
	}
	job := &generationJob{meetingID: input.MeetingID, sessionID: session.ID, turnID: turn.ID, cancel: cancel}
	if err := service.begin(job); err != nil {
		service.fail(job, err)
		return GenerateResult{}, err
	}
	defer service.end(job)
	payload, validation, err := BuildProviderInput(snapshot)
	if err != nil {
		service.fail(job, err)
		return GenerateResult{}, err
	}
	result, runErr := service.run(deadlineContext, job, payload, validation)
	if runErr != nil {
		service.fail(job, runErr)
		return GenerateResult{}, runErr
	}
	return result, nil
}

// closeSession 使用独立短截止关闭纪要 provider，不受生成 context 取消影响。
func (service *GenerationService) closeSession() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = service.sessions.Shutdown(ctx)
}

// Stop 幂等取消当前纪要生成，并尝试中断 provider turn。
func (service *GenerationService) Stop(ctx context.Context, meetingID string, turnID string) error {
	service.mu.Lock()
	job := service.active
	if job == nil || job.meetingID != meetingID || job.turnID != turnID {
		service.mu.Unlock()
		return nil
	}
	providerTurnID := job.providerTurnID
	job.cancel()
	service.mu.Unlock()
	if providerTurnID == "" {
		return nil
	}
	return service.provider.InterruptTurn(ctx, port.InterruptAgentTurnRequest{SessionID: job.sessionID, TurnID: providerTurnID})
}

// StopMeeting 幂等停止指定会议当前纪要生成，无需调用方持有 turn ID。
func (service *GenerationService) StopMeeting(ctx context.Context, meetingID string) error {
	if service == nil || meetingID == "" {
		return nil
	}
	service.mu.Lock()
	job := service.active
	service.mu.Unlock()
	if job == nil || job.meetingID != meetingID {
		return nil
	}
	return service.Stop(ctx, meetingID, job.turnID)
}

// State 返回内存 partial 的安全副本。
func (service *GenerationService) State() GenerationState {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.state
}

// run 执行 provider 并要求 final output 与 completed 同时存在。
func (service *GenerationService) run(ctx context.Context, job *generationJob, payload []byte, validation domainminutes.ValidationContext) (GenerateResult, error) {
	schema, err := domainminutes.OutputSchema()
	if err != nil {
		return GenerateResult{}, err
	}
	events, err := service.provider.RunTurn(ctx, port.RunAgentTurnRequest{SessionID: job.sessionID, TurnID: job.turnID, Kind: port.AgentTurnMinutes, Input: buildMinutesPrompt(payload), OutputSchema: schema, Deadline: deadlineFromContext(ctx)})
	if err != nil {
		return GenerateResult{}, err
	}
	var final []byte
	completed := false
	slowTimer := time.NewTimer(30 * time.Second)
	defer slowTimer.Stop()
	for events != nil {
		select {
		case <-ctx.Done():
			return GenerateResult{}, ctx.Err()
		case <-slowTimer.C:
			service.updateState(job.meetingID, func(state *GenerationState) { state.State = "slow" })
		case event, open := <-events:
			if !open {
				events = nil
				continue
			}
			if err := service.consumeEvent(ctx, job, event, &final, &completed); err != nil {
				return GenerateResult{}, err
			}
		}
	}
	// provider 因取消或截止关闭事件流时，必须保留真实终态，不能误报输出校验失败。
	if err := ctx.Err(); err != nil {
		return GenerateResult{}, err
	}
	if len(final) == 0 || !completed || job.providerTurnID == "" {
		return GenerateResult{}, apperr.Dependency(apperr.CodeMinutesOutputInvalid, errors.New("minutes final missing"), apperr.WithOp("minutes.output"))
	}
	output, err := domainminutes.ParseAndValidateOutput(final, validation)
	if err != nil {
		return GenerateResult{}, apperr.Dependency(apperr.CodeMinutesOutputInvalid, err, apperr.WithOp("minutes.validate"))
	}
	markdown, err := domainminutes.RenderMarkdown(output, validation)
	if err != nil {
		return GenerateResult{}, apperr.Dependency(apperr.CodeMinutesOutputInvalid, err, apperr.WithOp("minutes.render"))
	}
	version, err := service.repository.CommitGeneratedMinute(ctx, minutesrepository.CommitGeneratedMinuteInput{VersionID: service.ids.New(), TurnID: job.turnID, ProviderTurnID: job.providerTurnID, ContentMarkdown: string(markdown), UpdatedAt: service.clock.Now().UnixMilli()})
	if err != nil {
		return GenerateResult{}, mapMinutesConflict(err)
	}
	projection := service.project(ctx, job.meetingID)
	service.updateState(job.meetingID, func(state *GenerationState) {
		state.State, state.Partial, state.ErrorCode, state.ProjectionState = "completed", "", "", projection.State
	})
	return GenerateResult{Version: version, Projection: projection}, nil
}

// consumeEvent 更新轻量 partial，并拒绝失败或取消事件。
func (service *GenerationService) consumeEvent(ctx context.Context, job *generationJob, event port.AgentEvent, final *[]byte, completed *bool) error {
	switch event.Type {
	case port.AgentEventTurnStarted:
		if event.ProviderTurnID == "" {
			return fmt.Errorf("纪要 provider turn ID 为空")
		}
		if err := service.repository.MarkMinutesTurnRunning(ctx, job.turnID, event.ProviderTurnID, service.clock.Now().UnixMilli()); err != nil {
			return err
		}
		service.mu.Lock()
		job.providerTurnID = event.ProviderTurnID
		service.mu.Unlock()
	case port.AgentEventAnswerDelta:
		service.updateState(job.meetingID, func(state *GenerationState) { state.Partial += event.Delta })
	case port.AgentEventFinalOutput:
		if event.FinalOutput != nil {
			*final = append([]byte(nil), event.FinalOutput.JSON...)
		}
	case port.AgentEventCompleted:
		*completed = true
	case port.AgentEventFailed:
		return fmt.Errorf("纪要 provider 执行失败")
	case port.AgentEventCancelled:
		return context.Canceled
	}
	return nil
}

// begin 取得进程内唯一生成 owner。
func (service *GenerationService) begin(job *generationJob) error {
	service.mu.Lock()
	if service.active != nil {
		service.mu.Unlock()
		return apperr.Biz(apperr.CodeMinutesBusy, apperr.WithOp("minutes.owner"))
	}
	service.active = job
	service.state = GenerationState{State: "generating", MeetingID: job.meetingID, TurnID: job.turnID, Revision: service.state.Revision + 1}
	state := service.state
	service.mu.Unlock()
	service.publish(state)
	return nil
}

// end 仅清理仍由本次调用持有的 owner。
func (service *GenerationService) end(job *generationJob) {
	service.mu.Lock()
	if service.active == job {
		service.active = nil
	}
	service.mu.Unlock()
}

// fail 使用 CAS 收敛 turn，迟到事件不能覆盖成功或已停止状态。
func (service *GenerationService) fail(job *generationJob, cause error) {
	state, code := "failed", apperr.CodeMinutesOutputInvalid.ErrorCode
	if errors.Is(cause, context.DeadlineExceeded) {
		state, code = "timed_out", apperr.CodeAgentTurnTimeout.ErrorCode
	} else if errors.Is(cause, context.Canceled) {
		state, code = "cancelled", apperr.CodeAgentTurnCancelled.ErrorCode
	}
	_ = service.repository.FailMinutesTurn(context.Background(), minutesrepository.FailMinutesTurnInput{TurnID: job.turnID, State: state, ErrorCode: code, UpdatedAt: service.clock.Now().UnixMilli()})
	service.updateState(job.meetingID, func(value *GenerationState) { value.State, value.Partial, value.ErrorCode = state, "", code })
}

// updateState 更新安全运行态并发布不含正文的状态事件。
func (service *GenerationService) updateState(meetingID string, update func(*GenerationState)) {
	service.mu.Lock()
	update(&service.state)
	service.state.MeetingID, service.state.Revision = meetingID, service.state.Revision+1
	state := service.state
	service.mu.Unlock()
	service.publish(state)
}

// publish 在锁外发布不含 partial 正文的状态副本。
func (service *GenerationService) publish(state GenerationState) {
	if service.events != nil {
		state.Partial = ""
		service.events.PublishMinutesChanged(state.MeetingID, state)
	}
}

// project 刷新 current 文件，失败不回滚已提交版本。
func (service *GenerationService) project(ctx context.Context, meetingID string) ProjectionState {
	meeting, err := service.agentRepository.GetMeeting(ctx, meetingID)
	if err != nil || service.projector == nil {
		return ProjectionState{State: "failed", ErrorCode: apperr.CodeMinutesProjectionFailed.ErrorCode}
	}
	if err := service.projector.Flush(ctx, meeting); err != nil {
		return service.projector.State(meetingID)
	}
	return service.projector.State(meetingID)
}

// existingResult 复用相同 request ID 的终态结果。
func (service *GenerationService) existingResult(ctx context.Context, turn models.AgentTurn) (GenerateResult, error) {
	if turn.State == "completed" {
		versions, err := service.repository.ListVersions(ctx, turn.MeetingID, 0, 100)
		if err != nil {
			return GenerateResult{}, err
		}
		for _, version := range versions {
			if version.AgentTurnID != nil && *version.AgentTurnID == turn.ID {
				return GenerateResult{Version: version, Projection: service.projector.State(turn.MeetingID)}, nil
			}
		}
	}
	return GenerateResult{}, apperr.Biz(apperr.CodeMinutesBusy, apperr.WithOp("minutes.idempotency"))
}

// validate 检查服务依赖和基础命令字段。
func (service *GenerationService) validate(input GenerateInput) error {
	if service == nil || service.repository == nil || service.agentRepository == nil || service.facts == nil || service.provider == nil || service.rawRecord == nil || service.ids == nil || service.clock == nil || input.MeetingID == "" || input.RequestID == "" {
		return fmt.Errorf("纪要生成服务未初始化或参数无效")
	}
	return nil
}

// hasProcessingGap 判断事实快照是否仍在变化。
func hasProcessingGap(gaps []domainminutes.GapNotice) bool {
	for _, gap := range gaps {
		if gap.State == "processing" {
			return true
		}
	}
	return false
}

// buildMinutesPrompt 只包含白名单 JSON，并禁止目录遍历取事实。
func buildMinutesPrompt(payload []byte) string {
	return "请仅根据以下 MeetSieve 白名单事实生成符合 schema 的会议纪要 JSON。不得读取目录文件补充事实；gap_notice 必须原样返回。\n" + string(payload)
}

// deadlineFromContext 返回 provider 必须遵守的硬截止。
func deadlineFromContext(ctx context.Context) time.Time {
	deadline, _ := ctx.Deadline()
	return deadline
}

// mapMinutesConflict 把事务来源变化转换为稳定业务错误。
func mapMinutesConflict(err error) error {
	if errors.Is(err, minutesrepository.ErrConflict) {
		return apperr.Biz(apperr.CodeMinutesVersionConflict, apperr.WithOp("minutes.commit"))
	}
	return err
}
