package transcript

import (
	"context"
	"testing"

	"meet-sieve/internal/port"
)

// TestPCMQueue_RejectsFirstSampleOverCapacity 验证超过配置容量的首个样本立即背压且不等待消费者。
func TestPCMQueue_RejectsFirstSampleOverCapacity(t *testing.T) {
	queue := NewPCMQueue(DefaultPCMQueueCapacitySamples)
	pcm := make([]byte, DefaultPCMQueueCapacitySamples*2)
	if !queue.TryAcceptFrame(port.AudioFrame{StartSample: 0, PCM: pcm}) {
		t.Fatal("恰好十五秒 PCM 必须可接收")
	}
	if queue.TryAcceptFrame(port.AudioFrame{StartSample: DefaultPCMQueueCapacitySamples, PCM: []byte{1, 0}}) {
		t.Fatal("超过十五秒容量的首个样本必须立即触发背压")
	}
	if queue.BufferedSamples() != DefaultPCMQueueCapacitySamples {
		t.Fatalf("队列背压后不得改变积压：%d", queue.BufferedSamples())
	}
}

// TestPCMQueue_TakePreservesFrameOwnership 验证队列保存独立 PCM 副本并可在关闭后排空。
func TestPCMQueue_TakePreservesFrameOwnership(t *testing.T) {
	queue := NewPCMQueue(DefaultPCMQueueCapacitySamples)
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

// TestPCMQueue_PacketizesVariableFrames 验证音频回调大小不稳定时仍按 200ms 连续发送，关闭时刷出尾包。
func TestPCMQueue_PacketizesVariableFrames(t *testing.T) {
	queue := NewPCMQueue(DefaultPCMQueueCapacitySamples)
	first := make([]byte, 1600)
	second := make([]byte, 5600)
	if !queue.TryAcceptFrame(port.AudioFrame{StartSample: 0, PCM: first}) || !queue.TryAcceptFrame(port.AudioFrame{StartSample: 800, PCM: second}) {
		t.Fatal("不规则 PCM 回调应被接受")
	}
	frame, ok, err := queue.Take(context.Background())
	if err != nil || !ok || frame.StartSample != 0 || len(frame.PCM) != int(RealtimePCMPacketSamples*2) {
		t.Fatalf("首个 200ms 包错误：frame=%+v ok=%v err=%v", frame, ok, err)
	}
	queue.Close()
	tail, ok, err := queue.Take(context.Background())
	if err != nil || !ok || tail.StartSample != RealtimePCMPacketSamples || len(tail.PCM) != 800 {
		t.Fatalf("关闭时尾包错误：frame=%+v ok=%v err=%v", tail, ok, err)
	}
}

// TestPCMQueue_RejectsDiscontinuousFrame 验证音频位置跳变不会被错误拼接为同一 WebSocket 包。
func TestPCMQueue_RejectsDiscontinuousFrame(t *testing.T) {
	queue := NewPCMQueue(DefaultPCMQueueCapacitySamples)
	if !queue.TryAcceptFrame(port.AudioFrame{StartSample: 0, PCM: make([]byte, 640)}) {
		t.Fatal("首帧应被接受")
	}
	if queue.TryAcceptFrame(port.AudioFrame{StartSample: 321, PCM: make([]byte, 640)}) {
		t.Fatal("不连续 PCM 不应被拼接")
	}
}
