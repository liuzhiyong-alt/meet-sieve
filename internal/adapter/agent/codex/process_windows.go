//go:build windows

package codex

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// configureProcess 保证 Windows Codex 子进程不创建可见控制台窗口。
func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
