//go:build windows

package codex

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
)

// prepareLaunchCommand 在 Windows 对原生 exe 直启，对 npm batch shim 使用固定 cmd.exe 参数。
func prepareLaunchCommand(ctx context.Context, spec LaunchSpec, args []string) *exec.Cmd {
	if spec.BatchPath == "" {
		arguments := append(append([]string(nil), spec.PrefixArgs...), args...)
		return exec.CommandContext(ctx, spec.Command, arguments...)
	}

	command := exec.CommandContext(ctx, spec.Command)
	commandLine := buildWindowsBatchCommandLine(spec.BatchPath, args)
	command.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: strings.Join(spec.PrefixArgs, " ") + " " + commandLine,
	}
	return command
}
