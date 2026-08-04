package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
	"meet-sieve/models"
)

const finalSyncTimeout = 10 * time.Minute

// PostMeetingSessionOwner 确保会后 thread 可用，并唯一负责关闭 provider。
type PostMeetingSessionOwner interface {
	EnsurePostMeeting(context.Context, string) (models.AgentSession, error)
	Shutdown(context.Context) error
}

// FinalSyncDependencies 描述结束同步所需的 Step 7 复用能力。
type FinalSyncDependencies struct {
	Repository *agentrepository.Repository
	Context    *ContextBuilder
	Provider   port.AgentProvider
	RawRecord  RawRecordFlusher
	Sessions   PostMeetingSessionOwner
	IDs        identity.Generator
	Clock      clock.Clock
	Timeout    time.Duration
	Events     FinalSyncEventSink
}

// FinalSyncState 是结束同步事件允许暴露的稳定状态。
type FinalSyncState struct {
	MeetingID string
	State     string
	ErrorCode string
	Revision  uint64
}

// FinalSyncEventSink 接收不含上下文和 provider 内容的同步状态。
type FinalSyncEventSink interface {
	PublishFinalSyncChanged(FinalSyncState)
}

// FinalSyncEventSinkFunc 让装配层以函数实现结束同步事件出口。
type FinalSyncEventSinkFunc func(FinalSyncState)

// PublishFinalSyncChanged 发布一条安全状态。
func (publisher FinalSyncEventSinkFunc) PublishFinalSyncChanged(state FinalSyncState) {
	if publisher != nil {
		publisher(state)
	}
}

// FinalSyncService 在固定 cutoff 前只执行 ingest，不创建公开回答。
type FinalSyncService struct {
	repository *agentrepository.Repository
	context    *ContextBuilder
	provider   port.AgentProvider
	rawRecord  RawRecordFlusher
	sessions   PostMeetingSessionOwner
	ids        identity.Generator
	clock      clock.Clock
	timeout    time.Duration
	events     FinalSyncEventSink
	eventMu    sync.Mutex
	revisions  map[string]uint64
}

// NewFinalSyncService 创建结束同步服务。
func NewFinalSyncService(dependencies FinalSyncDependencies) *FinalSyncService {
	timeout := dependencies.Timeout
	if timeout <= 0 {
		timeout = finalSyncTimeout
	}
	return &FinalSyncService{repository: dependencies.Repository, context: dependencies.Context, provider: dependencies.Provider, rawRecord: dependencies.RawRecord, sessions: dependencies.Sessions, ids: dependencies.IDs, clock: dependencies.Clock, timeout: timeout, events: dependencies.Events, revisions: make(map[string]uint64)}
}

// SyncFinal 自动结束同步使用稳定幂等 key。
func (service *FinalSyncService) SyncFinal(ctx context.Context, meetingID string) error {
	return service.sync(ctx, meetingID, "final-sync:"+meetingID)
}

// RetryFinalSync 由主持人显式恢复原 thread 和未成功游标。
func (service *FinalSyncService) RetryFinalSync(ctx context.Context, meetingID string, requestID string) error {
	if requestID == "" {
		return fmt.Errorf("重试 Codex 结束同步：request ID 为空")
	}
	// turn key 保持稳定，request ID 只作为用户动作边界，不创建新的同步范围。
	return service.sync(ctx, meetingID, "final-sync:"+meetingID)
}

