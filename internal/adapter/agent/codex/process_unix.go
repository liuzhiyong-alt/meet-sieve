//go:build !windows

package codex

import "os/exec"

// configureProcess 在 Unix 平台不增加额外进程窗口配置。
func configureProcess(_ *exec.Cmd) {}
