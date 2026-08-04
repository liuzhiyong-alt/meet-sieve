//go:build !darwin && !windows

package systemopen

import (
	"context"
	"fmt"
)

func openFile(context.Context, string) error { return fmt.Errorf("当前平台不支持打开文件") }
func revealFile(context.Context, string) error {
	return fmt.Errorf("当前平台不支持显示文件")
}
func openURL(context.Context, string) error { return fmt.Errorf("当前平台不支持打开链接") }