// sync 执行 flush → fixed cutoff → batched ingest → close。
func (service *FinalSyncService) sync(ctx context.Context, meetingID string, key string) error {
	if service == nil || service.repository == nil || service.context == nil || service.provider == nil || service.rawRecord == nil || service.sessions == nil || service.ids == nil || service.clock == nil || meetingID == "" {
		return fmt.Errorf("Codex 结束同步服务未初始化")
	}
	enabled, err := service.repository.HasSuccessfulAgentSession(ctx, meetingID)
	if err != nil || !enabled {
		return err
	}
	service.publish(meetingID, "syncing", "")
	jobContext, cancel := context.WithTimeout(ctx, service.timeout)
	defer cancel()
	session, err := service.sessions.EnsurePostMeeting(jobContext, meetingID)
	if err != nil {
		service.failBeforeTurn(meetingID, err)
		return err
	}
	defer service.closeSession()
	if err := service.rawRecord.Flush(jobContext, meetingID); err != nil {
		service.failBeforeTurn(meetingID, err)
		return apperr.Dependency(apperr.CodeAgentContextFlushFailed, err, apperr.WithOp("agent.final_sync.flush"))
	}
	meeting, err := service.repository.GetMeeting(jobContext, meetingID)
	if err != nil {
		service.failBeforeTurn(meetingID, err)
		return err
	}
	snapshotJSON, throughSeq := service.snapshotForSession(jobContext, session)
	cutoff, err := service.repository.LatestEventSeq(jobContext, meetingID)
	if err != nil {
		service.failBeforeTurn(meetingID, err)
		return err
	}
	turn := models.AgentTurn{ID: service.ids.New(), MeetingID: meetingID, AgentSessionID: session.ID, Kind: "ingest", State: "pending", IdempotencyKey: key, CreatedAt: service.clock.Now().UnixMilli(), UpdatedAt: service.clock.Now().UnixMilli()}
	claimed, err := service.repository.BeginFinalSync(jobContext, turn)
	if err != nil {
		service.failBeforeTurn(meetingID, err)
		return err
	}
	if claimed.Completed {
		service.publish(meetingID, "unavailable", "")
		return nil
	}
	turn = claimed.Turn
	request := BuildContextRequest{MeetingID: meetingID, SessionID: session.ID, MeetingNo: meeting.MeetingNo, Subject: meeting.Subject, ThroughSeq: throughSeq, CutoffSeq: cutoff, Purpose: "final_sync", SnapshotJSON: snapshotJSON}
	built, err := service.context.Build(jobContext, request)
	if err != nil {
		service.fail(turn.ID, err)
		service.publish(meetingID, "unsynced", stableFinalSyncCode(err))
		return err
	}
	if len(built.Batches) == 0 {
		err = service.repository.CompleteFinalSyncNoChanges(jobContext, turn.ID, service.clock.Now().UnixMilli())
		service.publishFinalResult(meetingID, err)
		return err
	}
	batches, err := service.persistFinalBatches(jobContext, meetingID, session.ID, built.Batches)
	if err != nil {
		service.fail(turn.ID, err)
		service.publish(meetingID, "unsynced", stableFinalSyncCode(err))
		return err
	}
	first := true
	for index, batch := range batches {
		request.SnapshotJSON = snapshotJSON
		batch.Context.Prompt = buildPrompt(request, batch.Context.Content, "")
		validated, providerTurnID, executeErr := service.executeFinalBatch(jobContext, turn.ID, session, batch, built, first)
		if executeErr != nil {
			service.fail(turn.ID, executeErr)
			service.publish(meetingID, "unsynced", stableFinalSyncCode(executeErr))
			return executeErr
		}
		first = false
		snapshotJSON = validated.SnapshotJSON
		snapshot := service.finalSnapshot(session, turn.ID, batch.Context.ToSeq, validated)
		if index+1 < len(batches) {
			err = service.repository.CommitIngest(jobContext, turn.ID, providerTurnID, batch.ID, snapshot, service.clock.Now().UnixMilli())
		} else {
			err = service.repository.CompleteFinalSync(jobContext, turn.ID, providerTurnID, batch.ID, snapshot, service.clock.Now().UnixMilli())
		}
		if err != nil {
			service.fail(turn.ID, err)
			service.publish(meetingID, "unsynced", stableFinalSyncCode(err))
			return err
		}
	}
	service.publish(meetingID, "unavailable", "")
	return nil
}

// failBeforeTurn 处理 turn 尚未创建时的同步失败并发布稳定错误。
func (service *FinalSyncService) failBeforeTurn(meetingID string, cause error) {
	service.markUnsynced(meetingID)
	service.publish(meetingID, "unsynced", stableFinalSyncCode(cause))
}

// publishFinalResult 发布无新增事实路径的终态。
func (service *FinalSyncService) publishFinalResult(meetingID string, err error) {
	if err == nil {
		service.publish(meetingID, "unavailable", "")
		return
	}
	service.failBeforeTurn(meetingID, err)
}

// publish 为每场会议分配进程内单调 revision，并隐藏底层错误。
func (service *FinalSyncService) publish(meetingID string, state string, errorCode string) {
	if service.events == nil {
		return
	}
	service.eventMu.Lock()
	service.revisions[meetingID]++
	revision := service.revisions[meetingID]
	service.eventMu.Unlock()
	service.events.PublishFinalSyncChanged(FinalSyncState{MeetingID: meetingID, State: state, ErrorCode: errorCode, Revision: revision})
}

// stableFinalSyncCode 只返回可公开的稳定错误码。
func stableFinalSyncCode(cause error) string {
	var appError *apperr.AppError
	if errors.As(cause, &appError) {
		return appError.ErrorCode
	}
	return apperr.CodeAgentFinalSyncFailed.ErrorCode
}

// markUnsynced 只记录独立同步状态，不改变本地保存和 gap 状态。
func (service *FinalSyncService) markUnsynced(meetingID string) {
	_ = service.repository.MarkFinalSyncUnsynced(context.Background(), meetingID, service.clock.Now().UnixMilli())
}

