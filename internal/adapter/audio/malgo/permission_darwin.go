//go:build darwin

package malgo

/*
#cgo LDFLAGS: -framework AVFoundation -framework Foundation
int meetsieve_audio_authorization_status(void);
*/
import "C"

// ensureCapturePermission 在打开 CoreAudio 前读取系统麦克风授权，避免驱动层漏报拒绝。
func ensureCapturePermission() error {
	return mapCapturePermissionStatus(int(C.meetsieve_audio_authorization_status()))
}
