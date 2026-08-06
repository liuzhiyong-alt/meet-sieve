package meeting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

// RecordingCoordinator 管理进程内唯一 AudioStream 与分片录音 runner。
type RecordingCoordinator struct {
	capture           port.AudioCapture
	maxSegmentSamples int64
	checkpointSamples int64
	firstFrameTimeout time.Duration

	mu             sync.Mutex
	starting       bool
	session        *recordingSession
	lastSegments   []CompletedSegment
	lastStopErr    error
	segmentHandler CompletedSegmentHandler
	failureHandler RecordingFailureHandler
	frameHandler   PersistedPCMFrameHandler
}

// CompletedSegmentHandler 在完成 WAV 暴露后、继续消费音频前持久化其元数据。
type CompletedSegmentHandler func(ctx context.Context, segment CompletedSegment) error

// RecordingFailureHandler 在不可恢复故障完成资源收尾后更新会议状态。
type RecordingFailureHandler func(ctx context.Context, cause error)

// PersistedPCMFrameHandler 仅接收已经写入本地录音的 PCM 副本。
// 调用方必须非阻塞返回，网络与 SQLite 工作应由其自身队列消费。
type PersistedPCMFrameHandler func(frame port.AudioFrame)

// recordingSession 保存一次活动录音的退出信号和完成分片。
type recordingSession struct {
	stream          port.AudioStream
	recorder        *SegmentRecorder
	cancel          context.CancelFunc
	done            chan struct{}
	segments        []CompletedSegment
	err             error
	stopOnce        sync.Once
	stopSegments    []CompletedSegment
	stopErr         error
	segmentHandler  CompletedSegmentHandler
	failureHandler  RecordingFailureHandler
	frameHandler    PersistedPCMFrameHandler
	activate        chan struct{}
	activateOnce    sync.Once
	control         chan recordingControl
	paused          bool
	pauseOwner      string
	pauseBoundary   RecordingPauseBoundary
	discarded       int64
	pauseDiscarded  int64
	lastResumeOwner string
	lastResume      RecordingResumeBoundary
}

// RecordingPauseBoundary 是录音 runner 已确认停止写入的完整帧边界。
type RecordingPauseBoundary struct {
	LogicalSample  int64
	PhysicalSample int64
}

// RecordingResumeBoundary 是恢复写入时的物理边界和累计丢弃样本。
type RecordingResumeBoundary struct {
	LogicalSample    int64
	PhysicalSample   int64
	DiscardedSamples int64
}

type recordingControlKind string

const (
	recordingControlPause    recordingControlKind = "pause"
	recordingControlResume   recordingControlKind = "resume"
	recordingControlBoundary recordingControlKind = "boundary"
)

type recordingControl struct {
	kind     recordingControlKind
	ownerID  string
	response chan recordingControlResult
}

// Boundary 在完整 PCM 帧边界读取当前连续录音位置，不改变录音写入状态。
func (coordinator *RecordingCoordinator) Boundary(ctx context.Context) (RecordingPauseBoundary, error) {
	if coordinator == nil || ctx == nil {
		return RecordingPauseBoundary{}, fmt.Errorf("读取会议录音边界参数无效")
	}
	session, err := coordinator.activeSession()
	if err != nil {
		return RecordingPauseBoundary{}, err
	}
	response := make(chan recordingControlResult, 1)
	if err := sendRecordingControl(ctx, session, recordingControl{kind: recordingControlBoundary, response: response}); err != nil {
		return RecordingPauseBoundary{}, err
	}
	select {
	case result := <-response:
		return result.pause, result.err
	case <-session.done:
		return RecordingPauseBoundary{}, fmt.Errorf("录音已停止，无法读取边界")
	case <-ctx.Done():
		return RecordingPauseBoundary{}, ctx.Err()
	}
}

type recordingControlResult struct {
	pause  RecordingPauseBoundary
	resume RecordingResumeBoundary
	err    error
}

