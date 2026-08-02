package transcript

import (
	"context"
	"fmt"
	"sync"

	"meet-sieve/internal/port"
)

// PCMQueueCapacitySamples 是 ASR 旁路允许积压的固定两秒音频量。
const PCMQueueCapacitySamples int64 = 32000

// PCMQueue 在录音提交后暂存等待网络发送的 PCM，绝不反压录音线程。
type PCMQueue struct {
	mu       sync.Mutex
	frames   []port.AudioFrame
	buffered int64
	closed   bool
	wake     chan struct{}
}

// NewPCMQueue 创建固定容量的 PCM 旁路队列。
func NewPCMQueue() *PCMQueue {
	return &PCMQueue{wake: make(chan struct{}, 1)}
}

// TryAcceptFrame 非阻塞地接收一帧已持久化 PCM；满时返回 false 且不修改队列。
func (queue *PCMQueue) TryAcceptFrame(frame port.AudioFrame) bool {
	samples, ok := frameSampleCount(frame)
	if !ok || queue == nil {
		return false
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed || queue.buffered+samples > PCMQueueCapacitySamples {
		return false
	}
	queue.frames = append(queue.frames, port.AudioFrame{StartSample: frame.StartSample, PCM: append([]byte(nil), frame.PCM...)})
	queue.buffered += samples
	select {
	case queue.wake <- struct{}{}:
	default:
	}
	return true
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
	return queue.buffered
}

// frameSampleCount 验证 Step 3 固定 16-bit mono PCM 的样本数量。
func frameSampleCount(frame port.AudioFrame) (int64, bool) {
	if frame.StartSample < 0 || len(frame.PCM) == 0 || len(frame.PCM)%2 != 0 {
		return 0, false
	}
	return int64(len(frame.PCM) / 2), true
}
