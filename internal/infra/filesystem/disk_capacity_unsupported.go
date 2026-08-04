//go:build !darwin && !windows

package filesystem

import "fmt"

// VolumeBytes 在当前未支持平台返回明确错误。
func VolumeBytes(_ string) (uint64, uint64, error) {
	return 0, 0, fmt.Errorf("当前平台不支持读取磁盘容量")
}
