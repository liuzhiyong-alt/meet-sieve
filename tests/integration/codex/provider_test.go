//go:build !windows

package codex_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/adapter/agent/codex"
	agentdomain "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/port"
)

// TestProvider_LongLivedTurnAndApproval 验证真实子进程上的握手、thread、turn、审批与关闭顺序。
func TestProvider_LongLivedTurnAndApproval(t *testing.T) {
	executable := providerHelperExecutable(t)
	schemaContent := []byte(`{"type":"object"}`)
	digest := sha256.Sum256(schemaContent)
	verifier := codex.NewSchemaVerifier(providerSchemaRunner{content: schemaContent}, codex.SchemaContract{
		Version: "test", Files: map[string]string{"required.json": hex.EncodeToString(digest[:])},
	}, t.TempDir())
	provider := codex.NewProviderWithVerifier(executable, verifier)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := provider.StartSession(ctx, port.StartAgentSessionRequest{
		WorkingDirectory: t.TempDir(), Prompt: "固定会议助手边界",
	})
	if err != nil {
		t.Fatalf("启动长生命周期 session 失败：%v", err)
	}
	schema, err := agentdomain.OutputSchema(port.AgentTurnAnswer)
	if err != nil {
		t.Fatal(err)
	}
	events, err := provider.RunTurn(ctx, port.RunAgentTurnRequest{
		SessionID: session.ID, TurnID: "local-turn", Kind: port.AgentTurnAnswer,
		Input: "问题", OutputSchema: schema, Deadline: time.Now().Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("提交 turn 失败：%v", err)
	}

	var types []port.AgentEventType
	for event := range events {
		types = append(types, event.Type)
		if event.Type == port.AgentEventApprovalRequested {
			if event.Approval == nil || event.Approval.TurnID != "provider-turn" {
				t.Fatalf("审批未绑定 provider turn：%#v", event)
			}
			if err := provider.RespondApproval(ctx, port.RespondAgentApprovalRequest{
				SessionID: session.ID, TurnID: event.Approval.TurnID,
				ApprovalID: event.Approval.ID, Decision: port.AgentApprovalAllow,
			}); err != nil {
				t.Fatalf("允许原生审批失败：%v", err)
			}
		}
	}
	if !containsEvent(types, port.AgentEventTurnStarted) || !containsEvent(types, port.AgentEventApprovalRequested) ||
		!containsEvent(types, port.AgentEventAnswerDelta) || !containsEvent(types, port.AgentEventFinalOutput) ||
		!containsEvent(types, port.AgentEventCompleted) {
		t.Fatalf("turn 事件不完整：%v", types)
	}
	if err := provider.CloseSession(ctx, session.ID); err != nil {
		t.Fatalf("关闭长生命周期 session 失败：%v", err)
	}
}

// TestProviderHelperProcess 模拟严格 JSONL app-server，仅由包装脚本子进程执行。
func TestProviderHelperProcess(t *testing.T) {
	if !containsArgument(os.Args, "app-server") {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		method, _ := request["method"].(string)
		id, hasID := request["id"]
		switch method {
		case "initialize":
			_ = encoder.Encode(map[string]any{"id": id, "result": map[string]any{
				"codexHome": "/redacted", "platformFamily": "unix", "platformOs": "macos", "userAgent": "codex-test",
			}})
		case "initialized":
		case "account/read":
			_ = encoder.Encode(map[string]any{"id": id, "result": map[string]any{"account": map[string]string{"type": "apiKey"}, "requiresOpenaiAuth": false}})
		case "thread/start":
			_ = encoder.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]string{"id": "provider-thread"}}})
		case "turn/start":
			_ = encoder.Encode(map[string]any{"id": id, "result": map[string]any{"turn": map[string]string{"id": "provider-turn"}}})
			_ = encoder.Encode(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": "provider-thread", "turn": map[string]string{"id": "provider-turn", "status": "inProgress"}}})
			_ = encoder.Encode(map[string]any{"id": "approval-native", "method": "item/commandExecution/requestApproval", "params": map[string]any{
				"threadId": "provider-thread", "turnId": "provider-turn", "itemId": "item-1", "startedAtMs": 1, "command": "pwd",
			}})
		case "":
			if hasID {
				writeCompletedTurn(encoder)
			}
		case "turn/interrupt":
			_ = encoder.Encode(map[string]any{"id": id, "result": map[string]any{}})
		}
	}
}

// writeCompletedTurn 写出审批后的 delta、最终 item 和 completed 终态。
func writeCompletedTurn(encoder *json.Encoder) {
	output := `{"answer":"测试回答","snapshot":{"current_topics":[],"confirmed_decisions":[],"business_rules":[],"disagreements":[],"open_questions":[],"references":[]}}`
	_ = encoder.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{
		"threadId": "provider-thread", "turnId": "provider-turn", "itemId": "item-2", "delta": output,
	}})
	_ = encoder.Encode(map[string]any{"method": "item/completed", "params": map[string]any{
		"threadId": "provider-thread", "turnId": "provider-turn", "completedAtMs": 2,
		"item": map[string]any{"id": "item-2", "type": "agentMessage", "text": output},
	}})
	_ = encoder.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{
		"threadId": "provider-thread", "turn": map[string]string{"id": "provider-turn", "status": "completed"},
	}})
}

type providerSchemaRunner struct{ content []byte }

// Version 返回测试 schema 身份。
func (runner providerSchemaRunner) Version(context.Context, string) (string, error) {
	return "codex-cli test", nil
}

// Generate 写入测试所需的单个冻结 schema。
func (runner providerSchemaRunner) Generate(_ context.Context, _ string, outputDirectory string) error {
	return os.WriteFile(filepath.Join(outputDirectory, "required.json"), runner.content, 0o600)
}

// providerHelperExecutable 创建忽略固定 app-server 参数的测试包装脚本。
func providerHelperExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "codex-helper")
	content := fmt.Sprintf("#!/bin/sh\nexec %s -test.run=TestProviderHelperProcess -- \"$@\"\n", shellQuote(executable))
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// shellQuote 对包装脚本中的单个可信可执行路径做 POSIX 引用。
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// containsArgument 判断 helper 是否由 app-server 包装入口启动。
func containsArgument(arguments []string, target string) bool {
	for _, argument := range arguments {
		if argument == target {
			return true
		}
	}
	return false
}

// containsEvent 判断完整事件序列是否包含目标类型。
func containsEvent(events []port.AgentEventType, target port.AgentEventType) bool {
	for _, event := range events {
		if event == target {
			return true
		}
	}
	return false
}
