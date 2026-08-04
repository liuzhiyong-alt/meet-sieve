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

	"gorm.io/gorm"
)

// TestTurnService_CommitsQuestionAnswerSnapshotBatchAndCursorAtomically 验证完整成功事实链。
func TestTurnService_CommitsQuestionAnswerSnapshotBatchAndCursorAtomically(t *testing.T) {
	db := openAgentDatabase(t)
	prepareAvailableSession(t, db)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	provider := &turnProvider{}
	rawRecord := &turnRawRecord{}
	timelinePublishedAfterCommit := false
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	service := serviceagent.NewTurnService(serviceagent.TurnServiceDependencies{
		Repository: repository, Context: serviceagent.NewContextBuilder(repository), Provider: provider,
		RawRecord: rawRecord, Clock: clock.NewFixed(now), IDs: identity.NewFixedGenerator(
			"44444444-4444-4444-8444-444444444444", // turn
			"55555555-5555-4555-8555-555555555555", // question event
			"66666666-6666-4666-8666-666666666666", // batch
			"77777777-7777-4777-8777-777777777777", // answer event
			"88888888-8888-4888-8888-888888888888", // snapshot
		),
		Events: serviceagent.TurnEventSinkFunc(func(event port.AgentEvent) {
			if event.Type != port.AgentEventTimelineChanged {
				return
			}
			var count int64
			if err := db.Model(&models.MeetingEvent{}).Where("meeting_id = ? AND kind = ?", meetingID, "ai.answer").Count(&count).Error; err != nil {
				t.Fatalf("时间线通知时查询回答失败：%v", err)
			}
			timelinePublishedAfterCommit = count == 1
		}),
	})
	result, err := service.Ask(context.Background(), serviceagent.AskInput{
		MeetingID: meetingID, Question: "  请总结风险  ", Trigger: "manual", IdempotencyKey: "request-1",
	})
	if err != nil {
		t.Fatalf("执行智能体问答失败：%v", err)
	}
	if result.Answer != "测试回答" || result.QuestionSeq != 1 || result.AnswerSeq != 2 || rawRecord.flushes != 1 || rawRecord.dirties != 1 {
		t.Fatalf("问答结果或原始记录刷新错误：result=%#v raw=%#v", result, rawRecord)
	}
	if !timelinePublishedAfterCommit {
		t.Fatal("时间线变更通知必须在回答事务提交后发布")
	}
	var turn models.AgentTurn
	if err := db.Where("id = ?", result.TurnID).Take(&turn).Error; err != nil || turn.State != "completed" || turn.AnswerEventID == nil {
		t.Fatalf("turn 未原子完成：turn=%#v err=%v", turn, err)
	}
	var snapshot models.ContextSnapshot
	if err := db.Where("agent_session_id = ?", sessionID).Take(&snapshot).Error; err != nil || snapshot.ThroughSeq != result.QuestionSeq {
		t.Fatalf("滚动快照游标错误：snapshot=%#v err=%v", snapshot, err)
	}
	var payload string
	if err := db.Model(&models.MeetingEvent{}).Select("payload_json").Where("id = ?", *turn.AnswerEventID).Scan(&payload).Error; err != nil || payload != `{"content_format":"markdown","guest_visible":true,"text":"测试回答","v":2}` {
		t.Fatalf("公开回答 payload 错误：payload=%s err=%v", payload, err)
	}
}

// TestTurnService_FlushFailureKeepsQuestionButDoesNotCallProvider 验证 Flush 失败不启动 provider 且失败不提交 partial。
func TestTurnService_FlushFailureKeepsQuestionButDoesNotCallProvider(t *testing.T) {
	db := openAgentDatabase(t)
	prepareAvailableSession(t, db)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	provider := &turnProvider{}
	service := serviceagent.NewTurnService(serviceagent.TurnServiceDependencies{
		Repository: repository, Context: serviceagent.NewContextBuilder(repository), Provider: provider,
		RawRecord: &turnRawRecord{flushErr: errors.New("disk")}, Clock: clock.NewFixed(time.Now()),
		IDs: identity.NewFixedGenerator(
			"99999999-9999-4999-8999-999999999999", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
			"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		),
	})
	if _, err := service.Ask(context.Background(), serviceagent.AskInput{MeetingID: meetingID, Question: "问题", IdempotencyKey: "request-2"}); err == nil {
		t.Fatal("Flush 失败必须返回错误")
	}
	if provider.calls != 0 {
		t.Fatal("Flush 失败后不得调用 provider")
	}
	var kinds []string
	if err := db.Model(&models.MeetingEvent{}).Where("meeting_id = ?", meetingID).Order("seq").Pluck("kind", &kinds).Error; err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != "ai.question" || kinds[1] != "ai.failed" {
		t.Fatalf("失败事实不正确：%v", kinds)
	}
}

// prepareAvailableSession 把测试会议和 session 准备为可问答状态。
func prepareAvailableSession(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Model(&models.Meeting{}).Where("id = ?", meetingID).Update("agent_state", "available").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AgentSession{
		ID: sessionID, MeetingID: meetingID, Provider: "codex", ThreadID: agentStringPointer("provider-thread"),
		CWDRelativePath: "meetings/test", State: "available", StartedAt: 0, CreatedAt: 0, UpdatedAt: 0,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

// agentStringPointer 返回测试模型所需字符串指针。
func agentStringPointer(value string) *string { return &value }

type turnProvider struct{ calls int }

// RunTurn 返回满足 final + completed 双条件的结构化结果。
func (provider *turnProvider) RunTurn(_ context.Context, request port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	provider.calls++
	events := make(chan port.AgentEvent, 3)
	events <- port.AgentEvent{Type: port.AgentEventTurnStarted, SessionID: request.SessionID, TurnID: request.TurnID, ProviderTurnID: "provider-turn"}
	output := []byte(`{"answer":"测试回答","snapshot":{"current_topics":[],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":[]}}`)
	events <- port.AgentEvent{Type: port.AgentEventFinalOutput, SessionID: request.SessionID, TurnID: request.TurnID, ProviderTurnID: "provider-turn", FinalOutput: &port.AgentFinalOutput{JSON: output}}
	events <- port.AgentEvent{Type: port.AgentEventCompleted, SessionID: request.SessionID, TurnID: request.TurnID, ProviderTurnID: "provider-turn"}
	close(events)
	return events, nil
}

func (provider *turnProvider) CheckAvailability(context.Context, port.AgentAvailabilityRequest) (port.AgentAvailability, error) {
	return port.AgentAvailability{}, nil
}
func (provider *turnProvider) StartSession(context.Context, port.StartAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, nil
}
func (provider *turnProvider) ResumeSession(context.Context, port.ResumeAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, nil
}
func (provider *turnProvider) RespondApproval(context.Context, port.RespondAgentApprovalRequest) error {
	return nil
}
func (provider *turnProvider) InterruptTurn(context.Context, port.InterruptAgentTurnRequest) error {
	return nil
}
func (provider *turnProvider) CloseSession(context.Context, string) error { return nil }

type turnRawRecord struct {
	flushes  int
	dirties  int
	flushErr error
}

// Flush 记录 provider 前的强制刷新。
func (record *turnRawRecord) Flush(context.Context, string) error {
	record.flushes++
	return record.flushErr
}

// MarkDirty 记录成功提交后的投影刷新。
func (record *turnRawRecord) MarkDirty(string) error {
	record.dirties++
	return nil
}
