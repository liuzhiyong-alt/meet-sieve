package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
	serviceagent "meet-sieve/internal/service/agent"
	"meet-sieve/models"
)

// TestOrchestrator_InitializesWithoutBlockingMeetingAxes 验证 session/thread/initialize 快照成功后才 available。
func TestOrchestrator_InitializesWithoutBlockingMeetingAxes(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	provider := &initializeProvider{}
	rawRecord := &turnRawRecord{}
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	orchestrator := serviceagent.NewOrchestrator(serviceagent.OrchestratorDependencies{
		Repository: repository, Context: serviceagent.NewContextBuilder(repository), Provider: provider,
		RawRecord: rawRecord, Clock: clock.NewFixed(now), WorkspaceRoot: t.TempDir(),
		IDs: identity.NewFixedGenerator(
			"12121212-1212-4121-8121-121212121212", // session
			"13131313-1313-4131-8131-131313131313", // initialize turn
			"14141414-1414-4141-8141-141414141414", // snapshot
		),
	})
	if err := orchestrator.Initialize(context.Background(), meetingID); err != nil {
		t.Fatalf("初始化会议智能体失败：%v", err)
	}
	var meeting models.Meeting
	if err := db.Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	if meeting.AgentState != "available" || meeting.LifecycleState != "recording" || meeting.LocalSaveState != "saving" {
		t.Fatalf("初始化错误改变会议独立状态轴：%#v", meeting)
	}
	var session models.AgentSession
	if err := db.Where("meeting_id = ?", meetingID).Take(&session).Error; err != nil || session.State != "available" || session.ThreadID == nil || *session.ThreadID != "provider-thread" {
		t.Fatalf("session 未完成：session=%#v err=%v", session, err)
	}
	if rawRecord.flushes != 1 || provider.starts != 1 || provider.turns != 1 {
		t.Fatalf("初始化调用次数错误：raw=%#v provider=%#v", rawRecord, provider)
	}
	if err := orchestrator.Shutdown(context.Background()); err != nil || provider.closes != 1 {
		t.Fatalf("shutdown 未关闭 provider：err=%v closes=%d", err, provider.closes)
	}
}

// TestOrchestrator_ShutdownCancelsInitialization 验证会议结束撞上初始化时不会遗留 owner 或子进程。
func TestOrchestrator_ShutdownCancelsInitialization(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	provider := &blockingInitializeProvider{started: make(chan struct{})}
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	orchestrator := serviceagent.NewOrchestrator(serviceagent.OrchestratorDependencies{
		Repository: repository, Context: serviceagent.NewContextBuilder(repository), Provider: provider,
		RawRecord: &turnRawRecord{}, Clock: clock.NewFixed(now), WorkspaceRoot: t.TempDir(),
		IDs: identity.NewFixedGenerator("15151515-1515-4151-8151-151515151515"),
	})
	initializeDone := make(chan error, 1)
	go func() { initializeDone <- orchestrator.Initialize(context.Background(), meetingID) }()
	<-provider.started
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := orchestrator.Shutdown(shutdownContext); err != nil {
		t.Fatalf("停止初始化失败：%v", err)
	}
	if err := <-initializeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("初始化应收到取消：%v", err)
	}
	if provider.closes != 0 {
		t.Fatalf("尚未创建的 provider session 不应重复关闭：%d", provider.closes)
	}
}

type initializeProvider struct {
	starts int
	turns  int
	closes int
}

type blockingInitializeProvider struct {
	started chan struct{}
	closes  int
}

func (provider *blockingInitializeProvider) CheckAvailability(context.Context, port.AgentAvailabilityRequest) (port.AgentAvailability, error) {
	return port.AgentAvailability{}, nil
}
func (provider *blockingInitializeProvider) StartSession(ctx context.Context, _ port.StartAgentSessionRequest) (port.AgentSession, error) {
	close(provider.started)
	<-ctx.Done()
	return port.AgentSession{}, ctx.Err()
}
func (provider *blockingInitializeProvider) ResumeSession(context.Context, port.ResumeAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, errors.New("不应恢复")
}
func (provider *blockingInitializeProvider) RunTurn(context.Context, port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	return nil, errors.New("不应运行 turn")
}
func (provider *blockingInitializeProvider) RespondApproval(context.Context, port.RespondAgentApprovalRequest) error {
	return nil
}
func (provider *blockingInitializeProvider) InterruptTurn(context.Context, port.InterruptAgentTurnRequest) error {
	return nil
}
func (provider *blockingInitializeProvider) CloseSession(context.Context, string) error {
	provider.closes++
	return nil
}

func (provider *initializeProvider) CheckAvailability(context.Context, port.AgentAvailabilityRequest) (port.AgentAvailability, error) {
	return port.AgentAvailability{State: port.AgentAvailabilityAvailable}, nil
}

// StartSession 返回与本地 session 对齐的 provider thread。
func (provider *initializeProvider) StartSession(_ context.Context, request port.StartAgentSessionRequest) (port.AgentSession, error) {
	provider.starts++
	return port.AgentSession{ID: request.SessionID, ProviderSessionID: "provider-thread"}, nil
}

func (provider *initializeProvider) ResumeSession(context.Context, port.ResumeAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, nil
}

// RunTurn 返回 initialize 所需的 snapshot-only 输出。
func (provider *initializeProvider) RunTurn(_ context.Context, request port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	provider.turns++
	events := make(chan port.AgentEvent, 3)
	events <- port.AgentEvent{Type: port.AgentEventTurnStarted, SessionID: request.SessionID, TurnID: request.TurnID, ProviderTurnID: "provider-initialize-turn"}
	output := []byte(`{"snapshot":{"current_topics":[],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":[]}}`)
	events <- port.AgentEvent{Type: port.AgentEventFinalOutput, FinalOutput: &port.AgentFinalOutput{JSON: output}, ProviderTurnID: "provider-initialize-turn"}
	events <- port.AgentEvent{Type: port.AgentEventCompleted, ProviderTurnID: "provider-initialize-turn"}
	close(events)
	return events, nil
}

func (provider *initializeProvider) RespondApproval(context.Context, port.RespondAgentApprovalRequest) error {
	return nil
}
func (provider *initializeProvider) InterruptTurn(context.Context, port.InterruptAgentTurnRequest) error {
	return nil
}
func (provider *initializeProvider) CloseSession(context.Context, string) error {
	provider.closes++
	return nil
}
