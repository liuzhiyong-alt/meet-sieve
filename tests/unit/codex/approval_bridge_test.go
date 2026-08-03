package codex_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"meet-sieve/internal/adapter/agent/codex"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

// TestApprovalBridge_MapsAndRespondsExactlyOnce 验证原生审批映射、turn 绑定和一次性响应。
func TestApprovalBridge_MapsAndRespondsExactlyOnce(t *testing.T) {
	responder := &approvalResponder{}
	bridge := codex.NewApprovalBridge(responder)
	id := codex.StringRequestID("native-approval")
	params := json.RawMessage(`{"threadId":"thread-1","turnId":"provider-turn-1","command":"git status","cwd":"/safe/work","reason":"需要检查状态"}`)
	pending, err := bridge.Handle(codex.Message{
		ID: &id, Method: "item/commandExecution/requestApproval", Params: params,
	}, "session-1", "provider-turn-1")
	if err != nil {
		t.Fatalf("映射原生审批失败：%v", err)
	}
	if pending.Tool != "命令执行" || pending.TurnID != "provider-turn-1" || pending.ParameterSummary == "" {
		t.Fatalf("审批摘要不完整：%#v", pending)
	}
	request := port.RespondAgentApprovalRequest{
		SessionID: "session-1", TurnID: "provider-turn-1", ApprovalID: pending.ID, Decision: port.AgentApprovalAllow,
	}
	if err := bridge.Respond(context.Background(), request); err != nil {
		t.Fatalf("响应原生审批失败：%v", err)
	}
	if len(responder.responses) != 1 || responder.responses[0].result.(map[string]any)["decision"] != "accept" {
		t.Fatalf("原生响应不正确：%#v", responder.responses)
	}
	if err := bridge.Respond(context.Background(), request); !hasAppCode(err, apperr.CodeAgentApprovalExpired.ErrorCode) {
		t.Fatalf("重复响应必须返回过期错误：%v", err)
	}
}

// TestApprovalBridge_RejectsCrossTurnUnknownAndOverflow 验证跨 turn、未知 method 和每 turn 上限 fail closed。
func TestApprovalBridge_RejectsCrossTurnUnknownAndOverflow(t *testing.T) {
	responder := &approvalResponder{}
	bridge := codex.NewApprovalBridge(responder)
	unknownID := codex.IntRequestID(1)
	if _, err := bridge.Handle(codex.Message{ID: &unknownID, Method: "unknown/request", Params: []byte(`{}`)}, "session", "turn"); err == nil {
		t.Fatal("未知 ServerRequest 必须失败")
	}
	if len(responder.responses) != 1 || responder.responses[0].protocolError == nil || responder.responses[0].protocolError.Code != -32601 {
		t.Fatalf("未知 ServerRequest 未返回 -32601：%#v", responder.responses)
	}

	var first port.PendingAgentApproval
	for index := 0; index < codex.MaxPendingApprovals; index++ {
		id := codex.IntRequestID(int64(index + 2))
		pending, err := bridge.Handle(codex.Message{
			ID: &id, Method: "item/fileChange/requestApproval",
			Params: []byte(`{"threadId":"thread","turnId":"turn","itemId":"item","startedAtMs":1}`),
		}, "session", "turn")
		if err != nil {
			t.Fatalf("第 %d 个审批不应失败：%v", index+1, err)
		}
		if index == 0 {
			first = pending
		}
	}
	overflowID := codex.IntRequestID(99)
	if _, err := bridge.Handle(codex.Message{
		ID: &overflowID, Method: "item/fileChange/requestApproval",
		Params: []byte(`{"threadId":"thread","turnId":"turn","itemId":"item","startedAtMs":1}`),
	}, "session", "turn"); err == nil {
		t.Fatal("第九个 pending approval 必须失败")
	}
	crossTurn := port.RespondAgentApprovalRequest{SessionID: "session", TurnID: "other", ApprovalID: first.ID, Decision: port.AgentApprovalDecline}
	if err := bridge.Respond(context.Background(), crossTurn); !hasAppCode(err, apperr.CodeAgentApprovalExpired.ErrorCode) {
		t.Fatalf("跨 turn 响应未被拒绝：%v", err)
	}
}

// TestApprovalBridge_DeclinesAllOnTurnClose 验证取消、超时或退出时统一拒绝悬挂审批。
func TestApprovalBridge_DeclinesAllOnTurnClose(t *testing.T) {
	responder := &approvalResponder{}
	bridge := codex.NewApprovalBridge(responder)
	for index := 0; index < 2; index++ {
		id := codex.IntRequestID(int64(index + 1))
		_, err := bridge.Handle(codex.Message{
			ID: &id, Method: "item/commandExecution/requestApproval",
			Params: []byte(`{"threadId":"thread","turnId":"turn","command":"pwd","startedAtMs":1,"itemId":"item"}`),
		}, "session", "turn")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := bridge.DeclineTurn(context.Background(), "session", "turn"); err != nil {
		t.Fatalf("统一拒绝审批失败：%v", err)
	}
	if len(responder.responses) != 2 {
		t.Fatalf("悬挂审批未全部响应：%d", len(responder.responses))
	}
}

type approvalResponse struct {
	id            codex.RequestID
	result        any
	protocolError *codex.ProtocolError
}

type approvalResponder struct {
	responses []approvalResponse
	err       error
}

// Respond 记录测试中的原生响应。
func (responder *approvalResponder) Respond(id codex.RequestID, result any, protocolError *codex.ProtocolError) error {
	if responder.err != nil {
		return responder.err
	}
	responder.responses = append(responder.responses, approvalResponse{id: id, result: result, protocolError: protocolError})
	return nil
}

var _ codex.ServerRequestResponder = (*approvalResponder)(nil)

// hasAppCode 判断错误链是否包含目标稳定错误码。
func hasAppCode(err error, code string) bool {
	var appError *apperr.AppError
	return errors.As(err, &appError) && appError.ErrorCode == code
}
