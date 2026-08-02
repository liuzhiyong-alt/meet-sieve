package malgo

import "meet-sieve/internal/port"

const (
	capturePermissionNotDetermined = iota
	capturePermissionRestricted
	capturePermissionDenied
	capturePermissionAuthorized
)

// mapCapturePermissionStatus 将 AVFoundation 状态转换为跨平台 Port 错误。
func mapCapturePermissionStatus(status int) error {
	if status == capturePermissionRestricted || status == capturePermissionDenied {
		return port.ErrAudioPermissionDenied
	}
	return nil
}
