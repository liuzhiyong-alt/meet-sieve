package meeting

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestRuntimeServiceStartsOnlyAfterFirstFrameAndStateCommit 验证开始成功同时具备首帧文件和 recording/saving 数据库事实。
func TestRuntimeServiceStartsOnlyAfterFirstFrameAndStateCommit(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	currentClock := clock.NewFixed(time.Date(2026, 8, 1, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	meetingService := NewService(Dependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		),
		Clock: currentClock, DeviceCode: "ABCD",
	})
	capture := &fakeAudioCapture{stream: newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0, 2, 0}})}
	coordinator := NewRecordingCoordinator(capture, 960000, 32000, time.Second)
	persistedFrames := make(chan port.AudioFrame, 1)
	lanLifecycle := &observingLANLifecycle{repository: repository}
	runtimeService := NewRuntimeService(RuntimeDependencies{
		Meetings: meetingService, Repository: repository, Coordinator: coordinator,
		Capture: capture, Clock: currentClock, IDs: identity.NewFixedGenerator(
			"44444444-4444-4444-8444-444444444444",
			"55555555-5555-4555-8555-555555555555",
		), WorkspaceRoot: root,
		AvailableBytes: func(string) (uint64, error) { return 2 << 30, nil }, MinimumFreeBytes: 1 << 30,
		PersistedPCMObserver: func(frame port.AudioFrame) { persistedFrames <- frame },
		LAN:                  lanLifecycle,
	})

	started, err := runtimeService.StartMeeting(context.Background(), StartMeetingInput{
		CreatePreparingInput: CreatePreparingInput{
			MeetingNo: "20260801-ABCD-01", TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
		},
		DeviceID: "device-1", LANEnabled: true, LANInterfaceID: "private-interface",
	})
	if err != nil {
		t.Fatalf("开始会议失败：%v", err)
	}
	if started.LifecycleState != "recording" || started.LocalSaveState != "saving" {
		t.Fatalf("开始会议投影不正确：%+v", started)
	}
	if lanLifecycle.startCalls != 1 || lanLifecycle.startedState != "recording" || lanLifecycle.interfaceID != "private-interface" {
		t.Fatalf("LAN 未在录音提交后启动：%+v", lanLifecycle)
	}
	select {
	case frame := <-persistedFrames:
		if frame.StartSample != 0 || len(frame.PCM) != 4 {
			t.Fatalf("已保存 PCM 观察事实错误：%+v", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("已保存 PCM 未送达观察器")
	}
	partPath := filepath.Join(root, filepath.FromSlash(started.RelativeDir), "audio", "segments", "000001.wav.part")
	if _, err := os.Stat(partPath); err != nil {
		t.Fatalf("开始成功必须存在首帧 part：%v", err)
	}
	if _, err := coordinator.Stop(context.Background()); err != nil {
		t.Fatalf("清理测试录音失败：%v", err)
	}
}

// TestRuntimeServiceVoiceTurnPausesOnlyASR 验证 AI 回答期间录音持续，只有 ASR 投递暂停。
func TestRuntimeServiceVoiceTurnPausesAndResumesMedia(t *testing.T) {
	_, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	transactions := database.NewTransactionManager(db)
	meetingID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	turnID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	prepareMediaPauseMeeting(t, db, meetingID, turnID)
	stream := newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0}})
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 960000, 32000, time.Second)
	if err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments")); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Activate(); err != nil {
		t.Fatal(err)
	}
	transcript := &mediaTranscriptRuntime{}
	runtimeService := NewRuntimeService(RuntimeDependencies{
		Repository: repository, Transactions: transactions, Coordinator: coordinator,
		Transcript: transcript, Clock: clock.NewFixed(time.UnixMilli(2_000)),
		IDs: identity.NewFixedGenerator("cccccccc-cccc-4ccc-8ccc-cccccccccccc"),
	})
	pauseErrors := make(chan error, 1)
	go func() { pauseErrors <- runtimeService.PauseForTurn(context.Background(), meetingID, turnID) }()
	stream.push(port.AudioFrame{StartSample: 1, PCM: []byte{2, 0}})
	if err := <-pauseErrors; err != nil {
		t.Fatalf("暂停语音 turn 媒体失败：%v", err)
	}
	if state := runtimeService.RealtimeASRState(context.Background(), meetingID, "stopped"); state != "paused_for_ai" {
		t.Fatalf("活动媒体暂停未覆盖实时转写状态：%s", state)
	}
	resumeErrors := make(chan error, 1)
	go func() { resumeErrors <- runtimeService.ResumeAfterTurn(context.Background(), meetingID, turnID) }()
	stream.push(port.AudioFrame{StartSample: 2, PCM: []byte{3, 0}})
	if err := <-resumeErrors; err != nil {
		t.Fatalf("恢复语音 turn 媒体失败：%v", err)
	}
	var pause models.MeetingMediaPause
	if err := db.Where("agent_turn_id = ?", turnID).Take(&pause).Error; err != nil {
		t.Fatal(err)
	}
	if pause.State != "completed" || pause.LogicalSample == nil || *pause.LogicalSample != 1 || pause.DiscardedSamples != 0 {
		t.Fatalf("媒体暂停事实错误：%+v", pause)
	}
	if transcript.pauseBoundary != 1 || transcript.resumeBoundary != 2 {
		t.Fatalf("ASR 暂停恢复边界错误：%+v", transcript)
	}
	if state := runtimeService.RealtimeASRState(context.Background(), meetingID, "streaming"); state != "streaming" {
		t.Fatalf("媒体恢复后未回落到实时转写事实：%s", state)
	}
	if _, err := coordinator.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// prepareMediaPauseMeeting 写入媒体暂停测试所需的会议、session 和 pending turn。
