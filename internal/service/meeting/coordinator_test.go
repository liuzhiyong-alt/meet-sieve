package meeting

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

// TestRecordingCoordinatorStartsAfterFirstPersistedFrame 验证启动成功前首帧已经进入安全分片。
func TestRecordingCoordinatorStartsAfterFirstPersistedFrame(t *testing.T) {
	t.Parallel()

	stream := newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0, 2, 0}})
	capture := &fakeAudioCapture{stream: stream}
	coordinator := NewRecordingCoordinator(capture, 960000, 32000, time.Second)
	directory := filepath.Join(t.TempDir(), "segments")

	if err := coordinator.Start(context.Background(), "device-1", directory); err != nil {
		t.Fatalf("启动录音协调器失败：%v", err)
	}
	part, err := os.ReadFile(filepath.Join(directory, "000001.wav.part"))
	if err != nil {
		t.Fatalf("启动成功时首帧 part 必须可读取：%v", err)
	}
	if binary.LittleEndian.Uint32(part[40:44]) != 4 {
		t.Fatalf("启动成功时首帧 header 必须已提交，data size=%d", binary.LittleEndian.Uint32(part[40:44]))
	}
	segments, err := coordinator.Stop(context.Background())
	if err != nil {
		t.Fatalf("停止录音协调器失败：%v", err)
	}
	if len(segments) != 1 || segments[0].StartSample != 0 || segments[0].EndSample != 2 {
		t.Fatalf("停止尾片不正确：%+v", segments)
	}
	if capture.format != (port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}) || stream.stopCount != 1 {
		t.Fatalf("设备格式或停止次数不正确：format=%+v stop=%d", capture.format, stream.stopCount)
	}
}

// TestRecordingCoordinatorStopIsIdempotent 验证重复停止复用首次结果且不会再次停止设备。
func TestRecordingCoordinatorStopIsIdempotent(t *testing.T) {
	t.Parallel()

	stream := newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0}})
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 960000, 32000, time.Second)
	if err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments")); err != nil {
		t.Fatalf("启动录音失败：%v", err)
	}
	first, err := coordinator.Stop(context.Background())
	if err != nil {
		t.Fatalf("首次停止失败：%v", err)
	}
	second, err := coordinator.Stop(context.Background())
	if err != nil || len(second) != len(first) || stream.stopCount != 1 {
		t.Fatalf("重复停止必须复用结果：first=%+v second=%+v stop=%d err=%v", first, second, stream.stopCount, err)
	}
}

// TestRecordingCoordinatorConcurrentStopHasSingleFinalizer 验证并发停止只有一个终结者。
func TestRecordingCoordinatorConcurrentStopHasSingleFinalizer(t *testing.T) {
	t.Parallel()

	stream := newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0}})
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 960000, 32000, time.Second)
	if err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments")); err != nil {
		t.Fatalf("启动录音失败：%v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := coordinator.Stop(context.Background())
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("并发停止失败：%v", err)
		}
	}
	if stream.stopCount != 1 {
		t.Fatalf("并发停止只能停止一次设备，实际 %d 次", stream.stopCount)
	}
}

// TestRecordingCoordinatorMapsFirstFrameTimeout 验证首帧超时停止设备并返回稳定会议错误。
func TestRecordingCoordinatorMapsFirstFrameTimeout(t *testing.T) {
	t.Parallel()

	stream := newFakeAudioStream()
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 960000, 32000, 10*time.Millisecond)
	err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments"))
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeMeetingAudioStartTimeout.ErrorCode {
		t.Fatalf("首帧超时错误不稳定：%v", err)
	}
	if stream.stopCount != 1 {
		t.Fatalf("首帧超时必须停止设备，实际 %d 次", stream.stopCount)
	}
}

// TestRecordingCoordinatorPublishesCompletedSegments 验证每个完成分片在继续消费前交给持久化边界。
func TestRecordingCoordinatorPublishesCompletedSegments(t *testing.T) {
	t.Parallel()

	stream := newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0, 2, 0, 3, 0}})
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 2, 1, time.Second)
	var persisted []CompletedSegment
	if err := coordinator.SetCompletedSegmentHandler(func(_ context.Context, segment CompletedSegment) error {
		persisted = append(persisted, segment)
		return nil
	}); err != nil {
		t.Fatalf("设置分片处理器失败：%v", err)
	}
	if err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments")); err != nil {
		t.Fatalf("启动录音失败：%v", err)
	}
	if len(persisted) != 1 || persisted[0].SequenceNo != 1 || persisted[0].EndSample != 2 {
		t.Fatalf("完成分片没有及时发布：%+v", persisted)
	}
	if _, err := coordinator.Stop(context.Background()); err != nil {
		t.Fatalf("停止录音失败：%v", err)
	}
}

