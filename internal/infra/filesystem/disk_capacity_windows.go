//go:build windows

package filesystem

import (
	"fmt"
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// VolumeBytes 返回路径所在卷的总容量与当前可用容量。
func VolumeBytes(path string) (uint64, uint64, error) {
	nearestPath, err := nearestExistingCanonicalPath(path)
	if err != nil {
		return 0, 0, fmt.Errorf("解析磁盘容量路径失败: %w", err)
	}
	pointer, err := syscall.UTF16PtrFromString(nearestPath)
	if err != nil {
		return 0, 0, err
	}
	var available, total, totalFree uint64
	result, _, callErr := getDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(pointer)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree)))
	if result == 0 {
		return 0, 0, fmt.Errorf("读取磁盘容量失败: %w", callErr)
	}
	return total, available, nil
}
