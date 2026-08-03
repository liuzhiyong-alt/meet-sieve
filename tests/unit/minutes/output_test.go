package minutes_test

import (
	"strings"
	"testing"

	"meet-sieve/internal/domain/minutes"
)

// TestParseAndValidateOutput_AcceptsWhitelistedSourcesAndExactGaps 验证合法结构只引用固定事实。
func TestParseAndValidateOutput_AcceptsWhitelistedSourcesAndExactGaps(t *testing.T) {
	t.Parallel()
	context := testValidationContext()
	content := []byte(`{
		"v":1,
		"conclusions":[{"text":"采用方案 A","source_seq":[12]}],
		"topics":[{"title":"发布","content":"讨论周五发布","source_seq":[18]}],
		"tasks":[{"task":"准备发布","owner":"小刘","due":"周五","source_seq":[18]}],
		"references":[{"label":"需求文档","resource_event_seq":25}],
		"gap_notice":[{"start_sample":100,"end_sample":200,"state":"failed"}]
	}`)
	output, err := minutes.ParseAndValidateOutput(content, context)
	if err != nil {
		t.Fatalf("合法纪要被拒绝：%v", err)
	}
	markdown, err := minutes.RenderMarkdown(output, context)
	if err != nil {
		t.Fatalf("渲染纪要失败：%v", err)
	}
	text := string(markdown)
	for _, required := range []string{"## 会议结论", "## 讨论议题", "## 待办事项", "## 相关资料", "原始记录 00:00:12", "存在未完成的转写缺口"} {
		if !strings.Contains(text, required) {
			t.Fatalf("纪要缺少固定内容 %q：\n%s", required, text)
		}
	}
}

// TestParseAndValidateOutput_RejectsUnknownDuplicateOrUntrustedFacts 验证未知字段、重复 key 和非白名单来源均拒绝。
func TestParseAndValidateOutput_RejectsUnknownDuplicateOrUntrustedFacts(t *testing.T) {
	t.Parallel()
	base := testValidationContext()
	tests := []string{
		`{"v":1,"v":1,"conclusions":[],"topics":[],"tasks":[],"references":[],"gap_notice":[{"start_sample":100,"end_sample":200,"state":"failed"}]}`,
		`{"v":1,"unknown":true,"conclusions":[],"topics":[],"tasks":[],"references":[],"gap_notice":[{"start_sample":100,"end_sample":200,"state":"failed"}]}`,
		`{"v":1,"conclusions":[{"text":"AI 猜测","source_seq":[999]}],"topics":[],"tasks":[],"references":[],"gap_notice":[{"start_sample":100,"end_sample":200,"state":"failed"}]}`,
		`{"v":1,"conclusions":[],"topics":[],"tasks":[],"references":[],"gap_notice":[]}`,
	}
	for _, content := range tests {
		if _, err := minutes.ParseAndValidateOutput([]byte(content), base); err == nil {
			t.Fatalf("非法纪要必须拒绝：%s", content)
		}
	}
}

// testValidationContext 返回包含可验证 owner/due 与资料的固定事实快照。
func testValidationContext() minutes.ValidationContext {
	return minutes.ValidationContext{
		FactText:    map[int64]string{12: "团队确认采用方案 A", 18: "小刘在周五前准备发布"},
		FactAnchor:  map[int64]string{12: "00:00:12", 18: "00:00:18"},
		ResourceSeq: map[int64]struct{}{25: {}},
		GapNotice:   []minutes.GapNotice{{StartSample: 100, EndSample: 200, State: "failed"}},
	}
}
