package wails_test

import (
	"encoding/json"
	"strings"
	"testing"

	"meet-sieve/internal/port"
	wailstransport "meet-sieve/internal/transport/wails"
)

// TestAgentEventProjectionDoesNotLeakProviderInternals 验证持续事件不暴露 session、provider turn 或最终原始 JSON。
func TestAgentEventProjectionDoesNotLeakProviderInternals(t *testing.T) {
	event := port.AgentEvent{
		Type: port.AgentEventApprovalRequested, MeetingID: "meeting", SessionID: "secret-session",
		TurnID: "local-turn", ProviderTurnID: "secret-provider-turn", Revision: 2,
		FinalOutput: &port.AgentFinalOutput{JSON: []byte(`{"secret":"value"}`)},
		Approval:    &port.PendingAgentApproval{ID: "approval", Tool: "shell", Target: "文件", ParameterSummary: "修改一项", Risk: "会修改文件"},
	}
	encoded, err := json.Marshal(wailstransport.MapAgentEventDTO(event))
	if err != nil {
		t.Fatalf("序列化 Agent 事件失败：%v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"secret-session", "secret-provider-turn", `\"secret\"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Agent 事件泄漏内部字段 %q：%s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"revision":2`) || !strings.Contains(text, `"meeting_id":"meeting"`) {
		t.Fatalf("Agent 事件缺少恢复字段：%s", text)
	}
}
