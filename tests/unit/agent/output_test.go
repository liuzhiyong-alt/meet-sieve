package agent_test

import (
	"encoding/json"
	"strings"
	"testing"

	agent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/port"
)

// TestOutputSchemaWithReferences_ConstrainsReferencesToCurrentAllowlist 验证动态 Schema 在生成阶段限制引用来源。
func TestOutputSchemaWithReferences_ConstrainsReferencesToCurrentAllowlist(t *testing.T) {
	schema, err := agent.OutputSchemaWithReferences(port.AgentTurnInitialize, agent.ReferenceAllowlist{
		Sequences: map[int64]struct{}{12: {}, 3: {}},
		URLs:      map[string]struct{}{"https://example.com/doc": {}},
		Resources: map[string]struct{}{"attachments/report.pdf": {}},
	})
	if err != nil {
		t.Fatalf("生成动态输出 Schema 失败：%v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(schema, &document); err != nil {
		t.Fatalf("动态输出 Schema 不是合法 JSON：%v", err)
	}
	properties := document["properties"].(map[string]any)
	snapshot := properties["snapshot"].(map[string]any)
	snapshotProperties := snapshot["properties"].(map[string]any)
	references := snapshotProperties["references"].(map[string]any)
	items := references["items"].(map[string]any)
	values := items["enum"].([]any)
	want := []any{"attachments/report.pdf", "https://example.com/doc", "seq:12", "seq:3"}
	if len(values) != len(want) {
		t.Fatalf("引用枚举数量错误：got=%v want=%v", values, want)
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("引用枚举必须稳定排序：got=%v want=%v", values, want)
		}
	}

	emptySchema, err := agent.OutputSchemaWithReferences(port.AgentTurnInitialize, agent.ReferenceAllowlist{})
	if err != nil {
		t.Fatalf("生成空引用 Schema 失败：%v", err)
	}
	if !strings.Contains(string(emptySchema), `"maxItems":0`) {
		t.Fatalf("没有白名单时 references 必须只能为空：%s", emptySchema)
	}
}

// TestValidateOutput_AcceptsAnswerAndCanonicalSnapshot 验证回答、引用白名单和规范快照摘要。
func TestValidateOutput_AcceptsAnswerAndCanonicalSnapshot(t *testing.T) {
	content := []byte(`{
		"answer":"建议先验证录音链路。",
		"snapshot":{
			"current_topics":["录音"],"confirmed_decisions":[],"business_rules":[],
			"disagreements":[],"open_questions":[],
			"references":["seq:7","https://example.com/a","resources/report.pdf"]
		}
	}`)
	validated, err := agent.ValidateOutput(port.AgentTurnAnswer, content, agent.ReferenceAllowlist{
		Sequences: map[int64]struct{}{7: {}},
		URLs:      map[string]struct{}{"https://example.com/a": {}},
		Resources: map[string]struct{}{"resources/report.pdf": {}},
	})
	if err != nil {
		t.Fatalf("合法回答未通过校验：%v", err)
	}
	if validated.Answer != "建议先验证录音链路。" || len(validated.SnapshotJSON) == 0 || len(validated.SnapshotSHA256) != 64 {
		t.Fatalf("结构化输出结果不完整：%#v", validated)
	}
}

// TestValidateOutput_RejectsUnknownUnsafeAndOutOfScopeValues 验证未知字段、敏感内容和越界引用 fail closed。
func TestValidateOutput_RejectsUnknownUnsafeAndOutOfScopeValues(t *testing.T) {
	validSnapshot := `"snapshot":{"current_topics":[],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":[]}`
	tests := []string{
		`{"answer":"ok",` + validSnapshot + `,"reasoning":"secret"}`,
		`{"answer":"/Users/liu/private",` + validSnapshot + `}`,
		`{"answer":"token=sk-secret",` + validSnapshot + `}`,
		`{"answer":"ok","snapshot":{"current_topics":[],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":["seq:8"]}}`,
	}
	for _, content := range tests {
		if _, err := agent.ValidateOutput(port.AgentTurnAnswer, []byte(content), agent.ReferenceAllowlist{Sequences: map[int64]struct{}{7: {}}}); err == nil {
			t.Fatalf("危险或越界输出必须被拒绝：%s", content)
		}
	}
}

// TestValidateOutput_EnforcesTurnShapeAndLimits 验证 ingest 无回答字段且数组、单项和回答上限有效。
func TestValidateOutput_EnforcesTurnShapeAndLimits(t *testing.T) {
	snapshotOnly := []byte(`{"snapshot":{"current_topics":[],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":[]}}`)
	if _, err := agent.ValidateOutput(port.AgentTurnIngest, snapshotOnly, agent.ReferenceAllowlist{}); err != nil {
		t.Fatalf("合法 ingest snapshot 未通过：%v", err)
	}
	if _, err := agent.ValidateOutput(port.AgentTurnAnswer, snapshotOnly, agent.ReferenceAllowlist{}); err == nil {
		t.Fatal("answer turn 缺少回答必须失败")
	}
	tooLongAnswer := `{"answer":"` + strings.Repeat("答", agent.MaxAnswerRunes+1) + `",` + strings.TrimPrefix(string(snapshotOnly), "{")
	if _, err := agent.ValidateOutput(port.AgentTurnAnswer, []byte(tooLongAnswer), agent.ReferenceAllowlist{}); err == nil {
		t.Fatal("超长回答必须失败")
	}
	items := make([]string, agent.MaxSnapshotItems+1)
	for index := range items {
		items[index] = `"主题"`
	}
	overItems := `{"snapshot":{"current_topics":[` + strings.Join(items, ",") + `],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":[]}}`
	if _, err := agent.ValidateOutput(port.AgentTurnIngest, []byte(overItems), agent.ReferenceAllowlist{}); err == nil {
		t.Fatal("超过 50 项的快照数组必须失败")
	}
}

// TestAnswerDeltaParser_EmitsOnlyDecodedAnswer 验证分片与转义不会泄漏 snapshot 或原始 JSON。
func TestAnswerDeltaParser_EmitsOnlyDecodedAnswer(t *testing.T) {
	parser := agent.NewAnswerDeltaParser()
	parts := []string{`{"snapshot":{"current_topics":[]},"answer":"你`, `好\n世`, `界","other":"secret"}`}
	wants := []string{"你", "好\n世", "界"}
	var output strings.Builder
	for index, part := range parts {
		delta := parser.Push([]byte(part))
		if delta != wants[index] {
			t.Fatalf("第 %d 个增量错误：got %q want %q", index+1, delta, wants[index])
		}
		output.WriteString(delta)
	}
	if output.String() != "你好\n世界" {
		t.Fatalf("增量回答解析错误：%q", output.String())
	}
}
