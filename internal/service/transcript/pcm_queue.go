package transcript

import (
	"context"
	"fmt"
	"sync"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/port"
)

// DefaultPCMQueueCapacitySamples 是 ASR 建连期间允许积压的固定十五秒音频量。
const DefaultPCMQueueCapacitySamples int64 = 15 * transcriptdomain.SampleRate

// RealtimePCMPacketSamples 是火山双向实时识别建议的 200ms 16kHz PCM 包大小。
const RealtimePCMPacketSamples int64 = transcriptdomain.SampleRate / 5

// PCMQueue 在录音提交后暂存等待网络发送的 PCM，绝不反压录音线程。
type PCMQueue struct {
	mu       sync.Mutex
	frames   []port.AudioFrame
	buffered int64
	capacity int64
	closed   bool
	wake     chan struct{}

	pending      []byte
	pendingStart int64
	nextSample   int64
	initialized  bool
}

// NewPCMQueue 创建指定样本容量的 PCM 旁路队列。
func NewPCMQueue(capacity int64) *PCMQueue {
	return &PCMQueue{capacity: capacity, wake: make(chan struct{}, 1)}
}

// TryAcceptFrame 非阻塞地接收一帧已持久化 PCM，并规整为 200ms 包；满或不连续时不修改队列。
func (queue *PCMQueue) TryAcceptFrame(frame port.AudioFrame) bool {
	samples, ok := frameSampleCount(frame)
	if !ok || queue == nil {
		return false
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed || queue.capacity <= 0 || queue.buffered+int64(len(queue.pending)/2)+samples > queue.capacity {
		return false
	}
	if queue.initialized && frame.StartSample != queue.nextSample {
		return false
	}
	if !queue.initialized {
		queue.pendingStart, queue.nextSample, queue.initialized = frame.StartSample, frame.StartSample, true
	}
	queue.pending = append(queue.pending, frame.PCM...)
	queue.nextSample += samples
	queue.enqueueCompletePackets()
	select {
	case queue.wake <- struct{}{}:
	default:
	}
	return true
}

// enqueueCompletePackets 把积压 PCM 切成固定 200ms 包，保持绝对采样位置连续。
func (queue *PCMQueue) enqueueCompletePackets() {
	packetBytes := int(RealtimePCMPacketSamples * 2)
	for len(queue.pending) >= packetBytes {
		pcm := append([]byte(nil), queue.pending[:packetBytes]...)
		queue.frames = append(queue.frames, port.AudioFrame{StartSample: queue.pendingStart, PCM: pcm})
		queue.pending = queue.pending[packetBytes:]
		queue.pendingStart += RealtimePCMPacketSamples
		queue.buffered += RealtimePCMPacketSamples
	}
}

// Take 等待并取出下一帧；队列关闭且清空后返回 false。
func (queue *PCMQueue) Take(ctx context.Context) (port.AudioFrame, bool, error) {
	if queue == nil {
		return port.AudioFrame{}, false, fmt.Errorf("PCM 队列不能为空")
	}
	for {
		queue.mu.Lock()
		if len(queue.frames) > 0 {
			frame := queue.frames[0]
			queue.frames = queue.frames[1:]
			queue.buffered -= int64(len(frame.PCM) / 2)
			if len(queue.frames) > 0 {
				select {
				case queue.wake <- struct{}{}:
				default:
				}
			}
			queue.mu.Unlock()
			return frame, true, nil
		}
		closed := queue.closed
		queue.mu.Unlock()
		if closed {
			return port.AudioFrame{}, false, nil
		}
		select {
		case <-ctx.Done():
			return port.AudioFrame{}, false, ctx.Err()
		case <-queue.wake:
		}
	}
}

// Close 停止接收新 PCM，同时允许消费者排空已接收帧。
func (queue *PCMQueue) Close() {
	if queue == nil {
		return
	}
	queue.mu.Lock()
	if !queue.closed && len(queue.pending) > 0 {
		queue.frames = append(queue.frames, port.AudioFrame{StartSample: queue.pendingStart, PCM: append([]byte(nil), queue.pending...)})
		queue.buffered += int64(len(queue.pending) / 2)
		queue.pending = nil
	}
	queue.closed = true
	queue.mu.Unlock()
	select {
	case queue.wake <- struct{}{}:
	default:
	}
}

// BufferedSamples 返回当前积压，供 coordinator 在产生 gap 前记录边界。
func (queue *PCMQueue) BufferedSamples() int64 {
	if queue == nil {
		return 0
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return queue.buffered + int64(len(queue.pending)/2)
}

// frameSampleCount 验证 Step 3 固定 16-bit mono PCM 的样本数量。
func frameSampleCount(frame port.AudioFrame) (int64, bool) {
	if frame.StartSample < 0 || len(frame.PCM) == 0 || len(frame.PCM)%2 != 0 {
		return 0, false
	}
	return int64(len(frame.PCM) / 2), true
}
