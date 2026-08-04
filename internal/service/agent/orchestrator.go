package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
	"meet-sieve/models"
)

// OrchestratorDependencies 描述会议级 session owner 的真实依赖。
type OrchestratorDependencies struct {
	Repository    *agentrepository.Repository
	Context       *ContextBuilder
	Provider      port.AgentProvider
	RawRecord     RawRecordFlusher
	IDs           identity.Generator
	Clock         clock.Clock
	WorkspaceRoot string
	Executable    func(context.Context) (string, error)
}

// Orchestrator 持有进程内唯一活动 session；mutex 不包围数据库或 RPC I/O。
type Orchestrator struct {
	repository *agentrepository.Repository
	context    *ContextBuilder
	provider   port.AgentProvider
	rawRecord  RawRecordFlusher
	ids        identity.Generator
	clock      clock.Clock
	workspace  string
	executable func(context.Context) (string, error)
	mu         sync.Mutex
	current    *port.AgentSession
	initCancel context.CancelFunc
	initDone   chan struct{}
	retrying   bool
}

// NewOrchestrator 创建 session owner；构造阶段不启动 provider。
func NewOrchestrator(dependencies OrchestratorDependencies) *Orchestrator {
	return &Orchestrator{
		repository: dependencies.Repository, context: dependencies.Context, provider: dependencies.Provider,
		rawRecord: dependencies.RawRecord, ids: dependencies.IDs, clock: dependencies.Clock,
		workspace:  dependencies.WorkspaceRoot,
		executable: dependencies.Executable,
	}
}

// Initialize 在录音提交点之后初始化新 thread；失败不改变录音、ASR 或 LAN。
func (orchestrator *Orchestrator) Initialize(ctx context.Context, meetingID string) error {
	return orchestrator.initialize(ctx, meetingID, nil)
}

// Retry 优先恢复最近 thread；不存在时创建新 thread 并从本地快照恢复。
func (orchestrator *Orchestrator) Retry(ctx context.Context, meetingID string) error {
	if err := orchestrator.beginRetry(); err != nil {
		return err
	}
	defer orchestrator.finishRetry()
	// 进程退出后 owner 仍可能保留旧 session；先幂等收口，才能创建唯一恢复 session。
	if err := orchestrator.Shutdown(ctx); err != nil {
		return err
	}
	latest, err := orchestrator.repository.GetLatestSession(ctx, meetingID)
	if err != nil && !errors.Is(err, agentrepository.ErrNotFound) {
		return err
	}
	if errors.Is(err, agentrepository.ErrNotFound) {
		return orchestrator.Initialize(ctx, meetingID)
	}
	return orchestrator.initialize(ctx, meetingID, &latest)
}

