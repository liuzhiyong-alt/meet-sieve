//go:build darwin

package network

import (
	"context"
	"os/exec"
)

// resolveDefaultRoute 读取 macOS 当前 IPv4 默认路由网卡。
func resolveDefaultRoute(ctx context.Context) (string, string, bool) {
	output, err := exec.CommandContext(ctx, "/usr/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		return "", "", false
	}
	name, ok := parseDarwinDefaultInterface(output)
	return name, "", ok
}