func prepareMediaPauseMeeting(t *testing.T, db *gorm.DB, meetingID string, turnID string) {
	t.Helper()
	meeting := models.Meeting{ID: meetingID, MeetingNo: "20260806-ABCD-01", Subject: "媒体暂停", RelativeDir: "meetings/media", LocalTimezone: "Asia/Shanghai", LifecycleState: "recording", LocalSaveState: "saving", RealtimeASRState: "streaming", GapState: "none", AgentState: "busy", MinuteState: "not_generated", LANState: "disabled", CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	sessionID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	if err := db.Create(&models.AgentSession{ID: sessionID, MeetingID: meetingID, Provider: "codex", CWDRelativePath: "meetings/media", State: "available", StartedAt: 1, CreatedAt: 1, UpdatedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AgentTurn{ID: turnID, MeetingID: meetingID, AgentSessionID: sessionID, Kind: "answer", State: "pending", IdempotencyKey: "voice-turn", CreatedAt: 1, UpdatedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
}

type mediaTranscriptRuntime struct {
	pauseBoundary  int64
	resumeBoundary int64
}

func (runtime *mediaTranscriptRuntime) Start(context.Context, string, string) error { return nil }
func (runtime *mediaTranscriptRuntime) TryAcceptFrame(port.AudioFrame) bool         { return true }
func (runtime *mediaTranscriptRuntime) Pause(_ context.Context, _ string) (int64, error) {
	runtime.pauseBoundary = 1
	return runtime.pauseBoundary, nil
}
func (runtime *mediaTranscriptRuntime) Resume(_ context.Context, _ string) (int64, error) {
	runtime.resumeBoundary = 2
	return runtime.resumeBoundary, nil
}
func (runtime *mediaTranscriptRuntime) Stop(context.Context, string, int64) error { return nil }

// TestRuntimeServiceEndsWithSegmentsAndVerifiedMergedAsset 验证正常结束保留分片并完成最终录音与数据库终态。
func TestRuntimeServiceEndsWithSegmentsAndVerifiedMergedAsset(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	currentClock := clock.NewFixed(time.Date(2026, 8, 1, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)))
	meetingService := NewService(Dependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		), Clock: currentClock, DeviceCode: "ABCD",
	})
	capture := &fakeAudioCapture{stream: newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0, 2, 0}})}
	lanLifecycle := &observingLANLifecycle{repository: repository}
	rawRecord := &observingRawRecordFlusher{repository: repository}
	runtimeService := NewRuntimeService(RuntimeDependencies{
		Meetings: meetingService, Repository: repository,
		Coordinator: NewRecordingCoordinator(capture, 960000, 32000, time.Second),
		Capture:     capture, Clock: currentClock, IDs: identity.NewFixedGenerator(
			"44444444-4444-4444-8444-444444444444",
			"55555555-5555-4555-8555-555555555555",
		), WorkspaceRoot: root,
		AvailableBytes: func(string) (uint64, error) { return 2 << 30, nil }, MinimumFreeBytes: 1 << 30,
		LAN: lanLifecycle, RawRecord: rawRecord,
	})
	started, err := runtimeService.StartMeeting(context.Background(), StartMeetingInput{
		CreatePreparingInput: CreatePreparingInput{
			MeetingNo: "20260801-ABCD-01", TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
		}, DeviceID: "device-1",
	})
	if err != nil {
		t.Fatalf("开始会议失败：%v", err)
	}

	type endResult struct {
		meeting models.Meeting
		err     error
	}
	endResults := make(chan endResult, 2)
	startEnd := make(chan struct{})
	for range 2 {
		go func() {
			<-startEnd
			meeting, endErr := runtimeService.EndMeeting(context.Background(), started.ID)
			endResults <- endResult{meeting: meeting, err: endErr}
		}()
	}
	close(startEnd)
	first := <-endResults
	second := <-endResults
	if first.err != nil || second.err != nil {
		t.Fatalf("并发结束会议失败：first=%v second=%v", first.err, second.err)
	}
	ended := first.meeting
	if second.meeting.ID != ended.ID || capture.stream.stopCount != 1 {
		t.Fatalf("并发结束必须复用唯一结果：first=%+v second=%+v stop=%d", ended, second.meeting, capture.stream.stopCount)
	}
	if lanLifecycle.stopCalls != 1 {
		t.Fatalf("并发结束未只停止一次 LAN：stop=%d", lanLifecycle.stopCalls)
	}
	if rawRecord.flushCalls != 1 || rawRecord.lifecycleState != "finalizing" || rawRecord.localSaveState != "saving" {
		t.Fatalf("原始记录未在本地 saved 提交前强制刷新：%+v", rawRecord)
	}
	if ended.LifecycleState != "ended" || ended.LocalSaveState != "saved" {
		t.Fatalf("会议终态不正确：%+v", ended)
	}
	audioDirectory := filepath.Join(root, filepath.FromSlash(started.RelativeDir), "audio")
	for _, path := range []string{filepath.Join(audioDirectory, "segments", "000001.wav"), filepath.Join(audioDirectory, "recording.wav")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("安全结束缺少录音文件 %s：%v", path, err)
		}
	}
	var assets []models.AudioAsset
	if err := db.Where("meeting_id = ?", started.ID).Order("kind, sequence_no").Find(&assets).Error; err != nil {
		t.Fatalf("读取音频资产失败：%v", err)
	}
	if len(assets) != 2 || assets[0].State != "ready" || assets[1].State != "ready" {
		t.Fatalf("音频资产不完整：%+v", assets)
	}
}

