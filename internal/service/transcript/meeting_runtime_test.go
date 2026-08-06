package transcript

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	"meet-sieve/models"
)

// TestMeetingRuntimePauseStartsFreshASRSession 验证 AI 期间不投递 PCM，恢复首帧创建新 session。
func TestMeetingRuntimePauseStartsFreshASRSession(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "meetings", "realtime"), 0o700); err != nil {
		t.Fatalf("创建 realtime 会议目录失败：%v", err)
	}
	apiKey := "test-api-key"
	if err := db.Create(&models.Settings{
		ID: "99999999-9999-4999-8999-999999999999", SingletonKey: 1,
		VolcAPIKey: &apiKey, WakeWord: "会议助手", CreatedAt: 1_000, UpdatedAt: 1_000,
	}).Error; err != nil {
		t.Fatalf("准备 ASR 凭据失败：%v", err)
	}
	sessionStarted := make(chan *coordinatorSession, 2)
	runtime := NewMeetingRuntime(MeetingRuntimeDependencies{
		Settings: NewSettingsService(SettingsServiceDependencies{Repository: repository}), Repository: repository,
		Transactions: database.NewTransactionManager(db), Events: events,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber {
			session := newCoordinatorSession(0)
			sessionStarted <- session
			return &coordinatorTranscriber{session: session}
		},
		IDs: identity.NewFixedGenerator(
			"77777777-7777-4777-8777-777777777777",
			"88888888-8888-4888-8888-888888888888",
		),
		Clock: clock.NewFixed(time.UnixMilli(2_000)), Backoff: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}, ConnectTimeout: time.Second,
		FinalPersistTimeout: time.Second, FinalQueueCapacity: 128, PCMQueueSamples: DefaultPCMQueueCapacitySamples,
		RawRecord: NewRawRecordProjector(RawRecordProjectorDependencies{Repository: repository, WorkspaceRoot: workspace}), WorkspaceRoot: workspace,
	})
	ctx := context.Background()
	if err := runtime.Start(ctx, testMeetingID, MeetingASRModeRealtime); err != nil {
		t.Fatalf("启动 realtime 失败：%v", err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background(), testMeetingID, 20_000) })
	first := waitMeetingRuntimeSession(t, sessionStarted)
	if !runtime.TryAcceptFrame(port.AudioFrame{StartSample: 0, PCM: make([]byte, RealtimePCMPacketSamples*2)}) {
		t.Fatal("首个 PCM 未进入旧 ASR session")
	}
	select {
	case <-first.written:
	case <-time.After(time.Second):
		t.Fatal("等待旧 ASR session 写入超时")
	}
	pauseAt, err := runtime.Pause(ctx, testMeetingID)
	if err != nil {
		t.Fatalf("暂停 realtime 失败：%v", err)
	}
	if pauseAt != RealtimePCMPacketSamples {
		t.Fatalf("暂停边界错误：got=%d want=%d", pauseAt, RealtimePCMPacketSamples)
	}
	if !runtime.TryAcceptFrame(port.AudioFrame{StartSample: RealtimePCMPacketSamples, PCM: make([]byte, RealtimePCMPacketSamples*2)}) {
		t.Fatal("暂停期间应继续接受本地录音观察帧")
	}
	resumeResult := make(chan struct {
		boundary int64
		err      error
	}, 1)
	go func() {
		boundary, resumeErr := runtime.Resume(ctx, testMeetingID)
		resumeResult <- struct {
			boundary int64
			err      error
		}{boundary: boundary, err: resumeErr}
	}()
	waitMeetingRuntimeResuming(t, runtime)
	resumeAt := int64(RealtimePCMPacketSamples * 3)
	if !runtime.TryAcceptFrame(port.AudioFrame{StartSample: resumeAt, PCM: make([]byte, RealtimePCMPacketSamples*2)}) {
		t.Fatal("恢复首帧未进入新 ASR session")
	}
	result := <-resumeResult
	if result.err != nil || result.boundary != resumeAt {
		t.Fatalf("恢复应绑定首个实时帧：boundary=%d err=%v", result.boundary, result.err)
	}
	second := waitMeetingRuntimeSession(t, sessionStarted)
	if second == first {
		t.Fatal("恢复后必须创建新物理 session")
	}
	select {
	case <-second.written:
	case <-time.After(time.Second):
		t.Fatal("等待新 ASR session 写入超时")
	}
	if second.LastSentSample() != resumeAt+RealtimePCMPacketSamples {
		t.Fatalf("新 session 逻辑边界错误：got=%d want=%d", second.LastSentSample(), resumeAt+RealtimePCMPacketSamples)
	}
	var sessions []models.ASRSession
	if err := db.Where("meeting_id = ?", testMeetingID).Order("input_start_sample").Find(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].InputStartSample != 0 || sessions[1].InputStartSample != resumeAt {
		t.Fatalf("暂停恢复 session 起点错误：%+v", sessions)
	}
	var gapCount int64
	if err := db.Model(&models.ASRGap{}).Where("meeting_id = ?", testMeetingID).Count(&gapCount).Error; err != nil || gapCount != 0 {
		t.Fatalf("AI 主动暂停不得创建转写缺口：count=%d err=%v", gapCount, err)
	}
}

