//go:build darwin

package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const shellPathMarker = "__MEETSIEVE_PATH__"

// resolveLaunchEnvironment 合并 Finder 环境、登录 shell PATH 和稳定的 macOS 命令目录。
func resolveLaunchEnvironment(ctx context.Context) []string {
	environment := currentEnvironment()
	currentPath := environmentValue(environment, "PATH", false)
	shellPath := queryLoginShellPath(ctx, environment)
	homeDirectory, _ := os.UserHomeDir()
	return mergeEnvironmentPath(
		environment,
		false,
		shellPath,
		currentPath,
		filepath.Join(homeDirectory, ".local", "share", "mise", "shims"),
		filepath.Join(homeDirectory, ".volta", "bin"),
		filepath.Join(homeDirectory, ".local", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	)
}

// queryLoginShellPath 只执行固定命令读取 PATH；失败或超时由稳定目录回退覆盖。
func queryLoginShellPath(parent context.Context, environment []string) string {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/zsh", "-lic", `printf '__MEETSIEVE_PATH__%s\n' "$PATH"`)
	command.Env = environment
	output, err := command.Output()
	if err != nil || len(output) > 64*1024 {
		return ""
	}
	text := string(output)
	index := strings.LastIndex(text, shellPathMarker)
	if index < 0 {
		return ""
	}
	value := text[index+len(shellPathMarker):]
	if newline := strings.IndexByte(value, '\n'); newline >= 0 {
		value = value[:newline]
	}
	return strings.TrimSpace(value)
}
