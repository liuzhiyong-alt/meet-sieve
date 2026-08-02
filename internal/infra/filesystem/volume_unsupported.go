//go:build !darwin && !windows

package filesystem

import (
	"fmt"
	"runtime"
)

// DetectVolume 在非目标桌面平台拒绝猜测卷类型。
func DetectVolume(CanonicalPath) (VolumeKind, error) {
	return VolumeUnknown, fmt.Errorf("当前平台 %s 不支持卷类型检测", runtime.GOOS)
}