// waitMeetingRuntimeResuming 等待恢复调用完成门控登记，避免测试帧抢在被测 goroutine 之前。
func waitMeetingRuntimeResuming(t *testing.T, runtime *MeetingRuntime) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		resuming := runtime.resuming
		runtime.mu.Unlock()
		if resuming {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("恢复调用未进入等待首帧状态")
}

// waitMeetingRuntimeSession 等待异步 coordinator 建立测试物理 session。
func waitMeetingRuntimeSession(t *testing.T, started <-chan *coordinatorSession) *coordinatorSession {
	t.Helper()
	select {
	case session := <-started:
		return session
	case <-time.After(time.Second):
		t.Fatal("等待 ASR session 建立超时")
		return nil
	}
}

// TestMeetingRuntimeRecordOnlyPersistsFullGapAndRawRecord 验证仅录音模式生成整段 gap 和确定性原始记录。
func TestMeetingRuntimeRecordOnlyPersistsFullGapAndRawRecord(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	workspace := t.TempDir()
	meetingDirectory := filepath.Join(workspace, "meetings", "realtime")
	if err := os.MkdirAll(meetingDirectory, 0o700); err != nil {
		t.Fatalf("创建 record_only 会议目录失败：%v", err)
	}
	runtime := NewMeetingRuntime(MeetingRuntimeDependencies{
		Settings:   NewSettingsService(SettingsServiceDependencies{Repository: repository}),
		Repository: repository, Transactions: database.NewTransactionManager(db), Events: events,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber {
			return &coordinatorTranscriber{session: newCoordinatorSession(0)}
		},
		IDs: identity.NewFixedGenerator("77777777-7777-4777-8777-777777777777"), Clock: clock.NewFixed(time.UnixMilli(2_000)),
		Backoff:             []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second},
		ConnectTimeout:      time.Second,
		FinalPersistTimeout: time.Second, FinalQueueCapacity: 128,
		PCMQueueSamples: DefaultPCMQueueCapacitySamples,
		RawRecord:       NewRawRecordProjector(RawRecordProjectorDependencies{Repository: repository, WorkspaceRoot: workspace}), WorkspaceRoot: workspace,
	})
	if err := runtime.Start(context.Background(), testMeetingID, MeetingASRModeRecordOnly); err != nil {
		t.Fatalf("启动 record_only 失败：%v", err)
	}
	if err := db.Model(&models.Meeting{}).Where("id = ?", testMeetingID).Updates(map[string]any{"lifecycle_state": "finalizing"}).Error; err != nil {
		t.Fatalf("准备 record_only 收尾状态失败：%v", err)
	}
	if err := runtime.Stop(context.Background(), testMeetingID, 32000); err != nil {
		t.Fatalf("停止 record_only 失败：%v", err)
	}
	var gap models.ASRGap
	if err := db.Where("meeting_id = ?", testMeetingID).Take(&gap).Error; err != nil {
		t.Fatalf("读取 record_only gap 失败：%v", err)
	}
	if gap.StartSample != 0 || gap.EndSample != 32000 || gap.Reason != "record_only" {
		t.Fatalf("record_only gap 错误：%+v", gap)
	}
	content, err := os.ReadFile(filepath.Join(meetingDirectory, "会议原始记录.md"))
	if err != nil || len(content) == 0 {
		t.Fatalf("record_only 原始记录未生成：bytes=%d err=%v", len(content), err)
	}
}
