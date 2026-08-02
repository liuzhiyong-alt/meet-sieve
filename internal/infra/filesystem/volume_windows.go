//go:build windows

package filesystem

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// DetectVolume 使用 Windows 驱动器类型识别本地、可移动和网络卷。
func DetectVolume(path CanonicalPath) (VolumeKind, error) {
	volume := filepath.VolumeName(path.String())
	if volume == "" {
		return VolumeUnknown, fmt.Errorf("无法确定 Windows 路径卷: %s", path.String())
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return VolumeUnknown, fmt.Errorf("编码 Windows 卷根失败: %w", err)
	}
	switch windows.GetDriveType(root) {
	case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE:
		return VolumeLocal, nil
	case windows.DRIVE_REMOTE:
		return VolumeNetwork, nil
	default:
		return VolumeUnknown, nil
	}
}
