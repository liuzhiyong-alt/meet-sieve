package people_test

import (
	"testing"

	"meet-sieve/internal/domain/people"
)

// TestNormalizeName_NormalizesEquivalentUnicodeInput 验证成员与小组共用的名称键会折叠兼容字符、空白与大小写。
func TestNormalizeName_NormalizesEquivalentUnicodeInput(t *testing.T) {
	got, err := people.NormalizeName("\u3000Ｆｏｏ\u00a0\tBAR\n")
	if err != nil {
		t.Fatalf("规范化名称失败：%v", err)
	}
	if got != "foo bar" {
		t.Fatalf("规范化名称不正确：got %q want %q", got, "foo bar")
	}
}

// TestNormalizeName_RejectsBlankResult 验证只有 Unicode 空白的名称不能形成唯一键。
func TestNormalizeName_RejectsBlankResult(t *testing.T) {
	_, err := people.NormalizeName("\u3000\t\n")
	if err == nil {
		t.Fatal("空白名称必须被拒绝")
	}
}
