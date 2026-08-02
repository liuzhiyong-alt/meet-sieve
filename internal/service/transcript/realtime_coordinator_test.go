package transcript

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestRealtimeCoordinatorPersistsFinalAndStopsSession 验证 PCM、final、session 终态的完整纵向链路。
func TestRealtimeCoordinatorPersistsFinalAndStopsSession(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	remote := newCoordinatorSession(0)
	coordinator := NewRealtimeCoordinator(RealtimeCoordinatorDependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db), Events: events,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber {
			return &coordinatorTranscriber{session: remote}
		},
		IDs:   identity.NewFixedGenerator("33333333-3333-4333-8333-333333333333"),
		Clock: clock.NewFixed(time.UnixMilli(2_000)), Backoff: []time.Duration{0, 0, 0, 0, 0},
		Wait: func(context.Context, time.Duration) error { return nil }, FinalPersistTimeout: time.Second, FinalQueueCapacity: 128,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := coordinator.Start(ctx, testMeetingID, 0, transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeLegacy, AppID: "test-app", AccessToken: "test-token"}); err != nil {
		t.Fatalf("启动 realtime coordinator 失败：%v", err)
	}
	if !coordinator.TryAcceptFrame(port.AudioFrame{StartSample: 0, PCM: make([]byte, 32000)}) {
		t.Fatal("首帧 PCM 应被实时旁路接受")
	}
	select {
	case <-remote.written:
	case <-time.After(time.Second):
		t.Fatal("PCM 未写入 transcriber")
	}
	remote.events <- port.TranscriptionEvent{Type: port.TranscriptionFinal, ResultID: "speaker:0", ProviderResultID: "provider-final-1", Text: "会议开始。", StartSample: 0, EndSample: 16000, LastSentSample: 16000}
	waitForRowCount(t, db, &models.Utterance{}, 1)
	stopContext, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := coordinator.Stop(stopContext, 16000); err != nil {
		t.Fatalf("停止 realtime coordinator 失败：%v", err)
	}
	var session models.ASRSession
	if err := db.Where("meeting_id = ?", testMeetingID).Take(&session).Error; err != nil {
		t.Fatalf("读取 ASR session 失败：%v", err)
	}
	if session.State != "stopped" || session.LastSentSample != 16000 || session.LastFinalSample != 16000 {
		t.Fatalf("ASR session 终态错误：%+v", session)
	}
}

// newRealtimeCoordinatorDatabase 创建真实 migration、会议和统一事件服务。
func newRealtimeCoordinatorDatabase(t *testing.T) (*gorm.DB, *transcriptrepository.Repository, *EventService) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "realtime-coordinator.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移 coordinator 数据库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 coordinator 数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	startedAt := int64(1_000)
	meeting := models.Meeting{ID: testMeetingID, MeetingNo: "MS-20260802-0001", Subject: "实时转写", RelativeDir: "meetings/realtime", LocalTimezone: "Asia/Shanghai", StartedAt: &startedAt, LifecycleState: "recording", LocalSaveState: "saving", RealtimeASRState: "idle", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled", CreatedAt: startedAt, UpdatedAt: startedAt}
	if err = db.Create(&meeting).Error; err != nil {
		t.Fatalf("创建 coordinator 会议失败：%v", err)
	}
	repository := transcriptrepository.NewRepository(db)
	events := NewEventService(EventServiceDependencies{Repository: repository, Transactions: database.NewTransactionManager(db), IDs: identity.NewFixedGenerator("44444444-4444-4444-8444-444444444444", "55555555-5555-4555-8555-555555555555"), Clock: clock.NewFixed(time.UnixMilli(2_000))})
	return db, repository, events
}

// waitForRowCount 通过数据库事实等待异步 final 完成。
func waitForRowCount(t *testing.T, db *gorm.DB, model any, want int64) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil {
			t.Fatalf("查询异步持久化行数失败：%v", err)
		}
		if count == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("等待异步持久化超时：got=%d want=%d", count, want)
		case <-ticker.C:
		}
	}
}

type coordinatorTranscriber struct {
	session *coordinatorSession
}

// Start 返回当前测试物理 session，并发布连接建立事件。
func (transcriber *coordinatorTranscriber) Start(context.Context, port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	transcriber.session.events <- port.TranscriptionEvent{Type: port.TranscriptionSessionStarted, ProviderSessionID: "provider-session-1"}
	return transcriber.session, nil
}

type coordinatorSession struct {
	mu       sync.Mutex
	lastSent int64
	events   chan port.TranscriptionEvent
	written  chan struct{}
	writeOne sync.Once
	stopOne  sync.Once
}

// newCoordinatorSession 创建可观测 PCM 写入的 fake 外部边界。
func newCoordinatorSession(startSample int64) *coordinatorSession {
	return &coordinatorSession{lastSent: startSample, events: make(chan port.TranscriptionEvent, 8), written: make(chan struct{})}
}

// WriteFrame 接收连续 PCM 并推进真实发送边界。
func (session *coordinatorSession) WriteFrame(_ context.Context, frame port.AudioFrame) error {
	session.mu.Lock()
	session.lastSent = frame.StartSample + int64(len(frame.PCM)/2)
	session.mu.Unlock()
	session.writeOne.Do(func() { close(session.written) })
	return nil
}

// LastSentSample 返回 fake 已发送样本边界。
func (session *coordinatorSession) LastSentSample() int64 {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.lastSent
}

// Events 返回可控厂商事件流。
func (session *coordinatorSession) Events() <-chan port.TranscriptionEvent { return session.events }

// Stop 幂等关闭厂商事件流。
func (session *coordinatorSession) Stop(context.Context) error {
	session.stopOne.Do(func() { close(session.events) })
	return nil
}
