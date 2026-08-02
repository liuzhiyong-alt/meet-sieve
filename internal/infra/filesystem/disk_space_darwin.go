//go:build darwin

package filesystem

import (
	"fmt"
	"syscall"
)

// AvailableBytes 返回路径所在卷当前可用的字节数；不存在路径按最近存在父目录判断。
func AvailableBytes(path string) (uint64, error) {
	nearestPath, err := nearestExistingCanonicalPath(path)
	if err != nil {
		return 0, fmt.Errorf("解析磁盘空间路径失败: %w", err)
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(nearestPath, &stat); err != nil {
		return 0, fmt.Errorf("读取磁盘空间失败: %w", err)
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
