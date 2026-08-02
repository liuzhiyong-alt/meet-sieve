//go:build !darwin

package malgo

// ensureCapturePermission 在非 macOS 平台继续使用 miniaudio 的原生权限错误。
func ensureCapturePermission() error {
	return nil
}
