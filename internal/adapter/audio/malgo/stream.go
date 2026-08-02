package malgo

import (
	"context"
	"fmt"
	"sync"

	"meet-sieve/internal/port"

	malgoSDK "github.com/gen2brain/malgo"
)

const captureFrameQueueSize = 64

// captureStream 把 malgo 回调转换为可取消的连续 PCM 帧读取。
type captureStream struct {
	context    *malgoSDK.AllocatedContext
	device     *malgoSDK.Device
	frames     chan []byte
	overflow   chan struct{}
	stopped    chan struct{}
	release    func()
	stopOnce   sync.Once
	stopErr    error
	nextSample int64
}

// newCaptureStream 创建尚未绑定设备的采集流。
func newCaptureStream(deviceContext *malgoSDK.AllocatedContext, release func()) *captureStream {
	return &captureStream{
		context: deviceContext, frames: make(chan []byte, captureFrameQueueSize),
		overflow: make(chan struct{}, 1), stopped: make(chan struct{}), release: release,
	}
}

// callbacks 返回只复制 PCM 且不阻塞实时音频线程的回调。
func (stream *captureStream) callbacks() malgoSDK.DeviceCallbacks {
	return malgoSDK.DeviceCallbacks{Data: func(_ []byte, input []byte, _ uint32) {
		copied := append([]byte(nil), input...)
		select {
		case stream.frames <- copied:
		default:
			select {
			case stream.overflow <- struct{}{}:
			default:
			}
		}
	}}
}

// ReadFrames 等待下一段连续 PCM；context 取消会立即解除等待。
func (stream *captureStream) ReadFrames(ctx context.Context) (port.AudioFrame, error) {
	select {
	case <-ctx.Done():
		return port.AudioFrame{}, ctx.Err()
	case <-stream.stopped:
		return port.AudioFrame{}, fmt.Errorf("音频采集已停止")
	case <-stream.overflow:
		return port.AudioFrame{}, fmt.Errorf("音频采集消费过慢，连续性已丢失")
	case pcm := <-stream.frames:
		frame := port.AudioFrame{PCM: pcm, StartSample: stream.nextSample}
		stream.nextSample += int64(len(pcm) / 2)
		return frame, nil
	}
}

// Stop 幂等停止设备、释放 malgo context 并解除后续读取。
func (stream *captureStream) Stop(_ context.Context) error {
	stream.stopOnce.Do(func() {
		stream.stopErr = stream.stopResources()
		close(stream.stopped)
	})
	return stream.stopErr
}

// stopResources 按设备后 context 的顺序释放原生资源。
func (stream *captureStream) stopResources() error {
	var stopErr error
	if stream.device != nil {
		stopErr = stream.device.Stop()
		stream.device.Uninit()
		stream.device = nil
	}
	stream.releaseContext()
	return stopErr
}

// releaseContext 幂等释放 malgo context 和 adapter 会话占用。
func (stream *captureStream) releaseContext() {
	if stream.context != nil {
		_ = stream.context.Uninit()
		stream.context.Free()
		stream.context = nil
	}
	if stream.release != nil {
		stream.release()
		stream.release = nil
	}
}
