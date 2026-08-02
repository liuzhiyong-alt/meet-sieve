package meeting

import (
	"strings"
	"testing"
)

// TestNormalizeSubjectUsesDefault 验证空主题持久化为已确认的默认标题。
func TestNormalizeSubjectUsesDefault(t *testing.T) {
	t.Parallel()

	if got := NormalizeSubject(" \t "); got != "未命名会议" {
		t.Fatalf("空主题默认值错误：got=%q", got)
	}
}

// TestSubjectRejectsMoreThanTwoHundredCodePoints 验证主题长度按 Unicode code point 而非字节计算。
func TestSubjectRejectsMoreThanTwoHundredCodePoints(t *testing.T) {
	t.Parallel()

	valid := strings.Repeat("会", 200)
	if !IsValidSubject(valid) {
		t.Fatal("200 个 Unicode code point 应合法")
	}
	if IsValidSubject(valid + "议") {
		t.Fatal("超过 200 个 Unicode code point 必须拒绝")
	}
}

// TestNormalizeSubjectTrimsOuterWhitespace 验证有内容的主题只规范化首尾空白。
func TestNormalizeSubjectTrimsOuterWhitespace(t *testing.T) {
	t.Parallel()

	if got := NormalizeSubject("  周会  "); got != "周会" {
		t.Fatalf("主题规范化错误：got=%q", got)
	}
}

// TestIsValidMeetingNo 验证会议号符合日期、排除歧义字符的随机段和当日序号格式。
func TestIsValidMeetingNo(t *testing.T) {
	t.Parallel()

	if !IsValidMeetingNo("20260801-ABCD-01") {
		t.Fatal("合法会议号必须通过")
	}
	if IsValidMeetingNo("20260801-AOCD-01") {
		t.Fatal("随机段包含歧义字符 O 时必须拒绝")
	}
}
