package transcript

import (
	"context"
	"path/filepath"
	"sync"
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
		Clock: clock.NewFixed(time.UnixMilli(2_000)), Backoff: []time.Duration{0, 0, 0, 0, 0}, ConnectTimeout: time.Second,
		Wait: func(context.Context, time.Duration) error { return nil }, FinalPersistTimeout: time.Second, FinalQueueCapacity: 128,
		PCMQueueSamples: DefaultPCMQueueCapacitySamples,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := coordinator.Start(ctx, testMeetingID, 0, transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "test-key"}); err != nil {
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

// TestRealtimeCoordinatorBuffersAudioWhileConnecting 验证五秒建连期间的开头音频不会触发背压或丢失。
func TestRealtimeCoordinatorBuffersAudioWhileConnecting(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	remote := newCoordinatorSession(0)
	transcriber := &delayedCoordinatorTranscriber{
		session: remote,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	coordinator := NewRealtimeCoordinator(RealtimeCoordinatorDependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db), Events: events,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber { return transcriber },
		IDs:         identity.NewFixedGenerator("66666666-6666-4666-8666-666666666666"), Clock: clock.NewFixed(time.UnixMilli(2_000)),
		Backoff: []time.Duration{0, 0, 0, 0, 0}, ConnectTimeout: time.Second, Wait: func(context.Context, time.Duration) error { return nil },
		FinalPersistTimeout: time.Second, FinalQueueCapacity: 128, PCMQueueSamples: DefaultPCMQueueCapacitySamples,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := coordinator.Start(ctx, testMeetingID, 0, transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "test-key"}); err != nil {
		t.Fatalf("启动 realtime coordinator 失败：%v", err)
	}
	select {
	case <-transcriber.started:
	case <-time.After(time.Second):
		t.Fatal("ASR 建连未开始")
	}

	// 用五个一秒帧复现真实网络握手慢于旧两秒队列的场景。
	for second := int64(0); second < 5; second++ {
		startSample := second * transcriptdomain.SampleRate
		if !coordinator.TryAcceptFrame(port.AudioFrame{StartSample: startSample, PCM: make([]byte, transcriptdomain.SampleRate*2)}) {
			t.Fatalf("建连期间第 %d 秒 PCM 不应被拒绝", second+1)
		}
	}
	close(transcriber.release)
	waitForLastSentSample(t, remote, 5*transcriptdomain.SampleRate)

	stopContext, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := coordinator.Stop(stopContext, 5*transcriptdomain.SampleRate); err != nil {
		t.Fatalf("停止 realtime coordinator 失败：%v", err)
	}
}

// TestRealtimeCoordinatorConnectionAttemptTimesOut 验证 adapter 不返回时状态仍会退出 connecting 并最终降级。
func TestRealtimeCoordinatorConnectionAttemptTimesOut(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	transcriber := &hangingCoordinatorTranscriber{release: make(chan struct{})}
	reports := make(chan RealtimeFailureReport, 1)
	coordinator := NewRealtimeCoordinator(RealtimeCoordinatorDependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db), Events: events,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber { return transcriber },
		IDs: identity.NewFixedGenerator(
			"10000000-0000-4000-8000-000000000001", "10000000-0000-4000-8000-000000000002",
			"10000000-0000-4000-8000-000000000003", "10000000-0000-4000-8000-000000000004",
			"10000000-0000-4000-8000-000000000005", "10000000-0000-4000-8000-000000000006",
		),
		Clock: clock.NewFixed(time.UnixMilli(2_000)), Backoff: []time.Duration{0, 0, 0, 0, 0},
		ConnectTimeout: 20 * time.Millisecond, Wait: func(context.Context, time.Duration) error { return nil },
		FinalPersistTimeout: time.Second, FinalQueueCapacity: 128, PCMQueueSamples: DefaultPCMQueueCapacitySamples,
		ReportFailure: func(report RealtimeFailureReport) { reports <- report },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := coordinator.Start(ctx, testMeetingID, 0, transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "test-key"}); err != nil {
		t.Fatalf("启动 realtime coordinator 失败：%v", err)
	}
	waitForMeetingASRState(t, db, "unavailable")
	select {
	case report := <-reports:
		if report.ErrorCode != apperr.CodeASRConnectTimeout.ErrorCode || report.Cause == nil {
			t.Fatalf("建连超时报告错误：%+v", report)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到实时转写建连超时报告")
	}
	close(transcriber.release)

	stopContext, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := coordinator.Stop(stopContext, 0); err != nil {
		t.Fatalf("停止超时 coordinator 失败：%v", err)
	}
}

// TestRealtimeCoordinatorIgnoresLateStreamingEvent 验证旧连接的迟到事件不能覆盖失败终态。
func TestRealtimeCoordinatorIgnoresLateStreamingEvent(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	remote := newCoordinatorSession(0)
	coordinator := NewRealtimeCoordinator(RealtimeCoordinatorDependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db), Events: events,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber {
			return &coordinatorTranscriber{session: remote}
		},
		IDs: identity.NewFixedGenerator("88888888-8888-4888-8888-888888888888"), Clock: clock.NewFixed(time.UnixMilli(2_000)),
		Backoff: []time.Duration{0, 0, 0, 0, 0}, ConnectTimeout: time.Second, Wait: func(context.Context, time.Duration) error { return nil },
		FinalPersistTimeout: time.Second, FinalQueueCapacity: 128, PCMQueueSamples: DefaultPCMQueueCapacitySamples,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := coordinator.Start(ctx, testMeetingID, 0, transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "test-key"}); err != nil {
		t.Fatalf("启动 realtime coordinator 失败：%v", err)
	}
	current := waitForCurrentSession(t, coordinator)
	coordinator.handleFailure(realtimeFailure{
		generation: current.generation,
		code:       apperr.CodeASRStreamInterrupted.ErrorCode,
		retryable:  false,
		reason:     transcriptdomain.GapDisconnected,
	})
	coordinator.markStreaming(current, "late-provider-session")

	var meeting models.Meeting
	if err := db.Where("id = ?", testMeetingID).Take(&meeting).Error; err != nil {
		t.Fatalf("读取会议状态失败：%v", err)
	}
	var session models.ASRSession
	if err := db.Where("id = ?", current.id).Take(&session).Error; err != nil {
		t.Fatalf("读取 ASR session 失败：%v", err)
	}
	lateProviderSession := session.ProviderSessionID != nil && *session.ProviderSessionID == "late-provider-session"
	if meeting.RealtimeASRState != "unavailable" || session.State != "failed" || lateProviderSession {
		t.Fatalf("迟到事件不应复活失败连接：meeting=%s session=%+v", meeting.RealtimeASRState, session)
	}

	stopContext, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := coordinator.Stop(stopContext, 0); err != nil {
		t.Fatalf("停止 realtime coordinator 失败：%v", err)
	}
}

