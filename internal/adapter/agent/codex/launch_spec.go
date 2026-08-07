package codex

import (
	"context"
	"os/exec"
)

// LaunchSpec 描述已解析的 Codex 启动命令、固定参数和子进程环境。
// SourcePath 是用户选择或 PATH 解析后的入口，用于缓存身份和错误定位。
type LaunchSpec struct {
	// Command 是最终由操作系统启动的原生可执行文件。
	Command string
	// PrefixArgs 是 Windows cmd.exe 等受控启动器的固定参数。
	PrefixArgs []string
	// Env 是检测与正式 app-server 共用的完整子进程环境。
	Env []string
	// SourcePath 是用户配置最终解析到的 Codex 入口。
	SourcePath string
	// BatchPath 非空时表示 Windows npm batch shim，由 Command 代理启动。
	BatchPath string
}

// CommandContext 使用同一启动计划创建受 context 控制的 Codex 命令。
func (spec LaunchSpec) CommandContext(ctx context.Context, args ...string) *exec.Cmd {
	command := prepareLaunchCommand(ctx, spec, args)
	if len(spec.Env) > 0 {
		command.Env = append([]string(nil), spec.Env...)
	}
	configureProcess(command)
	return command
}

// CommandWithoutContext 创建由 Codex session 生命周期显式管理的长生命周期命令。
func (spec LaunchSpec) CommandWithoutContext(args ...string) *exec.Cmd {
	return spec.CommandContext(context.Background(), args...)
}
