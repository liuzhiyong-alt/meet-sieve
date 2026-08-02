package logger

import "regexp"

var (
	credentialPattern = regexp.MustCompile(`(?i)(password|passwd|token|secret|authorization|api[_-]?key)(\s*[:=]\s*)([^\s,;]+)`)
	unixUserPath      = regexp.MustCompile(`/(Users|home)/[^/\s]+(?:/[^\s,;:]+)+`)
	windowsUserPath   = regexp.MustCompile(`(?i)[a-z]:\\Users\\[^\\\s]+(?:\\[^\s,;:]+)+`)
)

// redactSensitive 对允许进入日志的文本执行凭证和用户路径脱敏。
func redactSensitive(value string) string {
	redacted := credentialPattern.ReplaceAllString(value, `${1}${2}[REDACTED]`)
	redacted = unixUserPath.ReplaceAllString(redacted, "[PATH]")
	return windowsUserPath.ReplaceAllString(redacted, "[PATH]")
}