// TestFinalPersistFailureKeepsRetryability 验证可恢复的 SQLite final 失败会进入自动重连链路。
func TestFinalPersistFailureKeepsRetryability(t *testing.T) {
	failure := finalPersistFailure(apperr.Dependency(apperr.CodeASREventPersistFailed, context.DeadlineExceeded))
	if failure.code != apperr.CodeASREventPersistFailed.ErrorCode || !failure.retryable || failure.reason != transcriptdomain.GapBackpressure {
		t.Fatalf("final 持久化失败分类错误：%+v", failure)
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

type delayedCoordinatorTranscriber struct {
	session *coordinatorSession
	started chan struct{}
	release chan struct{}
}

type hangingCoordinatorTranscriber struct {
	release chan struct{}
}

// Start 故意忽略 context，复现第三方 adapter 卡住且迟到返回的情况。
func (transcriber *hangingCoordinatorTranscriber) Start(context.Context, port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	<-transcriber.release
	return newCoordinatorSession(0), nil
}

// Start 等待测试放行后才返回物理 session，用于模拟慢速 WebSocket 握手。
func (transcriber *delayedCoordinatorTranscriber) Start(ctx context.Context, _ port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	close(transcriber.started)
	select {
	case <-transcriber.release:
		transcriber.session.events <- port.TranscriptionEvent{Type: port.TranscriptionSessionStarted, ProviderSessionID: "provider-session-delayed"}
		return transcriber.session, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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

// waitForLastSentSample 等待 fake session 消费到指定样本边界。
func waitForLastSentSample(t *testing.T, session *coordinatorSession, want int64) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if got := session.LastSentSample(); got == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("等待 PCM 发送超时：got=%d want=%d", session.LastSentSample(), want)
		case <-ticker.C:
		}
	}
}

// waitForCurrentSession 等待异步建连提交当前物理 session。
func waitForCurrentSession(t *testing.T, coordinator *RealtimeCoordinator) *physicalSession {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		coordinator.mu.Lock()
		current := coordinator.current
		coordinator.mu.Unlock()
		if current != nil {
			return current
		}
		select {
		case <-deadline.C:
			t.Fatal("等待当前物理 ASR session 超时")
		case <-ticker.C:
		}
	}
}

// waitForMeetingASRState 通过数据库事实等待状态机完成降级。
func waitForMeetingASRState(t *testing.T, db *gorm.DB, want string) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var meeting models.Meeting
		if err := db.Where("id = ?", testMeetingID).Take(&meeting).Error; err != nil {
			t.Fatalf("读取会议 ASR 状态失败：%v", err)
		}
		if meeting.RealtimeASRState == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("等待会议 ASR 状态超时：got=%s want=%s", meeting.RealtimeASRState, want)
		case <-ticker.C:
		}
	}
}
