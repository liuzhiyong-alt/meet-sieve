//go:build !darwin && !windows

package codex

import "context"

// resolveLaunchEnvironment 在其他平台保持当前进程环境，不推测未支持的桌面规则。
func resolveLaunchEnvironment(context.Context) []string {
	return currentEnvironment()
}
