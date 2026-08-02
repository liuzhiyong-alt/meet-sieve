package malgo

import (
	"errors"
	"testing"

	"meet-sieve/internal/port"
)

// TestMapCapturePermissionStatus 验证系统拒绝和受限状态不会继续打开音频设备。
func TestMapCapturePermissionStatus(t *testing.T) {
	for _, status := range []int{capturePermissionRestricted, capturePermissionDenied} {
		if err := mapCapturePermissionStatus(status); !errors.Is(err, port.ErrAudioPermissionDenied) {
			t.Fatalf("授权状态 %d 应映射为权限拒绝：%v", status, err)
		}
	}
	for _, status := range []int{capturePermissionNotDetermined, capturePermissionAuthorized} {
		if err := mapCapturePermissionStatus(status); err != nil {
			t.Fatalf("授权状态 %d 不应被提前拒绝：%v", status, err)
		}
	}
}
