//go:build !darwin && !windows

package filesystem

import (
	"fmt"
	"runtime"
)

// AvailableBytes 在非目标桌面平台拒绝猜测磁盘空间。
func AvailableBytes(string) (uint64, error) {
	return 0, fmt.Errorf("当前平台 %s 不支持磁盘空间检测", runtime.GOOS)
}
