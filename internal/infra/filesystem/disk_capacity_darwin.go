//go:build darwin

package filesystem

import (
	"fmt"
	"syscall"
)

// VolumeBytes 返回路径所在卷的总容量与当前可用容量。
func VolumeBytes(path string) (uint64, uint64, error) {
	nearestPath, err := nearestExistingCanonicalPath(path)
	if err != nil {
		return 0, 0, fmt.Errorf("解析磁盘容量路径失败: %w", err)
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(nearestPath, &stat); err != nil {
		return 0, 0, fmt.Errorf("读取磁盘容量失败: %w", err)
	}
	blockSize := uint64(stat.Bsize)
	return uint64(stat.Blocks) * blockSize, uint64(stat.Bavail) * blockSize, nil
}
