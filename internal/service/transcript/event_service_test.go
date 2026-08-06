package transcript

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestEventService_CommandCandidateIsHiddenUntilReleased 验证指令用途与 final 原子提交且所有转写投影排除候选。
func TestEventService_CommandCandidateIsHiddenUntilReleased(t *testing.T) {
	service, db := newEventServiceForTest(t,
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555",
	)
	projectionInvalidated := false
	classifier := &candidateClassifier{projectionInvalidated: &projectionInvalidated}
	service.classifier = classifier
	service.onPersisted = func(string, PersistedEvent) { projectionInvalidated = true }
	created, err := service.PersistFinal(context.Background(), FinalInput{
		MeetingID: testMeetingID, ASRSessionID: testSessionID, ProviderResultID: "voice-command",
		Text: "哈喽，会议助手，上面都说什么了？", Range: mustSampleRange(t, 0, 16000), LastSentSample: 64000,
	})
	if err != nil {
		t.Fatalf("持久化语音指令 final 失败：%v", err)
	}
	if classifier.committed != created.EntityID || classifier.rolledBack != "" || !classifier.commitAfterNotification {
		t.Fatalf("分类事务结果错误：%+v", classifier)
	}
	var relation models.AgentVoiceCommandUtterance
	if err = db.Where("utterance_id = ?", created.EntityID).Take(&relation).Error; err != nil || relation.State != "candidate" {
		t.Fatalf("候选关系未与 final 原子提交：relation=%+v err=%v", relation, err)
	}
	repository := transcriptrepository.NewRepository(db)
	entries, err := NewTimelineService(repository).List(context.Background(), testMeetingID, 0, 100)
	if err != nil || len(entries) != 0 {
		t.Fatalf("候选指令不得进入转写时间线：entries=%+v err=%v", entries, err)
	}
	rows, err := repository.LoadRawRecordRows(context.Background(), testMeetingID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("候选指令不得进入原始记录：rows=%+v err=%v", rows, err)
	}
	if err = db.Model(&models.AgentVoiceCommandUtterance{}).Where("id = ?", relation.ID).Update("state", "released").Error; err != nil {
		t.Fatal(err)
	}
	entries, err = NewTimelineService(repository).List(context.Background(), testMeetingID, 0, 100)
	if err != nil || len(entries) != 1 || entries[0].Text != "哈喽，会议助手，上面都说什么了？" {
		t.Fatalf("释放后必须恢复普通发言：entries=%+v err=%v", entries, err)
	}
}

type candidateClassifier struct {
	committed               string
	rolledBack              string
	projectionInvalidated   *bool
	commitAfterNotification bool
}

func (classifier *candidateClassifier) PrepareFinal(_ context.Context, candidate port.TranscriptFinalCandidate) port.TranscriptFinalClassification {
	return port.TranscriptFinalClassification{Token: candidate.UtteranceID, CommandID: candidate.UtteranceID, Position: 0, Candidate: true}
}

func (classifier *candidateClassifier) CommitFinal(token string) {
	classifier.committed = token
	classifier.commitAfterNotification = classifier.projectionInvalidated != nil && *classifier.projectionInvalidated
}
func (classifier *candidateClassifier) RollbackFinal(token string) { classifier.rolledBack = token }

const (
	testMeetingID = "11111111-1111-4111-8111-111111111111"
	testSessionID = "22222222-2222-4222-8222-222222222222"
)

