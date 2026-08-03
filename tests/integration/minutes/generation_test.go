package minutes_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
	minutesrepository "meet-sieve/internal/repository/minutes"
	minutesservice "meet-sieve/internal/service/minutes"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestGenerationService_CommitsOnlyValidatedOutput 验证合法 JSON 才创建版本并投影文件。
func TestGenerationService_CommitsOnlyValidatedOutput(t *testing.T) {
	db, repository := openMinutesDatabase(t)
	prepareMinuteFact(t, db)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "meetings", "m"), 0o700); err != nil {
		t.Fatal(err)
	}
	projector := minutesservice.NewMinuteProjector(repository, root)
	service := minutesservice.NewGenerationService(minutesservice.GenerationDependencies{
		Repository: repository, AgentRepository: agentrepository.NewRepository(db, database.NewTransactionManager(db)), Facts: repository,
		Provider: &minutesProvider{}, RawRecord: minutesFlusher{}, Projector: projector,
		IDs: identity.NewFixedGenerator("41414141-4141-4141-8141-414141414141", "42424242-4242-4242-8242-424242424242"), Clock: clock.NewFixed(time.UnixMilli(100)),
	})
	result, err := service.Generate(context.Background(), minutesservice.GenerateInput{MeetingID: meetingID, RequestID: "request-minutes"})
	if err != nil || result.Version.Source != "ai" || !result.Version.IsCurrent {
		t.Fatalf("生成纪要失败：result=%#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "meetings", "m", "会议纪要草稿.md")); err != nil {
		t.Fatalf("纪要投影不存在：%v", err)
	}
	var aiEvents int64
	if err := db.Model(&models.MeetingEvent{}).Where("meeting_id = ? AND kind LIKE 'ai.%'", meetingID).Count(&aiEvents).Error; err != nil || aiEvents != 0 {
		t.Fatalf("纪要生成不得污染统一事实流：count=%d err=%v", aiEvents, err)
	}
}

// TestGenerationService_InvalidOutputCreatesNoVersion 验证非法最终输出保留旧状态且不创建版本。
func TestGenerationService_InvalidOutputCreatesNoVersion(t *testing.T) {
	db, repository := openMinutesDatabase(t)
	prepareMinuteFact(t, db)
	service := newTestGenerationService(t, db, repository, &invalidMinutesProvider{}, time.Minute)
	_, err := service.Generate(context.Background(), minutesservice.GenerateInput{MeetingID: meetingID, RequestID: "invalid-output"})
	if err == nil {
		t.Fatal("非法纪要输出必须失败")
	}
	var count int64
	if err := db.Model(&models.MinuteVersion{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("非法输出不得创建版本：count=%d err=%v", count, err)
	}
}

// TestGenerationService_TimeoutAndStopCreateNoVersion 验证超时与主持人停止都收敛 turn 且不创建版本。
func TestGenerationService_TimeoutAndStopCreateNoVersion(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		db, repository := openMinutesDatabase(t)
		prepareMinuteFact(t, db)
		service := newTestGenerationService(t, db, repository, &blockingMinutesProvider{}, 10*time.Millisecond)
		_, err := service.Generate(context.Background(), minutesservice.GenerateInput{MeetingID: meetingID, RequestID: "timeout"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("应返回 deadline：%v", err)
		}
		assertNoMinutesVersion(t, db)
		assertLatestMinutesTurnState(t, db, "timed_out")
	})
	t.Run("stop", func(t *testing.T) {
		db, repository := openMinutesDatabase(t)
		prepareMinuteFact(t, db)
		provider := &blockingMinutesProvider{started: make(chan struct{})}
		service := newTestGenerationService(t, db, repository, provider, time.Minute)
		done := make(chan error, 1)
		go func() {
			_, err := service.Generate(context.Background(), minutesservice.GenerateInput{MeetingID: meetingID, RequestID: "stop"})
			done <- err
		}()
		<-provider.started
		state := service.State()
		if err := service.Stop(context.Background(), meetingID, state.TurnID); err != nil {
			t.Fatalf("停止纪要生成失败：%v", err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("停止后应返回 cancelled：%v", err)
		}
		assertNoMinutesVersion(t, db)
		assertLatestMinutesTurnState(t, db, "cancelled")
	})
}

// newTestGenerationService 创建带真实仓储和隔离文件投影的生成服务。
func newTestGenerationService(t *testing.T, db *gorm.DB, repository *minutesrepository.Repository, provider port.AgentProvider, timeout time.Duration) *minutesservice.GenerationService {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "meetings", "m"), 0o700); err != nil {
		t.Fatal(err)
	}
	return minutesservice.NewGenerationService(minutesservice.GenerationDependencies{
		Repository: repository, AgentRepository: agentrepository.NewRepository(db, database.NewTransactionManager(db)), Facts: repository,
		Provider: provider, RawRecord: minutesFlusher{}, Projector: minutesservice.NewMinuteProjector(repository, root),
		IDs: identity.NewUUIDGenerator(), Clock: clock.NewFixed(time.UnixMilli(100)), Timeout: timeout,
	})
}