// EnsurePostMeeting 确保已保存会议拥有可用于结束同步或纪要的 provider session。
func (orchestrator *Orchestrator) EnsurePostMeeting(ctx context.Context, meetingID string) (models.AgentSession, error) {
	if orchestrator == nil || orchestrator.repository == nil || orchestrator.provider == nil || orchestrator.ids == nil || orchestrator.clock == nil || meetingID == "" {
		return models.AgentSession{}, fmt.Errorf("会后 Codex session 编排器未初始化")
	}
	orchestrator.mu.Lock()
	hasOwner := orchestrator.current != nil
	orchestrator.mu.Unlock()
	if hasOwner {
		return orchestrator.repository.GetActiveSessionByMeeting(ctx, meetingID)
	}
	// 进程恢复后数据库可能残留 available session，但 provider 已不存在，先收敛再恢复 thread。
	if active, err := orchestrator.repository.GetActiveSessionByMeeting(ctx, meetingID); err == nil {
		_ = orchestrator.repository.EndSession(ctx, active.ID, nil, orchestrator.clock.Now().UnixMilli())
	}
	previousValue, err := orchestrator.repository.GetLatestSession(ctx, meetingID)
	if err != nil && !errors.Is(err, agentrepository.ErrNotFound) {
		return models.AgentSession{}, err
	}
	var previous *models.AgentSession
	if err == nil {
		previous = &previousValue
	}
	meeting, err := orchestrator.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return models.AgentSession{}, err
	}
	workingDirectory, err := trustedMeetingDirectory(orchestrator.workspace, meeting.RelativeDir)
	if err != nil {
		return models.AgentSession{}, err
	}
	cancelContext, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	if err := orchestrator.reserveOwner(cancel, done); err != nil {
		cancel()
		return models.AgentSession{}, err
	}
	defer orchestrator.finishInitializeOwner(cancel, done)
	success := false
	defer func() {
		if !success {
			orchestrator.releaseOwner()
		}
	}()
	if previous != nil && previous.ThreadID != nil && *previous.ThreadID != "" {
		executable := ""
		if orchestrator.executable != nil {
			executable, err = orchestrator.executable(cancelContext)
			if err != nil {
				return models.AgentSession{}, err
			}
		}
		resumed, resumeErr := orchestrator.provider.ResumeSession(cancelContext, port.ResumeAgentSessionRequest{SessionID: previous.ID, ProviderSessionID: *previous.ThreadID, WorkingDirectory: workingDirectory, ExecutablePath: executable})
		if resumeErr == nil {
			if err := orchestrator.repository.ReopenPostMeetingSession(cancelContext, previous.ID, resumed.ProviderSessionID, orchestrator.clock.Now().UnixMilli()); err != nil {
				_ = orchestrator.provider.CloseSession(context.Background(), previous.ID)
				return models.AgentSession{}, err
			}
			previous.ThreadID, previous.State, previous.EndedAt, previous.LastErrorCode = &resumed.ProviderSessionID, "available", nil, nil
			orchestrator.mu.Lock()
			orchestrator.current = &resumed
			orchestrator.mu.Unlock()
			success = true
			return *previous, nil
		}
		if !hasErrorCode(resumeErr, apperr.CodeAgentThreadNotFound.ErrorCode) {
			return models.AgentSession{}, resumeErr
		}
	}
	now := orchestrator.clock.Now().UnixMilli()
	var resumedFrom *string
	if previous != nil {
		resumedFrom = &previous.ID
	}
	session := models.AgentSession{ID: orchestrator.ids.New(), MeetingID: meetingID, Provider: "codex", CWDRelativePath: meeting.RelativeDir, State: "starting", ResumedFromSessionID: resumedFrom, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := orchestrator.repository.BeginPostMeetingSession(cancelContext, session); err != nil {
		return models.AgentSession{}, err
	}
	providerSession, err := orchestrator.openProviderSession(cancelContext, session.ID, workingDirectory, previous)
	if err != nil {
		orchestrator.failPostMeetingSession(session.ID, err)
		return models.AgentSession{}, err
	}
	if err := orchestrator.repository.SetSessionThread(cancelContext, session.ID, providerSession.ProviderSessionID, orchestrator.clock.Now().UnixMilli()); err != nil {
		_ = orchestrator.provider.CloseSession(context.Background(), session.ID)
		orchestrator.failPostMeetingSession(session.ID, err)
		return models.AgentSession{}, err
	}
	if err := orchestrator.repository.ActivatePostMeetingSession(cancelContext, session.ID, orchestrator.clock.Now().UnixMilli()); err != nil {
		_ = orchestrator.provider.CloseSession(context.Background(), session.ID)
		orchestrator.failPostMeetingSession(session.ID, err)
		return models.AgentSession{}, err
	}
	session.ThreadID, session.State = &providerSession.ProviderSessionID, "available"
	orchestrator.mu.Lock()
	orchestrator.current = &providerSession
	orchestrator.mu.Unlock()
	success = true
	return session, nil
}

// failPostMeetingSession 收敛会后 starting session，不改变本地保存事实。
func (orchestrator *Orchestrator) failPostMeetingSession(sessionID string, cause error) {
	code := apperr.CodeAgentFinalSyncFailed.ErrorCode
	var appError *apperr.AppError
	if errors.As(cause, &appError) {
		code = appError.ErrorCode
	}
	_ = orchestrator.repository.EndSession(context.Background(), sessionID, &code, orchestrator.clock.Now().UnixMilli())
}

