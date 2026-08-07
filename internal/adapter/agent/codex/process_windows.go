//go:build windows

package codex

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// configureProcess 保证 Windows Codex 子进程不创建可见控制台窗口。
func configureProcess(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	// batch 启动计划可能已经设置 CmdLine，这里只补充隐藏窗口属性。
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNoWindow
}