// TestRecordingCoordinatorForwardsOnlyPersistedPCM 验证 PCM 旁路发生在本地 WAV 写入成功之后且使用独立副本。
func TestRecordingCoordinatorForwardsOnlyPersistedPCM(t *testing.T) {
	t.Parallel()

	pcm := []byte{1, 0, 2, 0}
	stream := newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: pcm})
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 960000, 32000, time.Second)
	frames := make(chan port.AudioFrame, 1)
	if err := coordinator.SetPersistedPCMFrameHandler(func(frame port.AudioFrame) { frames <- frame }); err != nil {
		t.Fatalf("设置 PCM 旁路失败：%v", err)
	}
	if err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments")); err != nil {
		t.Fatalf("启动录音失败：%v", err)
	}
	forwarded := <-frames
	pcm[0] = 9
	if forwarded.StartSample != 0 || forwarded.PCM[0] != 1 {
		t.Fatalf("PCM 旁路必须保留写入时独立副本：%+v", forwarded)
	}
	if _, err := coordinator.Stop(context.Background()); err != nil {
		t.Fatalf("停止录音失败：%v", err)
	}
}

// TestRecordingCoordinatorPauseDropsPCMAndResumesContinuousSamples 验证暂停期不落盘，恢复后逻辑样本连续。
func TestRecordingCoordinatorPauseDropsPCMAndResumesContinuousSamples(t *testing.T) {
	stream := newFakeAudioStream(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0}})
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 960000, 32000, time.Second)
	frames := make(chan port.AudioFrame, 4)
	if err := coordinator.SetPersistedPCMFrameHandler(func(frame port.AudioFrame) { frames <- frame }); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments")); err != nil {
		t.Fatal(err)
	}
	<-frames // 首帧已经安全写入。
	if err := coordinator.Activate(); err != nil {
		t.Fatal(err)
	}

	pauseResult := make(chan RecordingPauseBoundary, 1)
	pauseErrors := make(chan error, 1)
	go func() {
		boundary, err := coordinator.Pause(context.Background(), "turn-1")
		pauseResult <- boundary
		pauseErrors <- err
	}()
	waitForRecordingControl(t, coordinator)
	stream.push(port.AudioFrame{StartSample: 1, PCM: []byte{2, 0}})
	if err := <-pauseErrors; err != nil {
		t.Fatalf("暂停录音失败：%v", err)
	}
	if boundary := <-pauseResult; boundary.LogicalSample != 1 || boundary.PhysicalSample != 1 {
		t.Fatalf("暂停边界错误：%+v", boundary)
	}
	resumeResult := make(chan RecordingResumeBoundary, 1)
	resumeErrors := make(chan error, 1)
	go func() {
		boundary, err := coordinator.Resume(context.Background(), "turn-1")
		resumeResult <- boundary
		resumeErrors <- err
	}()
	waitForRecordingControl(t, coordinator)
	stream.push(port.AudioFrame{StartSample: 2, PCM: []byte{4, 0}})
	if err := <-resumeErrors; err != nil {
		t.Fatalf("恢复录音失败：%v", err)
	}
	if boundary := <-resumeResult; boundary.LogicalSample != 1 || boundary.DiscardedSamples != 1 {
		t.Fatalf("恢复边界错误：%+v", boundary)
	}
	if resumed := <-frames; resumed.StartSample != 1 || resumed.PCM[0] != 4 {
		t.Fatalf("恢复帧必须紧接暂停前样本：%+v", resumed)
	}
	secondPause := make(chan error, 1)
	go func() {
		_, err := coordinator.Pause(context.Background(), "turn-2")
		secondPause <- err
	}()
	waitForRecordingControl(t, coordinator)
	stream.push(port.AudioFrame{StartSample: 3, PCM: []byte{5, 0}})
	if err := <-secondPause; err != nil {
		t.Fatalf("第二次暂停录音失败：%v", err)
	}
	secondResume := make(chan recordingControlResult, 1)
	go func() {
		boundary, err := coordinator.Resume(context.Background(), "turn-2")
		secondResume <- recordingControlResult{resume: boundary, err: err}
	}()
	waitForRecordingControl(t, coordinator)
	stream.push(port.AudioFrame{StartSample: 4, PCM: []byte{6, 0}})
	if result := <-secondResume; result.err != nil || result.resume.LogicalSample != 2 || result.resume.DiscardedSamples != 1 {
		t.Fatalf("第二次恢复必须只返回本次丢弃样本：%+v", result)
	}
	if resumed := <-frames; resumed.StartSample != 2 || resumed.PCM[0] != 6 {
		t.Fatalf("第二次恢复帧逻辑样本错误：%+v", resumed)
	}
	if _, err := coordinator.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestRecordingCoordinatorFinalizesRuntimeFailure 验证设备读失败会自动停止并发布唯一故障。
