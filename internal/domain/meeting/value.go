package meeting

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const defaultSubject = "未命名会议"

var meetingNoPattern = regexp.MustCompile(`^[0-9]{8}-[A-HJ-NP-Z2-9]{4}-[0-9]{2,4}$`)

// NormalizeSubject 去除主题首尾空白；空主题使用已确认的默认标题。
func NormalizeSubject(subject string) string {
	normalized := strings.TrimSpace(subject)
	if normalized == "" {
		return defaultSubject
	}
	return normalized
}

// IsValidSubject 判断规范化后的主题是否是不超过 200 个 Unicode code point 的合法文本。
func IsValidSubject(subject string) bool {
	normalized := NormalizeSubject(subject)
	return utf8.ValidString(normalized) && utf8.RuneCountInString(normalized) <= 200
}

// IsValidMeetingNo 判断会议号是否符合开始前可编辑的固定格式。
func IsValidMeetingNo(meetingNo string) bool {
	return meetingNoPattern.MatchString(meetingNo)
}
