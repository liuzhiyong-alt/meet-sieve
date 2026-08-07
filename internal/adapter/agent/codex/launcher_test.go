package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/apperr"
)

// TestLauncher_UsesResolvedEnvironmentForEnvShebang 验证桌面进程原始 PATH 缺失时，解析后的环境仍能启动 Node 入口。
func TestLauncher_UsesResolvedEnvironmentForEnvShebang(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nodePath := filepath.Join(root, "node")
	codexPath := filepath.Join(root, "codex")
	writeExecutable(t, nodePath, "#!/bin/sh\nprintf 'codex-cli test\\n'\n")
	writeExecutable(t, codexPath, "#!/usr/bin/env node\n")

	launcher := newLauncher("darwin", func(context.Context) []string {
		return []string{"HOME=" + root, "PATH=" + root}
	})
	spec, err := launcher.Resolve(context.Background(), codexPath, 0)
	if err != nil {
		t.Fatalf("解析 Codex 启动计划失败：%v", err)
	}
	output, err := spec.CommandContext(context.Background(), "--version").Output()
	if err != nil {
		t.Fatalf("解析后的环境未能启动 Codex：%v", err)
	}
	if string(output) != "codex-cli test\n" {
		t.Fatalf("Codex 版本输出不正确：%q", output)
	}
}

// TestLauncher_ReportsMissingInterpreter 验证脚本解释器缺失不会伪装成协议变化。
func TestLauncher_ReportsMissingInterpreter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	codexPath := filepath.Join(root, "codex")
	writeExecutable(t, codexPath, "#!/usr/bin/env missing-node\n")

	launcher := newLauncher("darwin", func(context.Context) []string {
		return []string{"HOME=" + root, "PATH=" + root}
	})
	_, err := launcher.Resolve(context.Background(), codexPath, 0)
	if normalized := apperr.Normalize(err); normalized.ErrorCode != apperr.CodeAgentRuntimeMissing.ErrorCode {
		t.Fatalf("解释器缺失错误码不正确：got=%s err=%v", normalized.ErrorCode, err)
	}
}

// TestLauncher_UsesExplicitLocalProxy 验证 Codex 子进程只继承设置页保存的代理变量。
func TestLauncher_UsesExplicitLocalProxy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	codexPath := filepath.Join(root, "codex")
	writeExecutable(t, codexPath, "#!/bin/sh\nexit 0\n")
	launcher := newLauncher("darwin", func(context.Context) []string {
		return []string{
			"PATH=" + root,
			"HTTP_PROXY=http://unexpected:1",
			"ALL_PROXY=socks5://unexpected:2",
		}
	})
	spec, err := launcher.Resolve(context.Background(), codexPath, 65400)
	if err != nil {
		t.Fatalf("解析带代理的启动计划失败：%v", err)
	}
	assertEnvironmentValue(t, spec.Env, "HTTP_PROXY", "http://127.0.0.1:65400")
	assertEnvironmentValue(t, spec.Env, "HTTPS_PROXY", "http://127.0.0.1:65400")
	assertEnvironmentValue(t, spec.Env, "http_proxy", "http://127.0.0.1:65400")
	assertEnvironmentValue(t, spec.Env, "https_proxy", "http://127.0.0.1:65400")
	assertEnvironmentValue(t, spec.Env, "NO_PROXY", "localhost,127.0.0.1,::1")
	if environmentValue(spec.Env, "ALL_PROXY", false) != "" {
		t.Fatal("Codex 不应继承未配置的 ALL_PROXY")
	}
}

// TestLauncher_RemovesInheritedProxyWhenDisabled 验证未配置代理时不会受终端环境影响。
func TestLauncher_RemovesInheritedProxyWhenDisabled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	codexPath := filepath.Join(root, "codex")
	writeExecutable(t, codexPath, "#!/bin/sh\nexit 0\n")
	launcher := newLauncher("darwin", func(context.Context) []string {
		return []string{"PATH=" + root, "http_proxy=http://unexpected:1"}
	})
	spec, err := launcher.Resolve(context.Background(), codexPath, 0)
	if err != nil {
		t.Fatalf("解析直连启动计划失败：%v", err)
	}
	if environmentValue(spec.Env, "http_proxy", false) != "" {
		t.Fatal("未配置代理时不应继承 http_proxy")
	}
}

// TestBuildWindowsBatchSpec_UsesControlledCommandProcessor 验证 Windows npm shim 不由 CreateProcess 直接执行。
func TestBuildWindowsBatchSpec_UsesControlledCommandProcessor(t *testing.T) {
	t.Parallel()

	spec, err := buildWindowsBatchSpec(`C:\Users\Alice Doe\codex.cmd`, []string{
		`ComSpec=C:\Windows\System32\cmd.exe`,
		`PATH=C:\Windows\System32`,
	})
	if err != nil {
		t.Fatalf("构造 Windows 启动计划失败：%v", err)
	}
	if spec.Command != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("Windows 命令处理器错误：%q", spec.Command)
	}
	if len(spec.PrefixArgs) != 4 || spec.PrefixArgs[0] != "/d" || spec.PrefixArgs[1] != "/v:off" || spec.PrefixArgs[2] != "/s" || spec.PrefixArgs[3] != "/c" {
		t.Fatalf("Windows 固定参数错误：%v", spec.PrefixArgs)
	}
	commandLine := buildWindowsBatchCommandLine(spec.BatchPath, []string{"app-server", "--stdio"})
	want := `""C:\Users\Alice Doe\codex.cmd" "app-server" "--stdio""`
	if commandLine != want {
		t.Fatalf("Windows batch 命令行引用错误：got=%q want=%q", commandLine, want)
	}
}

// writeExecutable 写入测试专用可执行文件。
func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("写入测试可执行文件失败：%v", err)
	}
}

// assertEnvironmentValue 断言启动计划包含期望环境变量。
func assertEnvironmentValue(t *testing.T, environment []string, name string, want string) {
	t.Helper()
	if got := environmentValue(environment, name, false); got != want {
		t.Fatalf("环境变量 %s 不正确：got=%q want=%q", name, got, want)
	}
}
