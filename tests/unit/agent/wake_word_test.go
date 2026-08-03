package agent_test

import (
	"testing"

	agent "meet-sieve/internal/domain/agent"
)

// TestNormalizeWakeWord_NormalizesAndValidatesBoundary 验证 NFKC、空白折叠和字符边界。
func TestNormalizeWakeWord_NormalizesAndValidatesBoundary(t *testing.T) {
	wake, err := agent.NormalizeWakeWord("  ＡＩ　助手  ")
	if err != nil || wake.Value != "AI 助手" || len(wake.Hash) != 64 {
		t.Fatalf("唤醒词规范化错误：wake=%#v err=%v", wake, err)
	}
	for _, invalid := range []string{"AI", "！！！", "AI\n助手", "这是一个超过十六个字符长度限制的唤醒词示例"} {
		if _, err := agent.NormalizeWakeWord(invalid); err == nil {
			t.Fatalf("非法唤醒词必须被拒绝：%q", invalid)
		}
	}
}

// TestWakeMatcher_MatchesOnlySentenceStartWithQuestion 验证句首精确匹配、分隔符和问题非空。
func TestWakeMatcher_MatchesOnlySentenceStartWithQuestion(t *testing.T) {
	wake, err := agent.NormalizeWakeWord("AI 助手")
	if err != nil {
		t.Fatal(err)
	}
	matcher := agent.NewWakeMatcher(wake)
	tests := []struct {
		text     string
		question string
	}{
		{"， AI 助手，请比较两个方案", "请比较两个方案"},
		{"AI 助手: 总结风险", "总结风险"},
		{"嗯，AI 助手总结风险", ""},
		{"我们请 AI 助手总结风险", ""},
		{"AI 助手版很好", ""},
		{"AI 助手", ""},
	}
	for _, test := range tests {
		if got := matcher.Match(test.text); got != test.question {
			t.Fatalf("匹配结果错误：text=%q got=%q want=%q", test.text, got, test.question)
		}
	}
}