type observingLANLifecycle struct {
	repository   *meetingrepository.Repository
	startCalls   int
	stopCalls    int
	startedState string
	interfaceID  string
}

type observingRawRecordFlusher struct {
	repository     *meetingrepository.Repository
	flushCalls     int
	lifecycleState string
	localSaveState string
}

// Flush 记录强制刷新发生时数据库仍处于核心收尾提交前状态。
func (flusher *observingRawRecordFlusher) Flush(ctx context.Context, meetingID string) error {
	flusher.flushCalls++
	meeting, err := flusher.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return err
	}
	flusher.lifecycleState = meeting.LifecycleState
	flusher.localSaveState = meeting.LocalSaveState
	return nil
}

// StartMeeting 记录 LAN 启动时 SQLite 已提交的会议状态。
func (lifecycle *observingLANLifecycle) StartMeeting(ctx context.Context, meetingID string, interfaceID string) error {
	lifecycle.startCalls++
	lifecycle.interfaceID = interfaceID
	meeting, err := lifecycle.repository.GetMeeting(ctx, meetingID)
	if err == nil {
		lifecycle.startedState = meeting.LifecycleState
	}
	return err
}

// StopMeeting 记录录音收尾前的 LAN 停止请求。
func (lifecycle *observingLANLifecycle) StopMeeting(_ context.Context, _ string) error {
	lifecycle.stopCalls++
	return nil
}

// TestRuntimeServiceRejectsLowDiskBeforeAudioAndDatabase 验证磁盘不足不会打开设备或创建活动会议。
func TestRuntimeServiceRejectsLowDiskBeforeAudioAndDatabase(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	capture := &fakeAudioCapture{stream: newFakeAudioStream()}
	runtimeService := NewRuntimeService(RuntimeDependencies{
		Meetings: &Service{}, Repository: repository,
		Coordinator: NewRecordingCoordinator(capture, 960000, 32000, time.Second), Capture: capture,
		Clock: clock.NewFixed(time.Unix(1, 0)), IDs: identity.NewUUIDGenerator(), WorkspaceRoot: root,
		AvailableBytes: func(string) (uint64, error) { return (1 << 30) - 1, nil }, MinimumFreeBytes: 1 << 30,
	})

	_, err := runtimeService.StartMeeting(context.Background(), StartMeetingInput{})
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != "MEETING_DISK_SPACE_LOW" {
		t.Fatalf("磁盘不足错误语义不正确：%v", err)
	}
	if capture.testCount != 0 || capture.startCount != 0 {
		t.Fatalf("磁盘不足不得访问设备：test=%d start=%d", capture.testCount, capture.startCount)
	}
	var count int64
	if err := db.Model(&models.Meeting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("磁盘不足不得创建会议：count=%d err=%v", count, err)
	}
}

