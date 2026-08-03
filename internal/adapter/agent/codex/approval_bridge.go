package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

const (
	// MaxPendingApprovals 是单 turn 可悬挂的原生审批硬上限。
	MaxPendingApprovals  = 8
	approvalSummaryLimit = 240
)

// ServerRequestResponder 隔离 ApprovalBridge 所需的 JSON-RPC 响应能力。
type ServerRequestResponder interface {
	Respond(id RequestID, result any, protocolError *ProtocolError) error
}

type approvalEntry struct {
	nativeID  RequestID
	sessionID string
	turnID    string
	method    string
	params    approvalParams
}

type approvalParams struct {
	ThreadID    string          `json:"threadId"`
	TurnID      string          `json:"turnId"`
	Command     any             `json:"command"`
	CWD         string          `json:"cwd"`
	GrantRoot   string          `json:"grantRoot"`
	Reason      string          `json:"reason"`
	ServerName  string          `json:"serverName"`
	Permissions json.RawMessage `json:"permissions"`
	FileChanges json.RawMessage `json:"fileChanges"`
}

// ApprovalBridge 只在内存中绑定原生 request ID、session 和当前 turn。
type ApprovalBridge struct {
	responder ServerRequestResponder
	mu        sync.Mutex
	pending   map[string]approvalEntry
}

// NewApprovalBridge 创建不持久化审批正文的原生审批桥。
func NewApprovalBridge(responder ServerRequestResponder) *ApprovalBridge {
	return &ApprovalBridge{responder: responder, pending: make(map[string]approvalEntry)}
}

// Handle 校验并转换一个 app-server ServerRequest；未知能力立即安全拒绝。
func (bridge *ApprovalBridge) Handle(message Message, sessionID string, activeTurnID string) (port.PendingAgentApproval, error) {
	if bridge == nil || bridge.responder == nil || message.ID == nil || sessionID == "" || activeTurnID == "" {
		return port.PendingAgentApproval{}, approvalUnsupported("原生审批参数无效")
	}
	if !isSupportedApprovalMethod(message.Method) {
		_ = bridge.responder.Respond(*message.ID, nil, &ProtocolError{Code: -32601, Message: "Method not supported"})
		return port.PendingAgentApproval{}, approvalUnsupported("未登记 ServerRequest")
	}
	var params approvalParams
	if err := json.Unmarshal(message.Params, &params); err != nil {
		_ = bridge.declineNative(*message.ID, message.Method, params)
		return port.PendingAgentApproval{}, approvalUnsupported("审批参数无法解析")
	}
	if params.TurnID != "" && params.TurnID != activeTurnID {
		_ = bridge.declineNative(*message.ID, message.Method, params)
		return port.PendingAgentApproval{}, approvalUnsupported("审批不属于当前 turn")
	}
	if message.Method == "mcpServer/elicitation/request" {
		_ = bridge.responder.Respond(*message.ID, map[string]any{"action": "decline"}, nil)
		return port.PendingAgentApproval{}, approvalUnsupported("MCP 结构化输入暂不能安全呈现")
	}

	approvalID := buildApprovalID(sessionID, activeTurnID, *message.ID)
	entry := approvalEntry{nativeID: *message.ID, sessionID: sessionID, turnID: activeTurnID, method: message.Method, params: params}
	bridge.mu.Lock()
	if bridge.countTurnLocked(sessionID, activeTurnID) >= MaxPendingApprovals {
		bridge.mu.Unlock()
		_ = bridge.declineNative(*message.ID, message.Method, params)
		return port.PendingAgentApproval{}, approvalUnsupported("单 turn 待审批数量超过上限")
	}
	if _, exists := bridge.pending[approvalID]; exists {
		bridge.mu.Unlock()
		_ = bridge.declineNative(*message.ID, message.Method, params)
		return port.PendingAgentApproval{}, approvalUnsupported("重复原生审批 request ID")
	}
	bridge.pending[approvalID] = entry
	bridge.mu.Unlock()
	return buildPendingApproval(approvalID, activeTurnID, message.Method, params), nil
}

// Respond 校验主持人响应的 session/turn/approval 身份，并只发送一次原生结果。
func (bridge *ApprovalBridge) Respond(ctx context.Context, request port.RespondAgentApprovalRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if bridge == nil || !request.Decision.Valid() {
		return approvalExpired()
	}
	bridge.mu.Lock()
	entry, exists := bridge.pending[request.ApprovalID]
	if exists && (entry.sessionID != request.SessionID || entry.turnID != request.TurnID) {
		exists = false
	}
	if exists {
		delete(bridge.pending, request.ApprovalID)
	}
	bridge.mu.Unlock()
	if !exists {
		return approvalExpired()
	}
	if request.Decision == port.AgentApprovalDecline {
		return bridge.declineNative(entry.nativeID, entry.method, entry.params)
	}
	result, err := allowResult(entry.method, entry.params)
	if err != nil {
		_ = bridge.declineNative(entry.nativeID, entry.method, entry.params)
		return err
	}
	if err := bridge.responder.Respond(entry.nativeID, result, nil); err != nil {
		return approvalUnsupported("写回原生审批失败")
	}
	return nil
}

