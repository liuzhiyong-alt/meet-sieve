package port_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"meet-sieve/internal/port"
)

// TestAgentEnums_OnlyAcceptDeclaredValues 验证 Agent 主状态使用封闭枚举，而不是任意字符串。
func TestAgentEnums_OnlyAcceptDeclaredValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		valid bool
	}{
		{name: "availability available", valid: port.AgentAvailabilityAvailable.Valid()},
		{name: "availability invalid", valid: port.AgentAvailabilityState("ready").Valid()},
		{name: "account logged in", valid: port.AgentAccountLoggedIn.Valid()},
		{name: "account invalid", valid: port.AgentAccountState("authenticated").Valid()},
		{name: "protocol compatible", valid: port.AgentProtocolCompatible.Valid()},
		{name: "protocol invalid", valid: port.AgentProtocolState("ok").Valid()},
		{name: "turn answer", valid: port.AgentTurnAnswer.Valid()},
		{name: "turn invalid", valid: port.AgentTurnKind("chat").Valid()},
		{name: "event completed", valid: port.AgentEventCompleted.Valid()},
		{name: "event invalid", valid: port.AgentEventType("done").Valid()},
		{name: "approval allow", valid: port.AgentApprovalAllow.Valid()},
		{name: "approval invalid", valid: port.AgentApprovalDecision("always").Valid()},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			want := test.name != "availability invalid" && test.name != "account invalid" &&
				test.name != "protocol invalid" && test.name != "turn invalid" &&
				test.name != "event invalid" && test.name != "approval invalid"
			if test.valid != want {
				t.Fatalf("枚举校验错误：got %t, want %t", test.valid, want)
			}
		})
	}
}

type agentProviderContract struct{}

func (agentProviderContract) CheckAvailability(context.Context, port.AgentAvailabilityRequest) (port.AgentAvailability, error) {
	return port.AgentAvailability{}, nil
}

func (agentProviderContract) StartSession(context.Context, port.StartAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, nil
}

func (agentProviderContract) ResumeSession(context.Context, port.ResumeAgentSessionRequest) (port.AgentSession, error) {
	return port.AgentSession{}, nil
}

func (agentProviderContract) RunTurn(context.Context, port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	events := make(chan port.AgentEvent)
	close(events)
	return events, nil
}

func (agentProviderContract) RespondApproval(context.Context, port.RespondAgentApprovalRequest) error {
	return nil
}

func (agentProviderContract) InterruptTurn(context.Context, port.InterruptAgentTurnRequest) error {
	return nil
}

func (agentProviderContract) CloseSession(context.Context, string) error { return nil }

// TestAgentProvider_ExposesStableBusinessContract 在编译期固定 service 所需的完整业务接口。
func TestAgentProvider_ExposesStableBusinessContract(t *testing.T) {
	t.Parallel()

	var provider port.AgentProvider = agentProviderContract{}
	events, err := provider.RunTurn(context.Background(), port.RunAgentTurnRequest{})
	if err != nil {
		t.Fatalf("接受 turn 不应返回错误：%v", err)
	}
	if _, open := <-events; open {
		t.Fatal("测试 provider 必须由生产方关闭事件通道")
	}
}

// TestAgentProvider_DoesNotLeakProviderProtocol 验证业务 port 不暴露厂商或传输层术语。
func TestAgentProvider_DoesNotLeakProviderProtocol(t *testing.T) {
	t.Parallel()

	path := filepath.Join(projectRoot(t), "internal", "port", "agent_provider.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 Agent port 失败：%v", err)
	}
	for _, forbidden := range []string{"Codex", "JSON-RPC", "jsonrpc"} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("Agent port 泄漏 provider 协议术语：%s", forbidden)
		}
	}
}
