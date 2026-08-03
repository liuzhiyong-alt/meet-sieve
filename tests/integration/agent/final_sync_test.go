package agent_test

import (
	"context"
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

// TestFinalSyncService_IngestsWithoutAnswerAndClosesProvider 验证结束同步不污染公开事件流。
func TestFinalSyncService_IngestsWithoutAnswerAndClosesProvider(t *testing.T) {
	db := openAgentDatabase(t)
	prepareAvailableSession(t, db)
	if err := db.Model(&models.Meeting{}).Where("id = ?", meetingID).Updates(map[string]any{"lifecycle_state": "ended", "local_save_state": "saved", "gap_state": "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	initializeTurn := models.AgentTurn{ID: "abababab-abab-4bab-8bab-abababababab", MeetingID: meetingID, AgentSessionID: sessionID, Kind: "initialize", State: "completed", IdempotencyKey: "initialize", StartedAt: int64Pointer(0), EndedAt: int64Pointer(0), CreatedAt: 0, UpdatedAt: 0}
	if err := db.Create(&initializeTurn).Error; err != nil {
		t.Fatal(err)
	}
	entityType, entityID := "asr_gap", "acacacac-acac-4cac-8cac-acacacacacac"
	event := models.MeetingEvent{ID: "adadadad-adad-4dad-8dad-adadadadadad", MeetingID: meetingID, Seq: 1, Kind: "asr.gap", OccurredAt: 1, Source: "system", EntityType: &entityType, EntityID: &entityID, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	gap := models.ASRGap{ID: entityID, MeetingID: meetingID, EventID: event.ID, StartSample: 0, EndSample: 16000, Reason: "record_only", OriginKey: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", State: "completed", AttemptCount: 1, CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&gap).Error; err != nil {
		t.Fatal(err)
	}
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	provider := &finalProvider{}
	owner := &finalSessionOwner{session: models.AgentSession{ID: sessionID, MeetingID: meetingID, State: "available"}}
	service := serviceagent.NewFinalSyncService(serviceagent.FinalSyncDependencies{
		Repository: repository, Context: serviceagent.NewContextBuilder(repository), Provider: provider,
		RawRecord: &turnRawRecord{}, Sessions: owner, Clock: clock.NewFixed(time.UnixMilli(10)),
		IDs: identity.NewFixedGenerator(
			"aeaeaeae-aeae-4eae-8eae-aeaeaeaeaeae", // turn
			"afafafaf-afaf-4faf-8faf-afafafafafaf", // batch
			"b0b0b0b0-b0b0-40b0-80b0-b0b0b0b0b0b0", // snapshot
		),
	})
	if err := service.SyncFinal(context.Background(), meetingID); err != nil {
		t.Fatalf("结束同步失败：%v", err)
	}
	var answerCount int64
	if err := db.Model(&models.MeetingEvent{}).Where("meeting_id = ? AND kind = 'ai.answer'", meetingID).Count(&answerCount).Error; err != nil || answerCount != 0 {
		t.Fatalf("结束同步不得创建回答：count=%d err=%v", answerCount, err)
	}
	var meeting models.Meeting
	if err := db.Where("id = ?", meetingID).Take(&meeting).Error; err != nil || meeting.AgentState != "unavailable" || owner.shutdowns != 1 {
		t.Fatalf("结束同步状态或关闭次数错误：meeting=%#v shutdowns=%d err=%v", meeting, owner.shutdowns, err)
	}
}

type finalSessionOwner struct {
	session   models.AgentSession
	shutdowns int
}

// EnsurePostMeeting 返回测试中的活动 session。
func (owner *finalSessionOwner) EnsurePostMeeting(context.Context, string) (models.AgentSession, error) {
	return owner.session, nil
}

// Shutdown 记录 provider owner 被关闭。
func (owner *finalSessionOwner) Shutdown(context.Context) error {
	owner.shutdowns++
	return nil
}

// int64Pointer 返回测试模型使用的时间指针。
func int64Pointer(value int64) *int64 { return &value }

var _ port.AgentProvider = (*turnProvider)(nil)

type finalProvider struct{ turnProvider }

// RunTurn 返回 ingest 所需的纯 snapshot 输出。
func (provider *finalProvider) RunTurn(_ context.Context, request port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	provider.calls++
	events := make(chan port.AgentEvent, 3)
	events <- port.AgentEvent{Type: port.AgentEventTurnStarted, ProviderTurnID: "final-provider-turn"}
	events <- port.AgentEvent{Type: port.AgentEventFinalOutput, FinalOutput: &port.AgentFinalOutput{JSON: []byte(`{"snapshot":{"current_topics":[],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":[]}}`)}}
	events <- port.AgentEvent{Type: port.AgentEventCompleted}
	close(events)
	return events, nil
}
