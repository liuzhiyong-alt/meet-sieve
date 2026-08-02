package transcript

import (
	"context"
	"testing"

	"meet-sieve/internal/port"
)

// TestPCMQueue_RejectsFirstSampleOverTwoSecondLimit 验证第 32001 个样本立即背压且不等待消费者。
func TestPCMQueue_RejectsFirstSampleOverTwoSecondLimit(t *testing.T) {
	queue := NewPCMQueue()
	pcm := make([]byte, PCMQueueCapacitySamples*2)
	if !queue.TryAcceptFrame(port.AudioFrame{StartSample: 0, PCM: pcm}) {
		t.Fatal("恰好两秒 PCM 必须可接收")
	}
	if queue.TryAcceptFrame(port.AudioFrame{StartSample: PCMQueueCapacitySamples, PCM: []byte{1, 0}}) {
		t.Fatal("第 32001 个样本必须立即触发背压")
	}
	if queue.BufferedSamples() != PCMQueueCapacitySamples {
		t.Fatalf("队列背压后不得改变积压：%d", queue.BufferedSamples())
	}
}

// TestPCMQueue_TakePreservesFrameOwnership 验证队列保存独立 PCM 副本并可在关闭后排空。
func TestPCMQueue_TakePreservesFrameOwnership(t *testing.T) {
	queue := NewPCMQueue()
	pcm := []byte{1, 0}
	if !queue.TryAcceptFrame(port.AudioFrame{StartSample: 0, PCM: pcm}) {
		t.Fatal("PCM 入队失败")
	}
	pcm[0] = 9
	queue.Close()
	frame, ok, err := queue.Take(context.Background())
	if err != nil || !ok || frame.PCM[0] != 1 {
		t.Fatalf("队列必须返回独立副本：frame=%+v ok=%v err=%v", frame, ok, err)
	}
	_, ok, err = queue.Take(context.Background())
	if err != nil || ok {
		t.Fatalf("关闭且排空后必须结束：ok=%v err=%v", ok, err)
	}
}
