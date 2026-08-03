//go:build windows

package network

import (
	"context"
	"os/exec"
)

// resolveDefaultRoute 读取 Windows 当前 metric 最小的 IPv4 默认路由接口地址。
func resolveDefaultRoute(ctx context.Context) (string, string, bool) {
	output, err := exec.CommandContext(ctx, "route", "PRINT", "-4", "0.0.0.0").Output()
	if err != nil {
		return "", "", false
	}
	address, ok := parseWindowsDefaultInterfaceAddress(output)
	return "", address, ok
}