// Shutdown 停止当前 provider session，并收敛本地 session 状态。
func (orchestrator *Orchestrator) Shutdown(ctx context.Context) error {
	orchestrator.mu.Lock()
	initCancel, initDone := orchestrator.initCancel, orchestrator.initDone
	orchestrator.mu.Unlock()
	if initCancel != nil {
		initCancel()
	}
	if initDone != nil {
		select {
		case <-initDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	orchestrator.mu.Lock()
	current := orchestrator.current
	orchestrator.current = nil
	orchestrator.mu.Unlock()
	if current == nil {
		return nil
	}
	if current.ID == "" {
		return nil
	}
	closeErr := orchestrator.provider.CloseSession(ctx, current.ID)
	now := orchestrator.clock.Now().UnixMilli()
	if closeErr != nil {
		code := apperr.CodeAgentInitializeFailed.ErrorCode
		_ = orchestrator.repository.EndSession(context.Background(), current.ID, &code, now)
		return closeErr
	}
	return orchestrator.repository.EndSession(ctx, current.ID, nil, now)
}

// initialize 创建本地 session、启动或恢复 thread，并运行非公开 initialize turn。
func (orchestrator *Orchestrator) initialize(ctx context.Context, meetingID string, previous *models.AgentSession) error {
	if orchestrator == nil || orchestrator.repository == nil || orchestrator.context == nil || orchestrator.provider == nil || orchestrator.rawRecord == nil || orchestrator.ids == nil || orchestrator.clock == nil || orchestrator.workspace == "" || meetingID == "" {
		return fmt.Errorf("智能体会话编排器未初始化")
	}
	initializeContext, cancelInitialize := context.WithCancel(ctx)
	initializeDone := make(chan struct{})
	if err := orchestrator.reserveOwner(cancelInitialize, initializeDone); err != nil {
		cancelInitialize()
		return err
	}
	ctx = initializeContext
	defer orchestrator.finishInitializeOwner(cancelInitialize, initializeDone)
	success := false
	defer func() {
		if !success {
			orchestrator.releaseOwner()
		}
	}()
	meeting, err := orchestrator.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return err
	}
	workingDirectory, err := trustedMeetingDirectory(orchestrator.workspace, meeting.RelativeDir)
	if err != nil {
		return err
	}
	now := orchestrator.clock.Now().UnixMilli()
	sessionModel := models.AgentSession{
		ID: orchestrator.ids.New(), MeetingID: meetingID, Provider: "codex",
		CWDRelativePath: meeting.RelativeDir, State: "starting", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if previous != nil {
		sessionModel.ResumedFromSessionID = &previous.ID
	}
	if err := orchestrator.repository.BeginInitialization(ctx, sessionModel); err != nil {
		return err
	}
	providerSession, err := orchestrator.openProviderSession(ctx, sessionModel.ID, workingDirectory, previous)
	if err != nil {
		return orchestrator.failInitialization(sessionModel.ID, err)
	}
	if err := orchestrator.repository.SetSessionThread(ctx, sessionModel.ID, providerSession.ProviderSessionID, orchestrator.clock.Now().UnixMilli()); err != nil {
		_ = orchestrator.provider.CloseSession(context.Background(), sessionModel.ID)
		return orchestrator.failInitialization(sessionModel.ID, err)
	}
	if err := orchestrator.rawRecord.Flush(ctx, meetingID); err != nil {
		_ = orchestrator.provider.CloseSession(context.Background(), sessionModel.ID)
		wrapped := apperr.Dependency(apperr.CodeAgentContextFlushFailed, err, apperr.WithOp("agent.initialize.flush"))
		return orchestrator.failInitialization(sessionModel.ID, wrapped)
	}
	if err := orchestrator.runInitializeTurn(ctx, meeting, sessionModel, previous); err != nil {
		_ = orchestrator.provider.CloseSession(context.Background(), sessionModel.ID)
		return orchestrator.failInitialization(sessionModel.ID, err)
	}
	orchestrator.mu.Lock()
	orchestrator.current = &providerSession
	orchestrator.mu.Unlock()
	success = true
	return nil
}

// openProviderSession 优先恢复 thread，找不到时明确回退为新 thread。
func (orchestrator *Orchestrator) openProviderSession(ctx context.Context, sessionID string, cwd string, previous *models.AgentSession) (port.AgentSession, error) {
	executable := ""
	if orchestrator.executable != nil {
		var err error
		executable, err = orchestrator.executable(ctx)
		if err != nil {
			return port.AgentSession{}, err
		}
	}
	if previous != nil && previous.ThreadID != nil && *previous.ThreadID != "" {
		resumed, err := orchestrator.provider.ResumeSession(ctx, port.ResumeAgentSessionRequest{
			SessionID: sessionID, ProviderSessionID: *previous.ThreadID, WorkingDirectory: cwd, ExecutablePath: executable,
		})
		if err == nil {
			return resumed, nil
		}
		if !hasErrorCode(err, apperr.CodeAgentThreadNotFound.ErrorCode) {
			return port.AgentSession{}, err
		}
	}
	return orchestrator.provider.StartSession(ctx, port.StartAgentSessionRequest{
		SessionID: sessionID, WorkingDirectory: cwd, Prompt: DeveloperInstructions, ExecutablePath: executable,
	})
}

// runInitializeTurn 从上次快照和当前 SQLite 范围构造非公开初始化结果。
func (orchestrator *Orchestrator) runInitializeTurn(ctx context.Context, meeting models.Meeting, session models.AgentSession, previous *models.AgentSession) error {
	snapshotJSON, throughSeq := orchestrator.previousSnapshot(ctx, previous)
	cutoff, err := orchestrator.repository.LatestEventSeq(ctx, meeting.ID)
	if err != nil {
		return err
	}
	request := BuildContextRequest{
		MeetingID: meeting.ID, SessionID: session.ID, MeetingNo: meeting.MeetingNo, Subject: meeting.Subject,
		ThroughSeq: throughSeq, CutoffSeq: cutoff, Purpose: "initialize", SnapshotJSON: snapshotJSON,
	}
	built, err := orchestrator.context.Build(ctx, request)
	if err != nil {
		return err
	}
	turnID := orchestrator.ids.New()
	now := orchestrator.clock.Now().UnixMilli()
	if err := orchestrator.repository.CreateTurn(ctx, models.AgentTurn{
		ID: turnID, MeetingID: meeting.ID, AgentSessionID: session.ID, Kind: "initialize", State: "pending",
		IdempotencyKey: "initialize:" + session.ID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return err
	}
	units, err := orchestrator.initializationUnits(ctx, meeting.ID, session.ID, built)
	if err != nil {
		return err
	}
	if len(units) == 0 {
		units = []persistedBatch{{Context: ContextBatch{FromSeq: throughSeq, ToSeq: cutoff, Prompt: built.FinalPrompt}}}
	}
	first := true
	for index, unit := range units {
		kind := port.AgentTurnIngest
		if index == len(units)-1 {
			kind = port.AgentTurnInitialize
		}
		request.SnapshotJSON = snapshotJSON
		unit.Context.Prompt = buildPrompt(request, unit.Context.Content, "")
		validated, providerTurnID, executeErr := orchestrator.executeInitializeUnit(ctx, session, turnID, unit, kind, built, first)
		if executeErr != nil {
			return executeErr
		}
		first = false
		snapshotJSON = validated.SnapshotJSON
		through := unit.Context.ToSeq
		if index+1 < len(units) {
			if err := orchestrator.repository.CommitIngest(ctx, turnID, providerTurnID, unit.ID,
				orchestrator.snapshotModel(session, turnID, through, validated), orchestrator.clock.Now().UnixMilli()); err != nil {
				return err
			}
			continue
		}
		if err := orchestrator.repository.CompleteInitialization(ctx, turnID, providerTurnID, unit.ID,
			orchestrator.snapshotModel(session, turnID, through, validated), orchestrator.clock.Now().UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

// initializationUnits 持久化初始化范围的确定性批次。
func (orchestrator *Orchestrator) initializationUnits(ctx context.Context, meetingID string, sessionID string, built ContextBuildResult) ([]persistedBatch, error) {
	units := make([]persistedBatch, 0, len(built.Batches))
	for _, batch := range built.Batches {
		id := orchestrator.ids.New()
		now := orchestrator.clock.Now().UnixMilli()
		if err := orchestrator.repository.CreateBatch(ctx, models.SyncBatch{
			ID: id, MeetingID: meetingID, AgentSessionID: sessionID, FromSeq: batch.FromSeq, ToSeq: batch.ToSeq,
			IdempotencyKey: batch.IdempotencyKey, State: "pending", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return nil, err
		}
		units = append(units, persistedBatch{ID: id, Context: batch})
	}
	return units, nil
}

// executeInitializeUnit 要求 snapshot final 和 completed 同时存在。
func (orchestrator *Orchestrator) executeInitializeUnit(ctx context.Context, session models.AgentSession, turnID string, unit persistedBatch, kind port.AgentTurnKind, built ContextBuildResult, first bool) (domainagent.ValidatedOutput, string, error) {
	if unit.ID != "" {
		if err := orchestrator.repository.MarkBatchRunning(ctx, unit.ID, orchestrator.clock.Now().UnixMilli()); err != nil {
			return domainagent.ValidatedOutput{}, "", err
		}
	}
	schema, err := buildAgentOutputSchema(kind, built)
	if err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	events, err := orchestrator.provider.RunTurn(ctx, port.RunAgentTurnRequest{
		SessionID: session.ID, TurnID: turnID, Kind: kind, Input: appendReferenceInstructions(unit.Context.Prompt, built),
		OutputSchema: schema, Deadline: deadlineFromContext(ctx),
	})
	if err != nil {
		return domainagent.ValidatedOutput{}, "", err
	}
	providerTurnID, final, completed := "", []byte(nil), false
	for event := range events {
		switch event.Type {
		case port.AgentEventTurnStarted:
			providerTurnID = event.ProviderTurnID
			if first {
				err = orchestrator.repository.MarkTurnRunning(ctx, turnID, providerTurnID, orchestrator.clock.Now().UnixMilli())
			} else {
				err = orchestrator.repository.SetRunningProviderTurn(ctx, turnID, providerTurnID, orchestrator.clock.Now().UnixMilli())
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
			return domainagent.ValidatedOutput{}, providerTurnID, apperr.Dependency(apperr.CodeAgentInitializeFailed, errors.New(event.FailureCode), apperr.WithOp("agent.initialize.turn"))
		}
	}
	if providerTurnID == "" || len(final) == 0 || !completed {
		return domainagent.ValidatedOutput{}, providerTurnID, apperr.Dependency(apperr.CodeAgentOutputInvalid, errors.New("initialize final missing"), apperr.WithOp("agent.initialize.output"))
	}
	validated, validateErr := domainagent.ValidateOutput(kind, final, referenceAllowlist(built))
	if validateErr != nil {
		return domainagent.ValidatedOutput{}, providerTurnID, apperr.Dependency(apperr.CodeAgentOutputInvalid, validateErr, apperr.WithOp("agent.initialize.output.validate"))
	}
	return validated, providerTurnID, nil
}

// previousSnapshot 读取旧 session 最后成功本地快照，失败时从零恢复。
func (orchestrator *Orchestrator) previousSnapshot(ctx context.Context, previous *models.AgentSession) ([]byte, int64) {
	if previous == nil {
		return nil, 0
	}
	snapshot, err := orchestrator.repository.GetSnapshot(ctx, previous.ID)
	if err != nil {
		return nil, 0
	}
	return []byte(snapshot.ContentJSON), snapshot.ThroughSeq
}

// snapshotModel 构造初始化成功的滚动快照。
func (orchestrator *Orchestrator) snapshotModel(session models.AgentSession, turnID string, throughSeq int64, output domainagent.ValidatedOutput) models.ContextSnapshot {
	now := orchestrator.clock.Now().UnixMilli()
	return models.ContextSnapshot{
		ID: orchestrator.ids.New(), MeetingID: session.MeetingID, AgentSessionID: session.ID, AgentTurnID: turnID,
		ThroughSeq: throughSeq, ContentJSON: string(output.SnapshotJSON), ContentSHA256: output.SnapshotSHA256,
		CreatedAt: now, UpdatedAt: now,
	}
}

// reserveOwner 防止同一进程并发初始化第二个 app-server。
func (orchestrator *Orchestrator) reserveOwner(cancel context.CancelFunc, done chan struct{}) error {
	orchestrator.mu.Lock()
	defer orchestrator.mu.Unlock()
	if orchestrator.current != nil {
		return apperr.Biz(apperr.CodeAgentBusy, apperr.WithOp("agent.orchestrator.owner"))
	}
	orchestrator.current = &port.AgentSession{}
	orchestrator.initCancel, orchestrator.initDone = cancel, done
	return nil
}

// beginRetry 原子取得重试所有权；初始化或其他重试进行中时不取消既有任务。
func (orchestrator *Orchestrator) beginRetry() error {
	orchestrator.mu.Lock()
	defer orchestrator.mu.Unlock()
	if orchestrator.retrying || orchestrator.initDone != nil {
		return apperr.Biz(apperr.CodeAgentBusy, apperr.WithOp("agent.orchestrator.retry"))
	}
	orchestrator.retrying = true
	return nil
}

// finishRetry 释放重试所有权，允许后续显式恢复。
func (orchestrator *Orchestrator) finishRetry() {
	orchestrator.mu.Lock()
	orchestrator.retrying = false
	orchestrator.mu.Unlock()
}

// finishInitializeOwner 发布初始化完成，并清理仅初始化阶段使用的取消句柄。
func (orchestrator *Orchestrator) finishInitializeOwner(cancel context.CancelFunc, done chan struct{}) {
	cancel()
	orchestrator.mu.Lock()
	if orchestrator.initDone == done {
		orchestrator.initCancel, orchestrator.initDone = nil, nil
	}
	close(done)
	orchestrator.mu.Unlock()
}

// releaseOwner 清理失败初始化留下的占位 owner。
func (orchestrator *Orchestrator) releaseOwner() {
	orchestrator.mu.Lock()
	orchestrator.current = nil
	orchestrator.mu.Unlock()
}

// failInitialization 记录稳定错误码；收敛失败时保留原始根因并附加清理错误。
func (orchestrator *Orchestrator) failInitialization(sessionID string, cause error) error {
	code := apperr.CodeAgentInitializeFailed.ErrorCode
	var appError *apperr.AppError
	if errors.As(cause, &appError) {
		code = appError.ErrorCode
	}
	if err := orchestrator.repository.FailInitialization(context.Background(), sessionID, code, orchestrator.clock.Now().UnixMilli()); err != nil {
		return errors.Join(cause, fmt.Errorf("收敛智能体初始化失败：%w", err))
	}
	return cause
}

// trustedMeetingDirectory 从可信 root 和数据库相对路径构造 cwd。
func trustedMeetingDirectory(root string, relative string) (string, error) {
	if !filepath.IsAbs(root) || relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("会议工作目录无效")
	}
	cleaned := filepath.Clean(relative)
	if cleaned == "." || cleaned == ".." || filepath.Dir(cleaned) == ".." {
		return "", fmt.Errorf("会议工作目录越界")
	}
	result := filepath.Join(root, cleaned)
	relativeToRoot, err := filepath.Rel(root, result)
	if err != nil || relativeToRoot == ".." || filepath.IsAbs(relativeToRoot) {
		return "", fmt.Errorf("会议工作目录越界")
	}
	return result, nil
}