// SetPersistedPCMFrameHandler 在录音开始前设置实时转写的非阻塞旁路。
func (coordinator *RecordingCoordinator) SetPersistedPCMFrameHandler(handler PersistedPCMFrameHandler) error {
	if coordinator == nil || handler == nil {
		return fmt.Errorf("PCM 旁路处理器无效")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.starting || coordinator.session != nil {
		return fmt.Errorf("录音运行中不能更换 PCM 旁路处理器")
	}
	coordinator.frameHandler = handler
	return nil
}

// SetFailureHandler 在录音开始前设置不可恢复运行时故障处理边界。
func (coordinator *RecordingCoordinator) SetFailureHandler(handler RecordingFailureHandler) error {
	if coordinator == nil || handler == nil {
		return fmt.Errorf("录音故障处理器无效")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.starting || coordinator.session != nil {
		return fmt.Errorf("录音运行中不能更换故障处理器")
	}
	coordinator.failureHandler = handler
	return nil
}

// SetCompletedSegmentHandler 在录音开始前设置完成分片持久化边界。
func (coordinator *RecordingCoordinator) SetCompletedSegmentHandler(handler CompletedSegmentHandler) error {
	if coordinator == nil || handler == nil {
		return fmt.Errorf("完成分片处理器无效")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.starting || coordinator.session != nil {
		return fmt.Errorf("录音运行中不能更换分片处理器")
	}
	coordinator.segmentHandler = handler
	return nil
}

// NewRecordingCoordinator 创建固定格式与时限的单会话录音协调器。
func NewRecordingCoordinator(capture port.AudioCapture, maxSegmentSamples int64, checkpointSamples int64, firstFrameTimeout time.Duration) *RecordingCoordinator {
	return &RecordingCoordinator{
		capture: capture, maxSegmentSamples: maxSegmentSamples,
		checkpointSamples: checkpointSamples, firstFrameTimeout: firstFrameTimeout,
	}
}

// IsCapturing 返回真实 AudioStream 是否已经完成首帧并仍由协调器持有。
func (coordinator *RecordingCoordinator) IsCapturing() bool {
	if coordinator == nil {
		return false
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return coordinator.session != nil
}

// Start 打开固定格式设备流；只有首帧成功写入 part 后才返回成功。
func (coordinator *RecordingCoordinator) Start(ctx context.Context, deviceID string, segmentsDirectory string) error {
	if err := coordinator.reserveStart(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			coordinator.finishFailedStart()
		}
	}()
	stream, err := coordinator.capture.Start(ctx, deviceID, port.AudioFormat{
		SampleRate: recordingSampleRate, BitsPerSample: recordingBitDepth, Channels: recordingChannels,
	})
	if err != nil {
		return mapMeetingAudioDeviceError(err, "meeting.recording.device_start")
	}
	recorder, err := NewSegmentRecorder(segmentsDirectory, coordinator.maxSegmentSamples, coordinator.checkpointSamples)
	if err != nil {
		_ = stream.Stop(ctx)
		return err
	}
	sessionContext, cancel := context.WithCancel(ctx)
	firstFrameContext, firstFrameCancel := context.WithTimeout(sessionContext, coordinator.firstFrameTimeout)
	firstFrame, err := stream.ReadFrames(firstFrameContext)
	firstFrameCancel()
	if err != nil {
		cancel()
		_ = stream.Stop(context.Background())
		if errors.Is(err, context.DeadlineExceeded) {
			return apperr.Dependency(apperr.CodeMeetingAudioStartTimeout, err, apperr.WithOp("meeting.recording.first_frame"))
		}
		return apperr.Dependency(apperr.CodeMeetingAudioDeviceUnavailable, err, apperr.WithOp("meeting.recording.first_frame"))
	}
	completed, err := recorder.WriteFrame(firstFrame)
	if err != nil {
		cancel()
		_ = stream.Stop(context.Background())
		_ = recorder.Abort()
		return fmt.Errorf("持久化会议录音首帧失败: %w", err)
	}
	if err := recorder.Checkpoint(); err != nil {
		cancel()
		_ = stream.Stop(context.Background())
		_ = recorder.Abort()
		return apperr.Dependency(apperr.CodeMeetingRecordingSyncFailed, err, apperr.WithOp("meeting.recording.first_frame_sync"))
	}
	if err := handleCompletedSegments(sessionContext, coordinator.segmentHandler, completed); err != nil {
		cancel()
		_ = stream.Stop(context.Background())
		_ = recorder.Abort()
		return apperr.Dependency(apperr.CodeMeetingRecordingWriteFailed, err, apperr.WithOp("meeting.recording.first_assets"))
	}
	session := &recordingSession{
		stream: stream, recorder: recorder, cancel: cancel, done: make(chan struct{}), segments: completed,
		segmentHandler: coordinator.segmentHandler, failureHandler: coordinator.failureHandler,
		frameHandler: coordinator.frameHandler,
		activate:     make(chan struct{}),
		control:      make(chan recordingControl, 4),
	}
	handlePersistedPCMFrame(session.frameHandler, firstFrame)
	coordinator.mu.Lock()
	coordinator.starting = false
	coordinator.session = session
	coordinator.lastSegments = nil
	coordinator.lastStopErr = nil
	coordinator.mu.Unlock()
	committed = true
	go coordinator.awaitActivation(sessionContext, session)
	return nil
}

// Pause 在下一条完整物理帧写入前关闭录音门；同一 owner 重试幂等。
func (coordinator *RecordingCoordinator) Pause(ctx context.Context, ownerID string) (RecordingPauseBoundary, error) {
	if coordinator == nil || ctx == nil || ownerID == "" {
		return RecordingPauseBoundary{}, fmt.Errorf("暂停会议录音参数无效")
	}
	session, err := coordinator.activeSession()
	if err != nil {
		return RecordingPauseBoundary{}, err
	}
	response := make(chan recordingControlResult, 1)
	request := recordingControl{kind: recordingControlPause, ownerID: ownerID, response: response}
	if err = sendRecordingControl(ctx, session, request); err != nil {
		return RecordingPauseBoundary{}, err
	}
	select {
	case result := <-response:
		return result.pause, result.err
	case <-session.done:
		return RecordingPauseBoundary{}, fmt.Errorf("录音已停止，无法确认暂停")
	case <-ctx.Done():
		return RecordingPauseBoundary{}, ctx.Err()
	}
}

// Resume 在下一条完整物理帧写入前恢复录音门，并返回累计丢弃样本。
func (coordinator *RecordingCoordinator) Resume(ctx context.Context, ownerID string) (RecordingResumeBoundary, error) {
	if coordinator == nil || ctx == nil || ownerID == "" {
		return RecordingResumeBoundary{}, fmt.Errorf("恢复会议录音参数无效")
	}
	session, err := coordinator.activeSession()
	if err != nil {
		return RecordingResumeBoundary{}, err
	}
	response := make(chan recordingControlResult, 1)
	request := recordingControl{kind: recordingControlResume, ownerID: ownerID, response: response}
	if err = sendRecordingControl(ctx, session, request); err != nil {
		return RecordingResumeBoundary{}, err
	}
	select {
	case result := <-response:
		return result.resume, result.err
	case <-session.done:
		return RecordingResumeBoundary{}, fmt.Errorf("录音已停止，无法确认恢复")
	case <-ctx.Done():
		return RecordingResumeBoundary{}, ctx.Err()
	}
}

// activeSession 返回当前活动 runner，不允许控制已完成或尚未建立的录音。
func (coordinator *RecordingCoordinator) activeSession() (*recordingSession, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.session == nil {
		return nil, fmt.Errorf("没有活动会议录音")
	}
	return coordinator.session, nil
}

// sendRecordingControl 把有界控制请求交给 runner，调用方等待独立响应。
func sendRecordingControl(ctx context.Context, session *recordingSession, request recordingControl) error {
	select {
	case session.control <- request:
		return nil
	case <-session.done:
		return fmt.Errorf("录音已停止")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Activate 在 recording/saving 状态事务提交后允许 runner 继续读取后续 PCM。
func (coordinator *RecordingCoordinator) Activate() error {
	if coordinator == nil {
		return fmt.Errorf("录音协调器不可用")
	}
	coordinator.mu.Lock()
	session := coordinator.session
	coordinator.mu.Unlock()
	if session == nil {
		return fmt.Errorf("没有待激活的会议录音")
	}
	session.activateOnce.Do(func() { close(session.activate) })
	return nil
}

// awaitActivation 保证首帧后的持续消费只发生在数据库提交点之后。
func (coordinator *RecordingCoordinator) awaitActivation(ctx context.Context, session *recordingSession) {
	select {
	case <-session.activate:
		coordinator.runSession(ctx, session)
	case <-ctx.Done():
		// 仍调用 runner，让统一 done/错误语义解除 Stop 等待。
		coordinator.runSession(ctx, session)
	}
}

// runSession 消费 PCM，并在不可恢复故障后自动停止设备和发布失败事实。
func (coordinator *RecordingCoordinator) runSession(ctx context.Context, session *recordingSession) {
	runRecording(ctx, session)
	if session.err == nil {
		return
	}
	session.stopOnce.Do(func() {
		session.stopSegments, session.stopErr = stopRecordingSession(context.Background(), session)
	})
	if session.failureHandler != nil {
		session.failureHandler(context.Background(), session.err)
	}
	coordinator.mu.Lock()
	if coordinator.session == session {
		coordinator.session = nil
		coordinator.lastSegments = append([]CompletedSegment(nil), session.stopSegments...)
		coordinator.lastStopErr = session.stopErr
	}
	coordinator.mu.Unlock()
}

// handleCompletedSegments 按序调用持久化边界，任一失败立即阻止后续音频消费。
func handleCompletedSegments(ctx context.Context, handler CompletedSegmentHandler, segments []CompletedSegment) error {
	if handler == nil {
		return nil
	}
	for _, segment := range segments {
		if err := handler(ctx, segment); err != nil {
			return err
		}
	}
	return nil
}

// Stop 停止设备和 runner，安全关闭非空尾片并返回全部完成分片。
func (coordinator *RecordingCoordinator) Stop(ctx context.Context) ([]CompletedSegment, error) {
	coordinator.mu.Lock()
	session := coordinator.session
	if session == nil && coordinator.lastSegments != nil {
		segments := append([]CompletedSegment(nil), coordinator.lastSegments...)
		err := coordinator.lastStopErr
		coordinator.mu.Unlock()
		return segments, err
	}
	coordinator.mu.Unlock()
	if session == nil {
		return nil, fmt.Errorf("没有活动会议录音")
	}
	// stopOnce 让显式结束、关窗结束和故障收尾共享唯一终结者。
	session.stopOnce.Do(func() {
		session.stopSegments, session.stopErr = stopRecordingSession(ctx, session)
	})
	segments := append([]CompletedSegment(nil), session.stopSegments...)
	finalErr := session.stopErr
	coordinator.mu.Lock()
	if coordinator.session == session {
		coordinator.session = nil
		coordinator.lastSegments = append([]CompletedSegment(nil), segments...)
		coordinator.lastStopErr = finalErr
	}
	coordinator.mu.Unlock()
	return segments, finalErr
}

// stopRecordingSession 执行一次设备停止、runner 回收和尾片安全关闭。
func stopRecordingSession(ctx context.Context, session *recordingSession) ([]CompletedSegment, error) {
	session.cancel()
	stopErr := session.stream.Stop(ctx)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-session.done:
	}
	tail, tailErr := session.recorder.CloseCurrent()
	if tail != nil {
		session.segments = append(session.segments, *tail)
	}
	segments := append([]CompletedSegment(nil), session.segments...)
	if session.err != nil {
		return segments, session.err
	}
	if stopErr != nil {
		return segments, fmt.Errorf("停止会议麦克风失败: %w", stopErr)
	}
	return segments, tailErr
}

// reserveStart 在设备调用前占用唯一启动槽，阻止并发双击创建第二条流。
func (coordinator *RecordingCoordinator) reserveStart() error {
	if coordinator == nil || coordinator.capture == nil || coordinator.maxSegmentSamples <= 0 || coordinator.checkpointSamples <= 0 || coordinator.firstFrameTimeout <= 0 {
		return fmt.Errorf("录音协调器依赖或配置无效")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.starting || coordinator.session != nil {
		return fmt.Errorf("已有会议录音正在运行")
	}
	coordinator.starting = true
	return nil
}

// finishFailedStart 释放未进入活动态的启动槽。
func (coordinator *RecordingCoordinator) finishFailedStart() {
	coordinator.mu.Lock()
	coordinator.starting = false
	coordinator.mu.Unlock()
}

// runRecording 持续读取 frame；取消属于正常停止，其他错误保留为会话失败。
func runRecording(ctx context.Context, session *recordingSession) {
	defer close(session.done)
	for {
		frame, err := session.stream.ReadFrames(ctx)
		if err != nil {
			if ctx.Err() == nil {
				session.err = fmt.Errorf("读取会议 PCM 失败: %w", err)
			}
			return
		}
		logicalFrame, writeFrame := applyRecordingControls(session, frame)
		if !writeFrame {
			continue
		}
		completed, err := session.recorder.WriteFrame(logicalFrame)
		if err != nil {
			session.err = err
			return
		}
		handlePersistedPCMFrame(session.frameHandler, logicalFrame)
		if err := handleCompletedSegments(ctx, session.segmentHandler, completed); err != nil {
			session.err = fmt.Errorf("登记完成录音分片失败: %w", err)
			return
		}
		session.segments = append(session.segments, completed...)
	}
}

// applyRecordingControls 在当前物理帧写入前处理全部已排队控制，并决定是否丢弃该帧。
func applyRecordingControls(session *recordingSession, frame port.AudioFrame) (port.AudioFrame, bool) {
	for {
		select {
		case request := <-session.control:
			handleRecordingControl(session, frame.StartSample, request)
		default:
			if session.paused {
				session.discarded += int64(len(frame.PCM) / 2)
				return port.AudioFrame{}, false
			}
			logical := frame
			logical.StartSample = frame.StartSample - session.discarded
			return logical, true
		}
	}
}

// handleRecordingControl 在 runner 单线程内更新 owner 和样本边界。
func handleRecordingControl(session *recordingSession, physicalSample int64, request recordingControl) {
	logicalSample := physicalSample - session.discarded
	switch request.kind {
	case recordingControlBoundary:
		request.response <- recordingControlResult{pause: RecordingPauseBoundary{LogicalSample: logicalSample, PhysicalSample: physicalSample}}
	case recordingControlPause:
		if session.paused && session.pauseOwner != request.ownerID {
			request.response <- recordingControlResult{err: fmt.Errorf("会议录音已由其他任务暂停")}
			return
		}
		if !session.paused {
			session.paused = true
			session.pauseOwner = request.ownerID
			session.pauseDiscarded = session.discarded
			session.lastResumeOwner = ""
			session.lastResume = RecordingResumeBoundary{}
			session.pauseBoundary = RecordingPauseBoundary{LogicalSample: logicalSample, PhysicalSample: physicalSample}
		}
		request.response <- recordingControlResult{pause: session.pauseBoundary}
	case recordingControlResume:
		if !session.paused {
			if session.lastResumeOwner == request.ownerID {
				request.response <- recordingControlResult{resume: session.lastResume}
				return
			}
			request.response <- recordingControlResult{err: fmt.Errorf("会议录音当前未暂停")}
			return
		}
		if session.pauseOwner != request.ownerID {
			request.response <- recordingControlResult{err: fmt.Errorf("会议录音暂停所有者不匹配")}
			return
		}
		resume := RecordingResumeBoundary{
			LogicalSample: logicalSample, PhysicalSample: physicalSample,
			DiscardedSamples: session.discarded - session.pauseDiscarded,
		}
		session.paused = false
		session.lastResumeOwner, session.lastResume = request.ownerID, resume
		session.pauseOwner = ""
		request.response <- recordingControlResult{resume: resume}
	default:
		request.response <- recordingControlResult{err: fmt.Errorf("未知录音控制命令")}
	}
}

// handlePersistedPCMFrame 复制 PCM 所有权，避免采集驱动复用缓冲区影响旁路消费者。
func handlePersistedPCMFrame(handler PersistedPCMFrameHandler, frame port.AudioFrame) {
	if handler == nil {
		return
	}
	pcm := append([]byte(nil), frame.PCM...)
	handler(port.AudioFrame{StartSample: frame.StartSample, PCM: pcm})
}