func TestRecordingCoordinatorFinalizesRuntimeFailure(t *testing.T) {
	t.Parallel()

	stream := newFakeAudioStream(
		port.AudioFrame{StartSample: 0, PCM: []byte{1, 0}},
		port.AudioFrame{},
	)
	coordinator := NewRecordingCoordinator(&fakeAudioCapture{stream: stream}, 960000, 32000, time.Second)
	failures := make(chan error, 1)
	if err := coordinator.SetFailureHandler(func(_ context.Context, err error) {
		failures <- err
	}); err != nil {
		t.Fatalf("设置运行时故障处理器失败：%v", err)
	}
	if err := coordinator.Start(context.Background(), "device-1", filepath.Join(t.TempDir(), "segments")); err != nil {
		t.Fatalf("启动录音失败：%v", err)
	}
	if err := coordinator.Activate(); err != nil {
		t.Fatalf("激活持续录音失败：%v", err)
	}
	select {
	case err := <-failures:
		if err == nil {
			t.Fatal("运行时故障必须保留 cause")
		}
	case <-time.After(time.Second):
		t.Fatal("运行时故障没有及时收尾")
	}
	if stream.stopCount != 1 {
		t.Fatalf("运行时故障必须自动停止设备一次，实际 %d", stream.stopCount)
	}
}

type fakeAudioCapture struct {
	stream     *fakeAudioStream
	format     port.AudioFormat
	testCount  int
	startCount int
	testErr    error
	testWait   <-chan struct{}
}

func (capture *fakeAudioCapture) ListInputDevices(context.Context) ([]port.InputDevice, error) {
	return nil, nil
}

func (capture *fakeAudioCapture) TestInputDevice(ctx context.Context, _ string) error {
	capture.testCount++
	if capture.testWait != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-capture.testWait:
		}
	}
	return capture.testErr
}

func (capture *fakeAudioCapture) Start(_ context.Context, _ string, format port.AudioFormat) (port.AudioStream, error) {
	capture.format = format
	capture.startCount++
	return capture.stream, nil
}

type fakeAudioStream struct {
	mu        sync.Mutex
	frames    chan port.AudioFrame
	stopCount int
}

// push 向活动 fake 设备流提交一条物理 PCM 帧。
func (stream *fakeAudioStream) push(frame port.AudioFrame) {
	stream.frames <- frame
}

// waitForRecordingControl 等待测试请求进入 runner 控制队列，不依赖固定 sleep。
func waitForRecordingControl(t *testing.T, coordinator *RecordingCoordinator) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		coordinator.mu.Lock()
		session := coordinator.session
		pending := session != nil && len(session.control) > 0
		coordinator.mu.Unlock()
		if pending {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("录音控制请求没有进入 runner")
}

func newFakeAudioStream(frames ...port.AudioFrame) *fakeAudioStream {
	channel := make(chan port.AudioFrame, len(frames))
	for _, frame := range frames {
		channel <- frame
	}
	return &fakeAudioStream{frames: channel}
}

func (stream *fakeAudioStream) ReadFrames(ctx context.Context) (port.AudioFrame, error) {
	select {
	case <-ctx.Done():
		return port.AudioFrame{}, ctx.Err()
	case frame := <-stream.frames:
		if frame.PCM == nil {
			return port.AudioFrame{}, io.EOF
		}
		return frame, nil
	}
}

func (stream *fakeAudioStream) Stop(context.Context) error {
	stream.mu.Lock()
	stream.stopCount++
	stream.mu.Unlock()
	return nil
}
