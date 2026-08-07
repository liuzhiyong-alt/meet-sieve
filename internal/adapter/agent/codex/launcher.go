package codex

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"meet-sieve/internal/infra/apperr"
)

type environmentResolver func(context.Context) []string

// Launcher 把用户配置的 Codex 入口解析为跨检测和正式会话复用的启动计划。
type Launcher struct {
	goos        string
	environment environmentResolver
}

// NewLauncher 创建使用当前操作系统桌面环境解析策略的 Codex 启动器。
func NewLauncher() *Launcher {
	return newLauncher(runtime.GOOS, resolveLaunchEnvironment)
}

// newLauncher 创建可注入环境的启动器，供边界测试复现 Finder 和 Explorer 环境。
func newLauncher(goos string, resolver environmentResolver) *Launcher {
	return &Launcher{goos: goos, environment: resolver}
}

// Resolve 解析入口、脚本运行时与本机代理，并生成检测和正式会话共用的启动计划。
func (launcher *Launcher) Resolve(ctx context.Context, configured string, proxyPort int) (LaunchSpec, error) {
	if launcher == nil || launcher.environment == nil || strings.TrimSpace(configured) == "" {
		return LaunchSpec{}, executableInvalid()
	}
	environment := applyLocalProxyEnvironment(launcher.environment(ctx), proxyPort)
	sourcePath, err := resolveCommandPath(configured, environment, launcher.goos)
	if err != nil {
		return LaunchSpec{}, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.launch.executable"))
	}
	if launcher.goos == "windows" {
		return resolveWindowsSpec(sourcePath, environment)
	}
	if err := validateScriptInterpreter(sourcePath, environment); err != nil {
		return LaunchSpec{}, err
	}
	return LaunchSpec{Command: sourcePath, Env: environment, SourcePath: sourcePath}, nil
}

// resolveCommandPath 使用为桌面应用补全后的 PATH 定位单个 Codex 入口。
func resolveCommandPath(command string, environment []string, goos string) (string, error) {
	trimmed := strings.TrimSpace(command)
	if filepath.IsAbs(trimmed) || strings.ContainsAny(trimmed, `/\`) {
		return validateCommandFile(trimmed, goos)
	}
	extensions := []string{""}
	if goos == "windows" && filepath.Ext(trimmed) == "" {
		extensions = windowsPathExtensions(environment)
	}
	for _, directory := range filepath.SplitList(environmentValue(environment, "PATH", goos == "windows")) {
		for _, extension := range extensions {
			if resolved, err := validateCommandFile(filepath.Join(directory, trimmed+extension), goos); err == nil {
				return resolved, nil
			}
		}
	}
	return "", os.ErrNotExist
}

// validateCommandFile 只接受普通文件；Unix 还要求至少一个执行位。
func validateCommandFile(path string, goos string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	if goos != "windows" && info.Mode()&0o111 == 0 {
		return "", os.ErrPermission
	}
	return absolute, nil
}

// validateScriptInterpreter 预检 shebang 解释器，避免把 Node 缺失误判为协议变化。
func validateScriptInterpreter(sourcePath string, environment []string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.launch.read"))
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, 4096)
	line, _ := reader.ReadString('\n')
	if !strings.HasPrefix(line, "#!") {
		return nil
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
	if len(fields) == 0 {
		return runtimeMissing(fmt.Errorf("Codex shebang 为空"), "agent.launch.shebang")
	}
	if fields[0] == "/usr/bin/env" {
		if len(fields) < 2 || strings.HasPrefix(fields[1], "-") {
			return runtimeMissing(fmt.Errorf("Codex env shebang 不受支持"), "agent.launch.shebang")
		}
		if _, err := resolveCommandPath(fields[1], environment, runtime.GOOS); err != nil {
			return runtimeMissing(err, "agent.launch.runtime")
		}
		return nil
	}
	if _, err := os.Stat(fields[0]); err != nil {
		return runtimeMissing(err, "agent.launch.runtime")
	}
	return nil
}

// resolveWindowsSpec 区分原生 exe 与 npm batch shim，并预检常见 Node 依赖。
func resolveWindowsSpec(sourcePath string, environment []string) (LaunchSpec, error) {
	extension := strings.ToLower(filepath.Ext(sourcePath))
	switch extension {
	case ".exe", ".com":
		return LaunchSpec{Command: sourcePath, Env: environment, SourcePath: sourcePath}, nil
	case ".cmd", ".bat":
		if batchReferencesNode(sourcePath) {
			if _, err := resolveCommandPath("node", environment, "windows"); err != nil {
				return LaunchSpec{}, runtimeMissing(err, "agent.launch.runtime")
			}
		}
		spec, err := buildWindowsBatchSpec(sourcePath, environment)
		if err != nil {
			return LaunchSpec{}, apperr.Dependency(apperr.CodeAgentLaunchFailed, err, apperr.WithOp("agent.launch.windows"))
		}
		return spec, nil
	default:
		return LaunchSpec{}, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.launch.extension"))
	}
}

// batchReferencesNode 只读取有限前缀，用于识别 npm shim 的 Node 运行时依赖。
func batchReferencesNode(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(content) > 64*1024 {
		content = content[:64*1024]
	}
	lower := strings.ToLower(string(content))
	return strings.Contains(lower, "node.exe") || strings.Contains(lower, " node ") || strings.Contains(lower, `\node`)
}

// windowsPathExtensions 返回安全、稳定的 Windows 可执行扩展名搜索顺序。
func windowsPathExtensions(environment []string) []string {
	configured := environmentValue(environment, "PATHEXT", true)
	if configured == "" {
		configured = ".COM;.EXE;.BAT;.CMD"
	}
	result := make([]string, 0, 4)
	for _, extension := range strings.Split(configured, ";") {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension == ".com" || extension == ".exe" || extension == ".bat" || extension == ".cmd" {
			result = append(result, extension)
		}
	}
	return result
}

// environmentValue 按平台大小写规则读取环境变量。
func environmentValue(environment []string, name string, caseInsensitive bool) string {
	for _, item := range environment {
		key, value, found := strings.Cut(item, "=")
		if found && ((!caseInsensitive && key == name) || (caseInsensitive && strings.EqualFold(key, name))) {
			return value
		}
	}
	return ""
}

// runtimeMissing 保留内部原因并返回稳定的运行时缺失提示。
func runtimeMissing(cause error, operation string) error {
	return apperr.Dependency(apperr.CodeAgentRuntimeMissing, cause, apperr.WithOp(operation))
}
