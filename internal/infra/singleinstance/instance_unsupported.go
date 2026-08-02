//go:build !darwin && !windows

package singleinstance

import (
	"fmt"
	"runtime"
)

// Acquire 在非目标桌面平台明确拒绝启动，避免无保护地打开工作目录或 SQLite。
func Acquire() (Outcome, *Lease, error) {
	return "", nil, fmt.Errorf("当前平台 %s 不支持 MeetSieve 单实例", runtime.GOOS)
}
