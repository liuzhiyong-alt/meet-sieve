package audio_test

import (
	"context"
	"testing"
	"time"

	"meet-sieve/internal/adapter/audio/malgo"
	"meet-sieve/internal/port"
)

// TestEnumerator_ListsRealCaptureDevices 是本机音频设备真实 smoke。
func TestEnumerator_ListsRealCaptureDevices(t *testing.T) {
	enumerator := malgo.NewEnumerator()
	devices, err := enumerator.ListInputDevices(context.Background())
	if err != nil {
		t.Fatalf("真实设备枚举失败：%v", err)
	}
	t.Logf("malgo 真实输入设备数量：%d", len(devices))
	for _, device := range devices {
		t.Logf("输入设备：name=%q default=%t channels=%d", device.Name, device.IsDefault, device.ChannelCount)
	}
}

// TestEnumerator_CapturesRealPCMFrame 验证默认麦克风可按 Step 2 正式格式打开并返回真实 PCM 帧。
func TestEnumerator_CapturesRealPCMFrame(t *testing.T) {
	enumerator := malgo.NewEnumerator()
	devices, err := enumerator.ListInputDevices(context.Background())
	if err != nil || len(devices) == 0 {
		t.Fatalf("没有可用于实测的输入设备：count=%d err=%v", len(devices), err)
	}
	device := devices[0]
	for _, candidate := range devices {
		if candidate.IsDefault {
			device = candidate
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := enumerator.Start(ctx, device.ID, port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1})
	if err != nil {
		t.Fatalf("打开真实麦克风失败：%v", err)
	}
	defer stream.Stop(context.Background())
	frame, err := stream.ReadFrames(ctx)
	if err != nil {
		t.Fatalf("读取真实 PCM 帧失败：%v", err)
	}
	if len(frame.PCM) == 0 || len(frame.PCM)%2 != 0 || frame.StartSample != 0 {
		t.Fatalf("真实 PCM 帧结构不正确：bytes=%d start=%d", len(frame.PCM), frame.StartSample)
	}
	t.Logf("真实麦克风 %q 返回 %d bytes PCM16", device.Name, len(frame.PCM))
}
