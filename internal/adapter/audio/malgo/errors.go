package malgo

import (
	"errors"
	"fmt"

	"meet-sieve/internal/port"

	malgoSDK "github.com/gen2brain/malgo"
)

// classifyCaptureError 将 miniaudio 结果映射为业务可稳定识别的 Port 错误类型。
func classifyCaptureError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, malgoSDK.ErrAccessDenied) {
		return fmt.Errorf("%w: %v", port.ErrAudioPermissionDenied, err)
	}
	return fmt.Errorf("%w: %v", port.ErrAudioDeviceUnavailable, err)
}
