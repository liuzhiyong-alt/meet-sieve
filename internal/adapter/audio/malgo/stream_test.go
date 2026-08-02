package malgo

import (
	"context"
	"errors"
	"testing"

	"meet-sieve/internal/port"

	malgoSDK "github.com/gen2brain/malgo"
)

// TestClassifyCaptureErrorPreservesPermissionKind 验证系统音频错误可跨 adapter 边界稳定识别。
func TestClassifyCaptureErrorPreservesPermissionKind(t *testing.T) {
	t.Parallel()

	if err := classifyCaptureError(malgoSDK.ErrAccessDenied); !errors.Is(err, port.ErrAudioPermissionDenied) {
		t.Fatalf("权限错误没有映射到 Port 语义：%v", err)
	}
	if err := classifyCaptureError(malgoSDK.ErrNoDevice); !errors.Is(err, port.ErrAudioDeviceUnavailable) {
		t.Fatalf("无设备错误没有映射到 Port 语义：%v", err)
	}
}

// TestCaptureStream_ReadFramesUsesContinuousSamplePositions 验证回调 PCM 以连续采样位置交付。
func TestCaptureStream_ReadFramesUsesContinuousSamplePositions(t *testing.T) {
	stream := newCaptureStream(nil, nil)
	callback := stream.callbacks().Data
	callback(nil, []byte{1, 0, 2, 0}, 2)
	callback(nil, []byte{3, 0}, 1)

	first, err := stream.ReadFrames(context.Background())
	if err != nil || first.StartSample != 0 || len(first.PCM) != 4 {
		t.Fatalf("首帧不正确：frame=%+v err=%v", first, err)
	}
	second, err := stream.ReadFrames(context.Background())
	if err != nil || second.StartSample != 2 || len(second.PCM) != 2 {
		t.Fatalf("第二帧不正确：frame=%+v err=%v", second, err)
	}
}

// TestCaptureStream_ReadFramesStopsOnCancellation 验证 context 取消解除阻塞读取。
func TestCaptureStream_ReadFramesStopsOnCancellation(t *testing.T) {
	stream := newCaptureStream(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := stream.ReadFrames(ctx); err != context.Canceled {
		t.Fatalf("取消错误不正确：%v", err)
	}
}

// TestCaptureStream_StopIsIdempotent 验证重复 Stop 只释放一次 adapter 会话。
func TestCaptureStream_StopIsIdempotent(t *testing.T) {
	releaseCount := 0
	stream := newCaptureStream(nil, func() { releaseCount++ })
	if err := stream.Stop(context.Background()); err != nil {
		t.Fatalf("首次停止失败：%v", err)
	}
	if err := stream.Stop(context.Background()); err != nil {
		t.Fatalf("重复停止失败：%v", err)
	}
	if releaseCount != 1 {
		t.Fatalf("会话释放次数不正确：%d", releaseCount)
	}
}
