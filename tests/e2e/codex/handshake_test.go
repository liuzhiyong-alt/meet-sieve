package codex_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/adapter/agent/codex"
	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/port"
)

// TestCodexHandshake_CompletesAgainstInstalledCodex 验证本机 Codex app-server 的真实协议握手。
func TestCodexHandshake_CompletesAgainstInstalledCodex(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("本机未安装 codex：%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := codex.NewClient(codex.Config{
		Command: "codex",
		Args:    []string{"app-server", "--stdio"},
	})
	result, err := client.Handshake(ctx)
	if err != nil {
		t.Fatalf("真实 Codex 握手失败：%v", err)
	}
	if result.UserAgent == "" || result.PlatformOS == "" {
		t.Fatalf("Codex initialize 响应缺少必要字段：%+v", result)
	}
	t.Logf(
		"Codex 真实握手成功：user_agent=%s platform=%s/%s",
		result.UserAgent,
		result.PlatformFamily,
		result.PlatformOS,
	)
}

// TestCodexProviderAvailability_VerifiesSchemaAndLogin 验证 Step 7 实际入口的版本、schema、握手和登录门。
func TestCodexProviderAvailability_VerifiesSchemaAndLogin(t *testing.T) {
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("本机未安装 codex：%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider := codex.NewProvider(executable)
	availability, err := provider.CheckAvailability(ctx, port.AgentAvailabilityRequest{ExecutablePath: executable})
	if err != nil {
		t.Fatalf("Codex Provider 真实探测失败：state=%+v err=%v", availability, err)
	}
	if availability.State != port.AgentAvailabilityAvailable || availability.AccountState != port.AgentAccountLoggedIn || availability.ProtocolState != port.AgentProtocolCompatible {
		t.Fatalf("Codex Provider 状态不完整：%+v", availability)
	}
	t.Logf("Codex Provider 可用：version=%s", availability.Version)
}

// TestCodexLauncher_ResolvesDesktopEnvironment 验证 Finder 风格精简 PATH 下仍能启动用户配置的 Codex 入口。
func TestCodexLauncher_ResolvesDesktopEnvironment(t *testing.T) {
	executable := os.Getenv("MEETSIEVE_DESKTOP_CODEX_PATH")
	if executable == "" {
		t.Skip("设置 MEETSIEVE_DESKTOP_CODEX_PATH 后执行桌面启动环境验证")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	spec, err := codex.NewLauncher().Resolve(ctx, executable, 0)
	if err != nil {
		t.Fatalf("桌面环境解析 Codex 失败：%v", err)
	}
	output, err := spec.CommandContext(ctx, "--version").Output()
	if err != nil {
		t.Fatalf("桌面环境启动 Codex 失败：%v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(output)), "codex-cli ") {
		t.Fatalf("Codex 版本输出不正确：%q", output)
	}
}

// TestCodexProviderThreeTurnsAndResume 验证真实 thread 中三轮结构化输出及跨进程恢复。
// 该测试会调用真实模型，默认跳过；发布前通过显式环境变量执行，避免普通 CI 产生外部费用。
func TestCodexProviderThreeTurnsAndResume(t *testing.T) {
	if os.Getenv("MEETSIEVE_REAL_CODEX_TURNS") != "1" {
		t.Skip("设置 MEETSIEVE_REAL_CODEX_TURNS=1 后执行真实三轮 Codex 验证")
	}
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Fatalf("本机未安装 codex：%v", err)
	}
	provider := codex.NewProvider(executable)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	defer func() { _ = provider.CloseSession(context.Background(), "e2e-session") }()
	workingDirectory := t.TempDir()
	const toolProof = "MEETSIEVE_TOOL_OK_20260802"
	if err := os.WriteFile(workingDirectory+"/tool-proof.txt", []byte(toolProof), 0o600); err != nil {
		t.Fatalf("准备真实只读工具夹具失败：%v", err)
	}

	session, err := provider.StartSession(ctx, port.StartAgentSessionRequest{
		SessionID: "e2e-session", WorkingDirectory: workingDirectory, ExecutablePath: executable,
		Prompt: "严格按每轮 JSON Schema 输出。除非问题明确要求，否则不调用工具；明确要求时只允许用 shell 读取当前临时目录中的指定文件，禁止写入和其他工具。",
	})
	if err != nil {
		t.Fatalf("创建真实 Codex thread 失败：%v", err)
	}
	if session.ProviderSessionID == "" {
		t.Fatal("真实 Codex thread 缺少 provider session ID")
	}

	rounds := []struct {
		kind  port.AgentTurnKind
		input string
	}{
		{port.AgentTurnInitialize, "会议事实：项目代号是晨星。建立快照；snapshot.references 必须是空数组。"},
		{port.AgentTurnIngest, "新增会议事实：交付日期为周五。更新快照；snapshot.references 必须是空数组。"},
		{port.AgentTurnAnswer, "必须实际使用 shell 读取当前工作目录的 tool-proof.txt，用一句话原样返回文件内容，并返回更新后的快照；snapshot.references 必须是空数组。"},
	}
	for index, round := range rounds {
		schema, schemaErr := domainagent.OutputSchema(round.kind)
		if schemaErr != nil {
			t.Fatal(schemaErr)
		}
		output, runErr := runRealTurn(ctx, provider, port.RunAgentTurnRequest{
			SessionID: session.ID, TurnID: "e2e-turn-" + string(rune('1'+index)), Kind: round.kind,
			Input: round.input, OutputSchema: schema, Deadline: time.Now().Add(90 * time.Second),
		})
		if runErr != nil {
			t.Fatalf("真实 Codex 第 %d 轮失败：%v", index+1, runErr)
		}
		validated, validateErr := domainagent.ValidateOutput(round.kind, output, domainagent.ReferenceAllowlist{})
		if validateErr != nil {
			t.Fatalf("真实 Codex 第 %d 轮输出未通过本地校验：%v", index+1, validateErr)
		}
		if index == len(rounds)-1 && !strings.Contains(validated.Answer, toolProof) {
			t.Fatalf("真实只读 shell 工具未返回夹具内容：%q", validated.Answer)
		}
	}
	if err := provider.CloseSession(ctx, session.ID); err != nil {
		t.Fatalf("关闭首个 app-server 失败：%v", err)
	}
	resumed, err := provider.ResumeSession(ctx, port.ResumeAgentSessionRequest{
		SessionID: "e2e-resumed", ProviderSessionID: session.ProviderSessionID,
		WorkingDirectory: workingDirectory, ExecutablePath: executable,
	})
	if err != nil || !resumed.Resumed || resumed.ProviderSessionID != session.ProviderSessionID {
		t.Fatalf("真实 Codex thread 恢复失败：session=%+v err=%v", resumed, err)
	}
	defer func() { _ = provider.CloseSession(context.Background(), resumed.ID) }()
	schema, err := domainagent.OutputSchema(port.AgentTurnAnswer)
	if err != nil {
		t.Fatal(err)
	}
	output, err := runRealTurn(ctx, provider, port.RunAgentTurnRequest{
		SessionID: resumed.ID, TurnID: "e2e-resumed-turn", Kind: port.AgentTurnAnswer,
		Input:        "不要再次调用工具，返回上一轮读到的文件内容；snapshot.references 必须是空数组。",
		OutputSchema: schema, Deadline: time.Now().Add(90 * time.Second),
	})
	if err != nil {
		t.Fatalf("恢复后的真实 Codex turn 失败：%v", err)
	}
	validated, err := domainagent.ValidateOutput(port.AgentTurnAnswer, output, domainagent.ReferenceAllowlist{})
	if err != nil || !strings.Contains(validated.Answer, toolProof) {
		t.Fatalf("恢复后的 thread 未保留上下文：answer=%q err=%v", validated.Answer, err)
	}
	t.Log("Codex Provider 真实三轮、只读工具调用和跨进程 thread 恢复通过")
}

// runRealTurn 等待真实 provider 同时返回 final output 和 completed。
func runRealTurn(ctx context.Context, provider port.AgentProvider, request port.RunAgentTurnRequest) ([]byte, error) {
	events, err := provider.RunTurn(ctx, request)
	if err != nil {
		return nil, err
	}
	var output []byte
	completed := false
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, ok := <-events:
			if !ok {
				if !completed || len(output) == 0 {
					return nil, errors.New("真实 Codex turn 缺少 final output 或 completed")
				}
				return output, nil
			}
			switch event.Type {
			case port.AgentEventFinalOutput:
				if event.FinalOutput != nil {
					output = append([]byte(nil), event.FinalOutput.JSON...)
				}
			case port.AgentEventCompleted:
				completed = true
			case port.AgentEventFailed, port.AgentEventCancelled:
				return nil, errors.New("真实 Codex turn 以失败状态结束：" + event.FailureCode)
			case port.AgentEventApprovalRequested:
				return nil, errors.New("只读临时文件操作不应触发原生审批")
			}
		}
	}
}
