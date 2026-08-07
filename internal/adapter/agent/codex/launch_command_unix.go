//go:build !windows

package codex

import (
	"context"
	"os/exec"
)

// prepareLaunchCommand 在 Unix 平台直接执行原生文件或带 shebang 的脚本。
func prepareLaunchCommand(ctx context.Context, spec LaunchSpec, args []string) *exec.Cmd {
	arguments := append(append([]string(nil), spec.PrefixArgs...), args...)
	return exec.CommandContext(ctx, spec.Command, arguments...)
}