// TestEventService_PersistFinalAndGap 验证 final/gap 均以单一有序事件写入，并可安全重放。
func TestEventService_PersistFinalAndGap(t *testing.T) {
	service, db := newEventServiceForTest(t,
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666",
	)
	notified := 0
	service.onPersisted = func(meetingID string, event PersistedEvent) {
		if meetingID != testMeetingID || event.Duplicate {
			t.Fatalf("投影通知内容错误：meeting=%s event=%+v", meetingID, event)
		}
		notified++
	}
	final := FinalInput{MeetingID: testMeetingID, ASRSessionID: testSessionID, ProviderResultID: "provider-final-1", Text: "第一句", Range: mustSampleRange(t, 0, 16000), LastSentSample: 64000}

	created, err := service.PersistFinal(context.Background(), final)
	if err != nil {
		t.Fatalf("写入 final 失败：%v", err)
	}
	if created.Seq != 1 || created.Kind != "utterance.final" || created.EntityID == "" || created.Duplicate {
		t.Fatalf("final 事件结果错误：%+v", created)
	}
	replayed, err := service.PersistFinal(context.Background(), final)
	if err != nil {
		t.Fatalf("重放 final 不应失败：%v", err)
	}
	if !replayed.Duplicate || replayed.EventID != created.EventID || replayed.EntityID != created.EntityID || replayed.Seq != created.Seq {
		t.Fatalf("final 幂等结果错误：首次=%+v 重放=%+v", created, replayed)
	}

	gap := GapInput{MeetingID: testMeetingID, ASRSessionID: pointer(testSessionID), Range: mustSampleRange(t, 16000, 32000), Reason: transcriptdomain.GapDisconnected}
	gapCreated, err := service.PersistGap(context.Background(), gap)
	if err != nil {
		t.Fatalf("写入 gap 失败：%v", err)
	}
	if gapCreated.Seq != 2 || gapCreated.Kind != "asr.gap" || gapCreated.EntityID == "" || gapCreated.Duplicate {
		t.Fatalf("gap 事件结果错误：%+v", gapCreated)
	}
	gapReplayed, err := service.PersistGap(context.Background(), gap)
	if err != nil || !gapReplayed.Duplicate || gapReplayed.Seq != gapCreated.Seq {
		t.Fatalf("gap 重放结果错误：结果=%+v 错误=%v", gapReplayed, err)
	}

	var eventCount, utteranceCount, gapCount int64
	if err = db.Model(&models.MeetingEvent{}).Where("meeting_id = ?", testMeetingID).Count(&eventCount).Error; err != nil {
		t.Fatalf("统计 event 失败：%v", err)
	}
	if err = db.Model(&models.Utterance{}).Where("meeting_id = ?", testMeetingID).Count(&utteranceCount).Error; err != nil {
		t.Fatalf("统计 utterance 失败：%v", err)
	}
	if err = db.Model(&models.ASRGap{}).Where("meeting_id = ?", testMeetingID).Count(&gapCount).Error; err != nil {
		t.Fatalf("统计 gap 失败：%v", err)
	}
	if eventCount != 2 || utteranceCount != 1 || gapCount != 1 {
		t.Fatalf("幂等写入数量错误：events=%d utterances=%d gaps=%d", eventCount, utteranceCount, gapCount)
	}
	if notified != 2 {
		t.Fatalf("只有新提交的 final/gap 应通知投影：%d", notified)
	}
}

// TestEventService_RejectsConflictingFinalReplay 验证同一 provider 结果不能静默覆盖已持久化原始记录。
func TestEventService_RejectsConflictingFinalReplay(t *testing.T) {
	service, _ := newEventServiceForTest(t,
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
	)
	input := FinalInput{MeetingID: testMeetingID, ASRSessionID: testSessionID, ProviderResultID: "provider-final-1", Text: "原始内容", Range: mustSampleRange(t, 0, 16000), LastSentSample: 64000}
	if _, err := service.PersistFinal(context.Background(), input); err != nil {
		t.Fatalf("写入初始 final 失败：%v", err)
	}
	input.Text = "冲突内容"
	_, err := service.PersistFinal(context.Background(), input)
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeASRFinalInvalid.ErrorCode {
		t.Fatalf("冲突重放必须返回 ASR_FINAL_INVALID，实际：%v", err)
	}
}