// persistFinalBatches 复用相同 session 的原 batch，失败批次只由显式 retry 重置。
func (service *FinalSyncService) persistFinalBatches(ctx context.Context, meetingID string, sessionID string, values []ContextBatch) ([]persistedBatch, error) {
	existing, err := service.repository.ListBatches(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]models.SyncBatch, len(existing))
	for _, batch := range existing {
		byKey[batch.IdempotencyKey] = batch
	}
	result := make([]persistedBatch, 0, len(values))
	for _, value := range values {
		if batch, found := byKey[value.IdempotencyKey]; found {
			if batch.State == "failed" {
				if err := service.repository.ResetBatchForRetry(ctx, batch.ID, service.clock.Now().UnixMilli()); err != nil {
					return nil, err
				}
			} else if batch.State == "completed" {
				continue
			} else if batch.State != "pending" {
				return nil, agentrepository.ErrConflict
			}
			result = append(result, persistedBatch{ID: batch.ID, Context: value})
			continue
		}
		batch := models.SyncBatch{ID: service.ids.New(), MeetingID: meetingID, AgentSessionID: sessionID, FromSeq: value.FromSeq, ToSeq: value.ToSeq, IdempotencyKey: value.IdempotencyKey, State: "pending", CreatedAt: service.clock.Now().UnixMilli(), UpdatedAt: service.clock.Now().UnixMilli()}
		if err := service.repository.CreateBatch(ctx, batch); err != nil {
			return nil, err
		}
		result = append(result, persistedBatch{ID: batch.ID, Context: value})
	}
	return result, nil
}

// executeFinalBatch 运行一次 ingest，并验证新的滚动 snapshot。
func (service *FinalSyncService) executeFinalBatch(ctx context.Context, turnID string, session models.AgentSession, batch persistedBatch, built ContextBuildResult, first bool) (domainagent.ValidatedOutput, string, error) {
	if err := service.repository.MarkBatchRunning(ctx, batch.ID, service.clock.Now().UnixMilli()); err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	schema, err := buildAgentOutputSchema(port.AgentTurnIngest, built)
	if err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	events, err := service.provider.RunTurn(ctx, port.RunAgentTurnRequest{SessionID: session.ID, TurnID: turnID, Kind: port.AgentTurnIngest, Input: appendReferenceInstructions(batch.Context.Prompt, built), OutputSchema: schema, Deadline: deadlineFromContext(ctx)})
	if err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	providerTurnID, final, completed := "", []byte(nil), false
	for event := range events {
		switch event.Type {
		case port.AgentEventTurnStarted:
			providerTurnID = event.ProviderTurnID
			if first {
				err = service.repository.MarkTurnRunning(ctx, turnID, providerTurnID, service.clock.Now().UnixMilli())
			} else {
				err = service.repository.SetRunningProviderTurn(ctx, turnID, providerTurnID, service.clock.Now().UnixMilli())
			}
			if err != nil {
				return domainagent.ValidatedOutput{}, "", err
			}
		case port.AgentEventFinalOutput:
			if event.FinalOutput != nil {
				final = append([]byte(nil), event.FinalOutput.JSON...)
			}
		case port.AgentEventCompleted:
			completed = true
		case port.AgentEventFailed, port.AgentEventCancelled:
			return domainagent.ValidatedOutput{}, providerTurnID, fmt.Errorf("Codex 结束同步失败")
		}
	}
	if providerTurnID == "" || len(final) == 0 || !completed {
		return domainagent.ValidatedOutput{}, providerTurnID, fmt.Errorf("Codex 结束同步输出不完整")
	}
	validated, validateErr := domainagent.ValidateOutput(port.AgentTurnIngest, final, referenceAllowlist(built))
	if validateErr != nil {
		return domainagent.ValidatedOutput{}, providerTurnID, apperr.Dependency(apperr.CodeAgentOutputInvalid, validateErr, apperr.WithOp("agent.final_sync.output.validate"))
	}
	return validated, providerTurnID, nil
}

// snapshotForSession 从当前或恢复来源 session 获取成功游标。
func (service *FinalSyncService) snapshotForSession(ctx context.Context, session models.AgentSession) ([]byte, int64) {
	snapshot, err := service.repository.GetSnapshot(ctx, session.ID)
	if err == nil {
		return []byte(snapshot.ContentJSON), snapshot.ThroughSeq
	}
	if session.ResumedFromSessionID != nil {
		snapshot, err = service.repository.GetSnapshot(ctx, *session.ResumedFromSessionID)
		if err == nil {
			return []byte(snapshot.ContentJSON), snapshot.ThroughSeq
		}
	}
	return nil, 0
}

// finalSnapshot 构造结束同步滚动快照。
func (service *FinalSyncService) finalSnapshot(session models.AgentSession, turnID string, throughSeq int64, output domainagent.ValidatedOutput) models.ContextSnapshot {
	now := service.clock.Now().UnixMilli()
	return models.ContextSnapshot{ID: service.ids.New(), MeetingID: session.MeetingID, AgentSessionID: session.ID, AgentTurnID: turnID, ThroughSeq: throughSeq, ContentJSON: string(output.SnapshotJSON), ContentSHA256: output.SnapshotSHA256, CreatedAt: now, UpdatedAt: now}
}

// fail 仅持久化稳定失败码。
func (service *FinalSyncService) fail(turnID string, cause error) {
	code := apperr.CodeAgentFinalSyncFailed.ErrorCode
	var appError *apperr.AppError
	if errors.As(cause, &appError) {
		code = appError.ErrorCode
	}
	_ = service.repository.FailFinalSync(context.Background(), turnID, code, service.clock.Now().UnixMilli())
}

// closeSession 使用独立短截止保证请求取消后仍尝试关闭 provider。
func (service *FinalSyncService) closeSession() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = service.sessions.Shutdown(ctx)
}
