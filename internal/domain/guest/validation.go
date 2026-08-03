// Package guest 定义局域网访客输入与网络选择的纯领域规则。
package guest

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"meet-sieve/internal/infra/apperr"

	"github.com/google/uuid"
)

const (
	// MaxDisplayNameCodePoints 是访客临时名称的 Unicode code point 上限。
	MaxDisplayNameCodePoints = 40
	// MaxMessageBytes 是规范化前后会议消息的 UTF-8 字节上限。
	MaxMessageBytes = 10_000
)

// NormalizeDisplayName 校验并返回去除首尾空白的访客显示名称。
func NormalizeDisplayName(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", invalidDisplayName()
	}
	normalized := strings.TrimSpace(value)
	if normalized == "" || utf8.RuneCountInString(normalized) > MaxDisplayNameCodePoints {
		return "", invalidDisplayName()
	}
	for _, character := range normalized {
		if unicode.IsControl(character) || isBidiControl(character) {
			return "", invalidDisplayName()
		}
	}
	return normalized, nil
}

// NormalizeMessage 校验消息并把 CRLF/CR 统一为 LF，不修改其他用户内容。
func NormalizeMessage(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) > MaxMessageBytes || strings.ContainsRune(value, '\x00') {
		return "", invalidMessage()
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	if len(normalized) > MaxMessageBytes || strings.TrimSpace(normalized) == "" {
		return "", invalidMessage()
	}
	return normalized, nil
}

// NormalizeLink 校验并返回无 userinfo 的绝对 HTTP/HTTPS URL。
func NormalizeLink(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", invalidLink()
	}
	normalized := strings.TrimSpace(value)
	if normalized == "" || containsControl(normalized) {
		return "", invalidLink()
	}
	parsed, err := url.Parse(normalized)
	if err != nil || !parsed.IsAbs() || parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", invalidLink()
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", invalidLink()
	}
	return normalized, nil
}

// ValidateRequestID 校验 Guest 写请求使用规范的 UUID 幂等键。
func ValidateRequestID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != strings.ToLower(value) {
		return apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("domain.guest.validate_request_id"))
	}
	return nil
}

// containsControl 判断文本是否包含控制或双向控制字符。
func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) || isBidiControl(character) {
			return true
		}
	}
	return false
}

// isBidiControl 识别可能造成显示欺骗的 Unicode 双向控制字符。
func isBidiControl(character rune) bool {
	switch character {
	case '\u061c', '\u200e', '\u200f',
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u2066', '\u2067', '\u2068', '\u2069':
		return true
	default:
		return false
	}
}

// invalidDisplayName 构建不泄漏原始输入的名称校验错误。
func invalidDisplayName() error {
	return apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("domain.guest.normalize_display_name"))
}

// invalidMessage 构建不泄漏消息正文的校验错误。
func invalidMessage() error {
	return apperr.Biz(apperr.CodeMessageInvalid, apperr.WithOp("domain.guest.normalize_message"))
}

// invalidLink 构建不记录完整 URL 的链接校验错误。
func invalidLink() error {
	return apperr.Biz(apperr.CodeLinkInvalid, apperr.WithOp("domain.guest.normalize_link"))
}
