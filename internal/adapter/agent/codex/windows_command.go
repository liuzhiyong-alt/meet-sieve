package codex

import (
	"fmt"
	"strings"
)

// buildWindowsBatchSpec 为 npm 生成的 .cmd/.bat 入口构造受控命令处理器计划。
func buildWindowsBatchSpec(sourcePath string, environment []string) (LaunchSpec, error) {
	commandProcessor := environmentValue(environment, "ComSpec", true)
	if commandProcessor == "" {
		systemRoot := environmentValue(environment, "SystemRoot", true)
		if systemRoot == "" {
			return LaunchSpec{}, fmt.Errorf("Windows ComSpec 和 SystemRoot 均为空")
		}
		commandProcessor = strings.TrimRight(systemRoot, `\/`) + `\System32\cmd.exe`
	}
	return LaunchSpec{
		Command: commandProcessor, PrefixArgs: []string{"/d", "/v:off", "/s", "/c"},
		Env: append([]string(nil), environment...), SourcePath: sourcePath, BatchPath: sourcePath,
	}, nil
}

// buildWindowsBatchCommandLine 对可信 batch 路径和应用固定参数执行 cmd.exe 专用引用。
func buildWindowsBatchCommandLine(batchPath string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteWindowsBatchArgument(batchPath))
	for _, argument := range args {
		parts = append(parts, quoteWindowsBatchArgument(argument))
	}
	// /s /c 要求外层再保留一对引号，内部路径与参数各自独立引用。
	return `"` + strings.Join(parts, " ") + `"`
}

// quoteWindowsBatchArgument 拒绝换行并转义 cmd.exe 会展开或终止命令的字符。
func quoteWindowsBatchArgument(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "%", "%%")
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}