// TestTimelineServiceListsFinalAndGapByCursor 验证 Timeline 使用持久 seq、匿名 session 顺序且支持 afterSeq。
func TestTimelineServiceListsFinalAndGapByCursor(t *testing.T) {
	service, db := newEventServiceForTest(t,
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666",
	)
	if _, err := service.PersistFinal(context.Background(), FinalInput{MeetingID: testMeetingID, ASRSessionID: testSessionID, ProviderResultID: "provider-final-1", Text: "第一句", Range: mustSampleRange(t, 0, 16000), LastSentSample: 64000}); err != nil {
		t.Fatalf("持久化 Timeline final 失败：%v", err)
	}
	if _, err := service.PersistGap(context.Background(), GapInput{MeetingID: testMeetingID, ASRSessionID: pointer(testSessionID), Range: mustSampleRange(t, 16000, 32000), Reason: transcriptdomain.GapDisconnected}); err != nil {
		t.Fatalf("持久化 Timeline gap 失败：%v", err)
	}
	timeline := NewTimelineService(transcriptrepository.NewRepository(db))
	entries, err := timeline.List(context.Background(), testMeetingID, 0, 100)
	if err != nil {
		t.Fatalf("读取 Timeline 失败：%v", err)
	}
	if len(entries) != 2 || entries[0].Seq != 1 || entries[0].Kind != "utterance.final" || entries[0].SessionOrder != 1 || entries[1].Seq != 2 || entries[1].GapReason != "disconnected" {
		t.Fatalf("Timeline 判别联合错误：%+v", entries)
	}
	after, err := timeline.List(context.Background(), testMeetingID, 1, 100)
	if err != nil || len(after) != 1 || after[0].Seq != 2 {
		t.Fatalf("Timeline afterSeq 错误：%+v err=%v", after, err)
	}
}

// TestEventOccurredAtRestoresDiscardedWallTime 验证逻辑样本压缩后仍能映射回真实会议墙钟时间。
func TestEventOccurredAtRestoresDiscardedWallTime(t *testing.T) {
	startedAt := int64(1_000)
	meeting := models.Meeting{StartedAt: &startedAt}
	occurredAt := eventOccurredAt(meeting, mustSampleRange(t, 16_000, 32_000), 32_000)
	if occurredAt != 4_000 {
		t.Fatalf("暂停样本墙钟映射错误：got=%d want=4000", occurredAt)
	}
}

// newEventServiceForTest 创建迁移完成的 SQLite 数据库和一场正在录音的会议。
func newEventServiceForTest(t *testing.T, ids ...string) (*EventService, *gorm.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移数据库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	startedAt := int64(1_000)
	if err = db.Create(&models.Meeting{ID: testMeetingID, MeetingNo: "MS-20260802-0001", Subject: "转写测试", RelativeDir: "meetings/test", LocalTimezone: "Asia/Shanghai", StartedAt: &startedAt, LifecycleState: "recording", LocalSaveState: "saving", RealtimeASRState: "streaming", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled", CreatedAt: startedAt, UpdatedAt: startedAt}).Error; err != nil {
		t.Fatalf("创建会议失败：%v", err)
	}
	if err = db.Create(&models.ASRSession{ID: testSessionID, MeetingID: testMeetingID, Provider: "volcano", State: "streaming", TransportMode: "seed_v1", StartedAt: startedAt, InputStartSample: 0, LastSentSample: 64000, LastFinalSample: 0, CreatedAt: startedAt, UpdatedAt: startedAt}).Error; err != nil {
		t.Fatalf("创建 ASR session 失败：%v", err)
	}
	return NewEventService(EventServiceDependencies{Repository: transcriptrepository.NewRepository(db), Transactions: database.NewTransactionManager(db), IDs: identity.NewFixedGenerator(ids...), Clock: clock.NewFixed(time.UnixMilli(2_000))}), db
}

// mustSampleRange 把测试样本范围转换成领域值，失败时立即报错。
func mustSampleRange(t *testing.T, start int64, end int64) transcriptdomain.SampleRange {
	t.Helper()
	sampleRange, err := transcriptdomain.NewSampleRange(start, end)
	if err != nil {
		t.Fatalf("创建样本范围失败：%v", err)
	}
	return sampleRange
}