// assertNoMinutesVersion 验证失败路径没有派生版本。
func assertNoMinutesVersion(t *testing.T, db *gorm.DB) {
	t.Helper()
	var count int64
	if err := db.Model(&models.MinuteVersion{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("失败路径不得创建版本：count=%d err=%v", count, err)
	}
}

// assertLatestMinutesTurnState 验证最近生成 turn 的持久终态。
func assertLatestMinutesTurnState(t *testing.T, db *gorm.DB, expected string) {
	t.Helper()
	var turn models.AgentTurn
	if err := db.Where("meeting_id = ? AND kind = 'minutes'", meetingID).Order("created_at DESC").Take(&turn).Error; err != nil || turn.State != expected {
		t.Fatalf("纪要 turn 终态错误：turn=%#v err=%v", turn, err)
	}
}

// prepareMinuteFact 写入一条可引用的主持人消息。
func prepareMinuteFact(t *testing.T, db *gorm.DB) {
	t.Helper()
	event := models.MeetingEvent{ID: "43434343-4343-4343-8343-434343434343", MeetingID: meetingID, Seq: 1, Kind: "message.created", OccurredAt: 1, Source: "host", CreatedAt: 1, UpdatedAt: 1}
	entityType, entityID := "message", "44444444-4444-4444-8444-444444444444"
	event.EntityType, event.EntityID = &entityType, &entityID
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	message := models.Message{ID: entityID, MeetingID: meetingID, EventID: event.ID, AuthorKind: "host", DisplayNameSnapshot: "主持人", Content: "确认下周一发布", CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
}

type minutesProvider struct{}

// RunTurn 返回引用真实 seq 的结构化纪要。
func (provider *minutesProvider) RunTurn(_ context.Context, request port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	events := make(chan port.AgentEvent, 3)
	events <- port.AgentEvent{Type: port.AgentEventTurnStarted, ProviderTurnID: "minutes-provider-turn"}
	events <- port.AgentEvent{Type: port.AgentEventFinalOutput, FinalOutput: &port.AgentFinalOutput{JSON: []byte(`{"v":1,"conclusions":[{"text":"确认发布","source_seq":[1]}],"topics":[],"tasks":[],"references":[],"gap_notice":[]}`)}}
	events <- port.AgentEvent{Type: port.AgentEventCompleted}
	close(events)
	return events, nil
}

func (*minutesProvider) CheckAvailability(context.Context, port.AgentAvailabilityRequest) (port.AgentAvailability, error) {
	return port.AgentAvailability{}, nil
}
func (*minutesProvider) StartSession(context.Context, port.StartAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, nil
}
func (*minutesProvider) ResumeSession(context.Context, port.ResumeAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, nil
}
func (*minutesProvider) RespondApproval(context.Context, port.RespondAgentApprovalRequest) error {
	return nil
}
func (*minutesProvider) InterruptTurn(context.Context, port.InterruptAgentTurnRequest) error {
	return nil
}
func (*minutesProvider) CloseSession(context.Context, string) error { return nil }

type invalidMinutesProvider struct{ minutesProvider }

// RunTurn 返回 schema 不允许的未知字段。
func (*invalidMinutesProvider) RunTurn(context.Context, port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	events := make(chan port.AgentEvent, 3)
	events <- port.AgentEvent{Type: port.AgentEventTurnStarted, ProviderTurnID: "invalid-turn"}
	events <- port.AgentEvent{Type: port.AgentEventFinalOutput, FinalOutput: &port.AgentFinalOutput{JSON: []byte(`{"v":1,"unknown":true}`)}}
	events <- port.AgentEvent{Type: port.AgentEventCompleted}
	close(events)
	return events, nil
}

type blockingMinutesProvider struct {
	minutesProvider
	started chan struct{}
}

// RunTurn 发布 started 后等待取消或超时，并关闭事件流。
func (provider *blockingMinutesProvider) RunTurn(ctx context.Context, _ port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	events := make(chan port.AgentEvent, 1)
	events <- port.AgentEvent{Type: port.AgentEventTurnStarted, ProviderTurnID: "blocking-turn"}
	if provider.started != nil {
		close(provider.started)
	}
	go func() {
		<-ctx.Done()
		close(events)
	}()
	return events, nil
}

type minutesFlusher struct{}

// Flush 模拟生成前原始记录已持久化。
func (minutesFlusher) Flush(context.Context, string) error { return nil }
