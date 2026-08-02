package voice_test

import (
	"context"
	"encoding/binary"
	"math"
	"sync"
	"testing"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
	voiceservice "meet-sieve/internal/service/voice"
)

// TestRecorder_RejectsConcurrentStart 验证同一 Recorder 不启动第二个设备会话。
func TestRecorder_RejectsConcurrentStart(t *testing.T) {
	stream := newFakeAudioStream()
	recorder := voiceservice.NewRecorder(&fakeAudioCapture{stream: stream})
	if err := recorder.Start(context.Background(), "device-1"); err != nil {
		t.Fatalf("启动首次录音失败：%v", err)
	}
	if got := apperr.Normalize(recorder.Start(context.Background(), "device-1")); got.ErrorCode != "VOICE_RECORDING_BUSY" {
		t.Fatalf("并发录音错误不正确：%+v", got)
	}
	stream.frames <- []byte{1, 0, 2, 0}
	<-stream.consumed
	result, err := recorder.Stop(context.Background())
	if err != nil || len(result.PCM) != 4 {
		t.Fatalf("停止录音结果不正确：result=%+v err=%v", result, err)
	}
}

// TestRecorder_SnapshotReportsRealPCMLevel 验证录音音量来自已采集 PCM，而不是前端动画假值。
func TestRecorder_SnapshotReportsRealPCMLevel(t *testing.T) {
	stream := newFakeAudioStream()
	recorder := voiceservice.NewRecorder(&fakeAudioCapture{stream: stream})
	if err := recorder.Start(context.Background(), "device-1"); err != nil {
		t.Fatalf("启动录音失败：%v", err)
	}
	pcm := make([]byte, 160*2)
	for offset := 0; offset < len(pcm); offset += 2 {
		binary.LittleEndian.PutUint16(pcm[offset:], uint16(16384))
	}
	stream.frames <- pcm
	<-stream.consumed

	snapshot := recorder.Snapshot()
	if math.Abs(snapshot.Level-0.5) > 0.001 || snapshot.DurationMS != 10 {
		t.Fatalf("录音快照不正确：%+v", snapshot)
	}
	if _, err := recorder.Stop(context.Background()); err != nil {
		t.Fatalf("停止录音失败：%v", err)
	}
}

// TestRecorder_AutoStopsAtSixtySeconds 验证录音达到硬上限时不写入第 60 秒后的样本。
func TestRecorder_AutoStopsAtSixtySeconds(t *testing.T) {
	stream := newFakeAudioStream()
	recorder := voiceservice.NewRecorder(&fakeAudioCapture{stream: stream})
	if err := recorder.Start(context.Background(), "device-1"); err != nil {
		t.Fatalf("启动录音失败：%v", err)
	}
	stream.frames <- make([]byte, 16000*60*2+200)
	<-stream.consumed
	result, err := recorder.Stop(context.Background())
	if err != nil {
		t.Fatalf("停止自动截断录音失败：%v", err)
	}
	if len(result.PCM) != 16000*60*2 || !result.AutoStopped || result.DurationMS != 60000 {
		t.Fatalf("60 秒录音结果不正确：%+v", result)
	}
}

type fakeAudioCapture struct{ stream *fakeAudioStream }

// ListInputDevices 返回测试设备。
func (capture *fakeAudioCapture) ListInputDevices(context.Context) ([]port.InputDevice, error) {
	return []port.InputDevice{{ID: "device-1", Name: "测试麦克风"}}, nil
}

// TestInputDevice 验证测试设备 ID。
func (capture *fakeAudioCapture) TestInputDevice(context.Context, string) error { return nil }

// Start 返回可控 fake 流。
func (capture *fakeAudioCapture) Start(context.Context, string, port.AudioFormat) (port.AudioStream, error) {
	return capture.stream, nil
}

type fakeAudioStream struct {
	frames   chan []byte
	done     chan struct{}
	consumed chan struct{}
	once     sync.Once
}

// newFakeAudioStream 创建可由测试推送 PCM 的流。
func newFakeAudioStream() *fakeAudioStream {
	return &fakeAudioStream{frames: make(chan []byte, 1), done: make(chan struct{}), consumed: make(chan struct{}, 1)}
}

// ReadFrames 等待测试 PCM 或停止信号。
func (stream *fakeAudioStream) ReadFrames(ctx context.Context) (port.AudioFrame, error) {
	select {
	case <-ctx.Done():
		return port.AudioFrame{}, ctx.Err()
	case <-stream.done:
		return port.AudioFrame{}, context.Canceled
	case pcm := <-stream.frames:
		stream.consumed <- struct{}{}
		return port.AudioFrame{PCM: pcm}, nil
	}
}

// Stop 幂等解除 fake 流读取。
func (stream *fakeAudioStream) Stop(context.Context) error {
	stream.once.Do(func() { close(stream.done) })
	return nil
}
