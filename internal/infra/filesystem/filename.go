package filesystem

import (
	"path/filepath"
	"strings"
)

const fallbackFilename = "untitled"

// SafeFilename 移除目录信息并替换 Windows 与 macOS 都不安全的文件名字符。
func SafeFilename(value string) string {
	name := filepath.Base(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"<", "_",
		">", "_",
		":", "_",
		`"`, "_",
		"/", "_",
		`\`, "_",
		"|", "_",
		"?", "_",
		"*", "_",
	)
	name = strings.Trim(replacer.Replace(name), " .")
	if name == "" || name == "." || name == ".." {
		return fallbackFilename
	}
	return name
}
