//go:build !darwin && !windows

package filesystem

import (
	"fmt"
	"runtime"
)

// CurrentInstallRoot 在非目标桌面平台拒绝猜测应用安装边界。
func CurrentInstallRoot() (CanonicalPath, error) {
	return CanonicalPath{}, fmt.Errorf("当前平台 %s 不支持安装目录检测", runtime.GOOS)
}
