//go:build windows

package filesystem

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// AvailableBytes 返回 Windows 路径所在卷当前可用的字节数。
func AvailableBytes(path string) (uint64, error) {
	root := filepath.VolumeName(path)
	if root == "" {
		return 0, fmt.Errorf("无法确定 Windows 磁盘根目录")
	}
	directory, err := windows.UTF16PtrFromString(root + `\`)
	if err != nil {
		return 0, fmt.Errorf("编码 Windows 磁盘根目录失败: %w", err)
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(directory, &available, nil, nil); err != nil {
		return 0, fmt.Errorf("读取 Windows 磁盘空间失败: %w", err)
	}
	return available, nil
}