// DeclineTurn 在取消、超时或进程退出时拒绝并移除指定 turn 的全部审批。
func (bridge *ApprovalBridge) DeclineTurn(ctx context.Context, sessionID string, turnID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bridge.mu.Lock()
	entries := make([]approvalEntry, 0)
	for id, entry := range bridge.pending {
		if entry.sessionID == sessionID && entry.turnID == turnID {
			entries = append(entries, entry)
			delete(bridge.pending, id)
		}
	}
	bridge.mu.Unlock()
	for _, entry := range entries {
		if err := bridge.declineNative(entry.nativeID, entry.method, entry.params); err != nil {
			return err
		}
	}
	return nil
}

// Pending 返回当前 turn 可供主持人页面恢复的审批摘要。
func (bridge *ApprovalBridge) Pending(sessionID string, turnID string) []port.PendingAgentApproval {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	result := make([]port.PendingAgentApproval, 0)
	for id, entry := range bridge.pending {
		if entry.sessionID == sessionID && entry.turnID == turnID {
			result = append(result, buildPendingApproval(id, turnID, entry.method, entry.params))
		}
	}
	return result
}

// declineNative 根据原生 response schema 返回单次拒绝。
func (bridge *ApprovalBridge) declineNative(id RequestID, method string, params approvalParams) error {
	var result any
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		result = map[string]any{"decision": "decline"}
	case "execCommandApproval", "applyPatchApproval":
		result = map[string]any{"decision": map[string]any{"denied": map[string]string{"rejection": "用户拒绝"}}}
	case "mcpServer/elicitation/request":
		result = map[string]any{"action": "decline"}
	default:
		return bridge.responder.Respond(id, nil, &ProtocolError{Code: -32001, Message: "User declined"})
	}
	return bridge.responder.Respond(id, result, nil)
}

// allowResult 只返回本次允许，不使用 session 级授权缓存。
func allowResult(method string, params approvalParams) (any, error) {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return map[string]any{"decision": "accept"}, nil
	case "execCommandApproval", "applyPatchApproval":
		return map[string]any{"decision": "approved"}, nil
	case "item/permissions/requestApproval":
		if len(params.Permissions) == 0 || string(params.Permissions) == "null" {
			return nil, approvalUnsupported("权限审批缺少 permissions")
		}
		var permissions any
		if err := json.Unmarshal(params.Permissions, &permissions); err != nil {
			return nil, approvalUnsupported("权限审批结构无效")
		}
		return map[string]any{"permissions": permissions, "scope": "turn"}, nil
	default:
		return nil, approvalUnsupported("审批 method 无允许映射")
	}
}

// countTurnLocked 统计指定 turn 的悬挂审批，调用方必须持锁。
func (bridge *ApprovalBridge) countTurnLocked(sessionID string, turnID string) int {
	count := 0
	for _, entry := range bridge.pending {
		if entry.sessionID == sessionID && entry.turnID == turnID {
			count++
		}
	}
	return count
}

// buildPendingApproval 只构造 UI 所需摘要，不暴露原始参数。
func buildPendingApproval(id string, turnID string, method string, params approvalParams) port.PendingAgentApproval {
	tool := map[string]string{
		"item/commandExecution/requestApproval": "命令执行", "item/fileChange/requestApproval": "修改文件",
		"execCommandApproval": "命令执行", "applyPatchApproval": "修改文件",
		"item/permissions/requestApproval": "权限请求",
	}[method]
	target := firstNonEmpty(params.CWD, params.GrantRoot)
	summary := summarizeValue(params.Command)
	if summary == "" && len(params.FileChanges) > 0 {
		summary = "Codex 请求修改文件"
	}
	if summary == "" && method == "item/permissions/requestApproval" {
		summary = "Codex 请求本轮额外权限"
	}
	return port.PendingAgentApproval{
		ID: id, TurnID: turnID, Tool: tool, Target: truncateSummary(target),
		ParameterSummary: truncateSummary(summary), Risk: truncateSummary(params.Reason),
	}
}

// buildApprovalID 生成不泄漏原生 request ID 的稳定内存身份。
func buildApprovalID(sessionID string, turnID string, nativeID RequestID) string {
	digest := sha256.Sum256([]byte(sessionID + "\x00" + turnID + "\x00" + nativeID.Key()))
	return hex.EncodeToString(digest[:16])
}

// isSupportedApprovalMethod 只登记当前 runtime schema 已冻结的审批类请求。
func isSupportedApprovalMethod(method string) bool {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval",
		"execCommandApproval", "applyPatchApproval", "item/permissions/requestApproval",
		"mcpServer/elicitation/request":
		return true
	default:
		return false
	}
}

// summarizeValue 把 string 或 argv 转成有界展示摘要。
func summarizeValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

// truncateSummary 限制审批摘要，避免原生参数撑爆 UI 事件。
func truncateSummary(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > approvalSummaryLimit {
		return string(runes[:approvalSummaryLimit]) + "…"
	}
	return string(runes)
}

// firstNonEmpty 返回首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// approvalUnsupported 返回不包含原始参数的安全依赖错误。
func approvalUnsupported(reason string) error {
	return apperr.Dependency(
		apperr.CodeAgentApprovalUnsupported,
		fmt.Errorf("%s", reason),
		apperr.WithOp("agent.approval.bridge"),
	)
}

// approvalExpired 返回重复、过期或跨 turn 的稳定业务错误。
func approvalExpired() error {
	return apperr.Biz(apperr.CodeAgentApprovalExpired, apperr.WithOp("agent.approval.respond"))
}