// TestRuntimeServiceMapsMicrophonePermissionDenied 验证权限拒绝使用会议上下文稳定错误且不创建会议。
func TestRuntimeServiceMapsMicrophonePermissionDenied(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	capture := &fakeAudioCapture{stream: newFakeAudioStream(), testErr: port.ErrAudioPermissionDenied}
	runtimeService := NewRuntimeService(RuntimeDependencies{
		Meetings: &Service{}, Repository: repository,
		Coordinator: NewRecordingCoordinator(capture, 960000, 32000, time.Second), Capture: capture,
		Clock: clock.NewFixed(time.Unix(1, 0)), IDs: identity.NewUUIDGenerator(), WorkspaceRoot: root,
		AvailableBytes: func(string) (uint64, error) { return 2 << 30, nil }, MinimumFreeBytes: 1 << 30,
	})

	_, err := runtimeService.StartMeeting(context.Background(), StartMeetingInput{DeviceID: "device-1"})
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != "MEETING_AUDIO_PERMISSION_DENIED" {
		t.Fatalf("麦克风权限错误语义不正确：%v", err)
	}
	var count int64
	if err := db.Model(&models.Meeting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("权限拒绝不得创建会议：count=%d err=%v", count, err)
	}
}

// TestRuntimeServiceTimesOutBlockedDeviceTest 验证原生设备打开无响应时不会永久卡住创建页。
func TestRuntimeServiceTimesOutBlockedDeviceTest(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	capture := &fakeAudioCapture{stream: newFakeAudioStream(), testWait: make(chan struct{})}
	runtimeService := NewRuntimeService(RuntimeDependencies{
		Meetings: &Service{}, Repository: repository,
		Coordinator: NewRecordingCoordinator(capture, 960000, 32000, time.Second), Capture: capture,
		Clock: clock.NewFixed(time.Unix(1, 0)), IDs: identity.NewUUIDGenerator(), WorkspaceRoot: root,
		AvailableBytes: func(string) (uint64, error) { return 2 << 30, nil }, MinimumFreeBytes: 1 << 30,
		DeviceTestTimeout: 10 * time.Millisecond,
	})

	_, err := runtimeService.StartMeeting(context.Background(), StartMeetingInput{DeviceID: "device-1"})
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != "MEETING_AUDIO_START_TIMEOUT" {
		t.Fatalf("设备预检超时错误语义不正确：%v", err)
	}
	var count int64
	if err := db.Model(&models.Meeting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("设备预检超时不得创建会议：count=%d err=%v", count, err)
	}
}

// TestRuntimeServiceInterruptsMeetingAfterDeviceFailure 验证运行时设备故障无需用户操作即可落为 interrupted/failed。
func TestRuntimeServiceInterruptsMeetingAfterDeviceFailure(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	currentClock := clock.NewFixed(time.UnixMilli(100))
	meetingService := NewService(Dependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		), Clock: currentClock, DeviceCode: "ABCD",
	})
	capture := &fakeAudioCapture{stream: newFakeAudioStream(
		port.AudioFrame{StartSample: 0, PCM: []byte{1, 0}},
		port.AudioFrame{},
	)}
	runtimeService := NewRuntimeService(RuntimeDependencies{
		Meetings: meetingService, Repository: repository,
		Coordinator: NewRecordingCoordinator(capture, 960000, 32000, time.Second), Capture: capture,
		Clock: currentClock, IDs: identity.NewUUIDGenerator(), WorkspaceRoot: root,
		AvailableBytes: func(string) (uint64, error) { return 2 << 30, nil }, MinimumFreeBytes: 1 << 30,
	})
	started, err := runtimeService.StartMeeting(context.Background(), StartMeetingInput{
		CreatePreparingInput: CreatePreparingInput{
			MeetingNo: "19700101-ABCD-01", TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
		}, DeviceID: "device-1",
	})
	if err != nil {
		t.Fatalf("开始会议失败：%v", err)
	}
	deadline := time.After(time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		stored, queryErr := repository.GetMeeting(context.Background(), started.ID)
		if queryErr == nil && stored.LifecycleState == "interrupted" && stored.LocalSaveState == "failed" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("设备故障没有及时中断会议：meeting=%+v err=%v", stored, queryErr)
		case <-ticker.C:
		}
	}
}

// openRuntimeMeetingDatabase 创建位于测试工作目录内的真实 SQLite。
func openRuntimeMeetingDatabase(t *testing.T) (string, *gorm.DB) {
	t.Helper()
	root := t.TempDir()
	dataDirectory := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		t.Fatalf("创建 data 目录失败：%v", err)
	}
	databasePath := filepath.Join(dataDirectory, "meetings.db")
	if err := database.Migrate(databasePath); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return root, db
}
