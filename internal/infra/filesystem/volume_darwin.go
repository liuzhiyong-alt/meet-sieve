//go:build darwin

package filesystem

import (
	"fmt"
	"syscall"
)

// DetectVolume 使用 macOS statfs 检测目标路径所在卷的文件系统类型。
func DetectVolume(path CanonicalPath) (VolumeKind, error) {
	nearestExistingPath, err := nearestExistingCanonicalPath(path.String())
	if err != nil {
		return VolumeUnknown, fmt.Errorf("解析卷检测路径失败: %w", err)
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(nearestExistingPath, &stat); err != nil {
		return VolumeUnknown, fmt.Errorf("读取 macOS 卷信息失败: %w", err)
	}
	return ClassifyFilesystemType(statfsFilesystemType(stat.Fstypename)), nil
}

// statfsFilesystemType 将 C 风格文件系统名称转换为 Go 字符串。
func statfsFilesystemType(name [16]int8) string {
	bytes := make([]byte, 0, len(name))
	for _, value := range name {
		if value == 0 {
			break
		}
		bytes = append(bytes, byte(value))
	}
	return string(bytes)
}
