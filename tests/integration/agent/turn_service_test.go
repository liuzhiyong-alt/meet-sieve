package agent_test

import (
	"context"
	"errors"
	"fmt"
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

// TestTurnService_VoiceQuestionConsumesAllCommandUtterances 验证语音问题与多条候选 final 在同一事务绑定。
func TestTurnService_VoiceQuestionConsumesAllCommandUtterances(t *testing.T) {
	db := openAgentDatabase(t)
	prepareAvailableSession(t, db)
	prepareVoiceCommandCandidates(t, db)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	service := serviceagent.NewTurnService(serviceagent.TurnServiceDependencies{
		Repository: repository, Context: serviceagent.NewContextBuilder(repository), Provider: &turnProvider{},
		RawRecord: &turnRawRecord{}, Clock: clock.NewFixed(time.UnixMilli(2_000)), IDs: identity.NewFixedGenerator(
			"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			"cccccccc-cccc-4ccc-8ccc-cccccccccccc", "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
			"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
		),
	})
	media := &turnMediaLifecycle{}
	if err := service.SetVoiceTurnMediaLifecycle(media); err != nil {
		t.Fatal(err)
	}
	result, err := service.Ask(context.Background(), serviceagent.AskInput{
		MeetingID: meetingID, Question: "上面都说什么了？", Trigger: "wake_word",
		TriggerUtteranceID:  agentStringPointer("77777777-7777-4777-8777-777777777777"),
		TriggerUtteranceIDs: []string{"77777777-7777-4777-8777-777777777777", "88888888-8888-4888-8888-888888888888"},
		VoiceCommandID:      "command-1", IdempotencyKey: "wake:command-1",
	})
	if err != nil {
		t.Fatalf("执行语音问题失败：%v", err)
	}
	var relations []models.AgentVoiceCommandUtterance
	if err = db.Where("command_id = ?", "command-1").Order("position ASC").Find(&relations).Error; err != nil {
		t.Fatal(err)
	}
	if len(relations) != 2 || relations[0].State != "consumed" || relations[1].State != "consumed" || relations[0].AgentTurnID == nil || *relations[0].AgentTurnID != result.TurnID {
		t.Fatalf("语音指令关系未完整消费：%+v", relations)
	}
	if media.pauses != 1 || media.resumes != 1 || media.turnID != result.TurnID {
		t.Fatalf("语音 turn 媒体生命周期错误：%+v", media)
	}
}

// TestTurnService_VoicePauseFailureSkipsProvider 验证媒体边界未确认时不会把语音问题发送给 Codex。
func TestTurnService_VoicePauseFailureSkipsProvider(t *testing.T) {
	db := openAgentDatabase(t)
	prepareAvailableSession(t, db)
	prepareVoiceCommandCandidates(t, db)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	provider := &turnProvider{}
	service := serviceagent.NewTurnService(serviceagent.TurnServiceDependencies{
		Repository: repository, Context: serviceagent.NewContextBuilder(repository), Provider: provider,
		RawRecord: &turnRawRecord{}, Clock: clock.NewFixed(time.UnixMilli(2_000)), IDs: identity.NewFixedGenerator(
			"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			"cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		),
	})
	media := &turnMediaLifecycle{pauseErr: errors.New("pause failed")}
	if err := service.SetVoiceTurnMediaLifecycle(media); err != nil {
		t.Fatal(err)
	}
	_, err := service.Ask(context.Background(), serviceagent.AskInput{
		MeetingID: meetingID, Question: "上面都说什么了？", Trigger: "wake_word",
		TriggerUtteranceID:  agentStringPointer("77777777-7777-4777-8777-777777777777"),
		TriggerUtteranceIDs: []string{"77777777-7777-4777-8777-777777777777", "88888888-8888-4888-8888-888888888888"},
		VoiceCommandID:      "command-1", IdempotencyKey: "wake:command-1",
	})
	if err == nil || provider.calls != 0 || media.pauses != 1 || media.resumes != 0 {
		t.Fatalf("暂停失败仍执行了 AI 或恢复流程：err=%v provider=%d media=%+v", err, provider.calls, media)
	}
}

// prepareVoiceCommandCandidates 写入两条已由 ASR 持久化的候选指令 final。
func prepareVoiceCommandCandidates(t *testing.T, db *gorm.DB) {
	t.Helper()
	rows := []models.AgentVoiceCommandUtterance{
		{ID: "99999999-9999-4999-8999-999999999991", MeetingID: meetingID, CommandID: "command-1", UtteranceID: "77777777-7777-4777-8777-777777777777", Position: 0, State: "candidate", CreatedAt: 1, UpdatedAt: 1},
		{ID: "99999999-9999-4999-8999-999999999992", MeetingID: meetingID, CommandID: "command-1", UtteranceID: "88888888-8888-4888-8888-888888888888", Position: 1, State: "candidate", CreatedAt: 1, UpdatedAt: 1},
	}
	if err := db.Create(&models.ASRSession{ID: "66666666-6666-4666-8666-666666666666", MeetingID: meetingID, Provider: "volcano", State: "stopped", StartedAt: 1, TransportMode: "seed_v1", InputStartSample: 0, LastSentSample: 32_000, LastFinalSample: 32_000, CreatedAt: 1, UpdatedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
	for index, utteranceID := range []string{"77777777-7777-4777-8777-777777777777", "88888888-8888-4888-8888-888888888888"} {
		eventID := []string{"33333333-3333-4333-8333-333333333331", "33333333-3333-4333-8333-333333333332"}[index]
		if err := db.Create(&models.MeetingEvent{ID: eventID, MeetingID: meetingID, Seq: int64(index + 1), Kind: "utterance.final", OccurredAt: int64(index), Source: "asr", EntityType: agentStringPointer("utterance"), EntityID: &utteranceID, CreatedAt: 1, UpdatedAt: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.Utterance{ID: utteranceID, MeetingID: meetingID, EventID: eventID, ASRSessionID: "66666666-6666-4666-8666-666666666666", ProviderResultID: fmt.Sprintf("result-%d", index), OriginalText: "指令", CurrentText: "指令", StartSample: int64(index * 16_000), EndSample: int64((index + 1) * 16_000), SpeakerAssignmentSource: "unassigned", TextRevision: 1, SpeakerRevision: 1, CreatedAt: 1, UpdatedAt: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
}

type turnMediaLifecycle struct {
	pauses   int
	resumes  int
	turnID   string
	pauseErr error
}

func (lifecycle *turnMediaLifecycle) PauseForTurn(_ context.Context, _ string, turnID string) error {
	lifecycle.pauses++
	lifecycle.turnID = turnID
	return lifecycle.pauseErr
}

func (lifecycle *turnMediaLifecycle) ResumeAfterTurn(_ context.Context, _ string, turnID string) error {
	lifecycle.resumes++
	lifecycle.turnID = turnID
	return nil
}

func (lifecycle *turnMediaLifecycle) FinalizePausedTurn(context.Context, string, string) error {
	return nil
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
