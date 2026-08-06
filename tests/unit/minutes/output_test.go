package minutes_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"meet-sieve/internal/domain/minutes"
)

// TestOutputSchema_UsesCodexSupportedKeywords 验证发送给 Codex 的 schema 不包含 response format 禁用的关键字。
func TestOutputSchema_UsesCodexSupportedKeywords(t *testing.T) {
	t.Parallel()
	schema, err := minutes.OutputSchema()
	if err != nil {
		t.Fatalf("生成纪要 schema 失败：%v", err)
	}
	if bytes.Contains(schema, []byte(`"uniqueItems"`)) {
		t.Fatalf("纪要 schema 包含 Codex 不支持的 uniqueItems：%s", schema)
	}
	var document struct {
		Properties map[string]struct {
			Type string `json:"type"`
			Enum []int  `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &document); err != nil {
		t.Fatalf("解析纪要 schema 失败：%v", err)
	}
	version := document.Properties["v"]
	if version.Type != "integer" || len(version.Enum) != 1 || version.Enum[0] != 1 {
		t.Fatalf("纪要 schema 的 v 必须是固定整数 1：%s", schema)
	}
}

// TestOutputSchemaForResources_RestrictsReferences 验证资料引用只能使用本次 resource 白名单。
func TestOutputSchemaForResources_RestrictsReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		resources     map[int64]struct{}
		wantMaxItems  int
		wantSequences []int64
	}{
		{name: "没有资料", resources: map[int64]struct{}{}, wantMaxItems: 0},
		{name: "限定资料序号", resources: map[int64]struct{}{25: {}, 7: {}}, wantMaxItems: 1000, wantSequences: []int64{7, 25}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := minutes.OutputSchemaForResources(test.resources)
			if err != nil {
				t.Fatalf("生成纪要 schema 失败：%v", err)
			}
			var document referenceSchemaDocument
			if err := json.Unmarshal(schema, &document); err != nil {
				t.Fatalf("解析纪要 schema 失败：%v", err)
			}
			references := document.Properties.References
			if references.MaxItems != test.wantMaxItems {
				t.Fatalf("资料数量约束错误：got %d, want %d", references.MaxItems, test.wantMaxItems)
			}
			got := references.Items.Properties.ResourceEventSeq.Enum
			if len(got) != len(test.wantSequences) || !equalSequences(got, test.wantSequences) {
				t.Fatalf("资料序号约束错误：got %v, want %v", got, test.wantSequences)
			}
		})
	}
}

type referenceSchemaDocument struct {
	Properties struct {
		References struct {
			MaxItems int `json:"maxItems"`
			Items    struct {
				Properties struct {
					ResourceEventSeq struct {
						Enum []int64 `json:"enum"`
					} `json:"resource_event_seq"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"references"`
	} `json:"properties"`
}

// equalSequences 比较已按稳定顺序输出的资料序号。
func equalSequences(left []int64, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

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
		`{"v":1,"conclusions":[{"text":"重复来源","source_seq":[12,12]}],"topics":[],"tasks":[],"references":[],"gap_notice":[{"start_sample":100,"end_sample":200,"state":"failed"}]}`,
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
