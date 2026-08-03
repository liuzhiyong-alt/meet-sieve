package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"

	"github.com/google/uuid"
)

const defaultRPCTimeout = 15 * time.Second

// Provider 使用一个长生命周期 app-server 进程实现稳定 AgentProvider。
type Provider struct {
	executable string
	verifier   *SchemaVerifier
	mu         sync.Mutex
	sessions   map[string]*sessionRuntime
}

// NewProvider 创建 Codex provider；构造阶段不启动进程或 goroutine。
func NewProvider(executablePath string) *Provider {
	return &Provider{
		executable: executablePath,
		verifier:   NewSchemaVerifier(CommandSchemaRunner{}, RequiredSchemaContract(), ""),
		sessions:   make(map[string]*sessionRuntime),
	}
}

// NewProviderWithVerifier 创建可注入 schema 校验器的 provider，供契约测试使用。
func NewProviderWithVerifier(executablePath string, verifier *SchemaVerifier) *Provider {
	return &Provider{executable: executablePath, verifier: verifier, sessions: make(map[string]*sessionRuntime)}
}

// CheckAvailability 严格校验 executable、runtime schema、握手和登录状态。
func (provider *Provider) CheckAvailability(ctx context.Context, request port.AgentAvailabilityRequest) (port.AgentAvailability, error) {
	executable := request.ExecutablePath
	if executable == "" {
		executable = provider.executable
	}
	if provider == nil || provider.verifier == nil || executable == "" {
		return unavailable(port.AgentProtocolUnchecked, port.AgentAccountUnknown, "Codex 可执行文件未配置"), executableInvalid()
	}
	if err := provider.verifier.Verify(ctx, executable); err != nil {
		return unavailable(port.AgentProtocolIncompatible, port.AgentAccountUnknown, "当前 Codex 版本暂不兼容"), err
	}
	version, err := (CommandSchemaRunner{}).Version(ctx, executable)
	if err != nil {
		return unavailable(port.AgentProtocolCompatible, port.AgentAccountUnknown, "无法读取 Codex 版本"), executableInvalid()
	}
	loggedIn, err := probeAccount(ctx, executable)
	if err != nil {
		return unavailable(port.AgentProtocolCompatible, port.AgentAccountUnknown, "Codex 探测失败"), err
	}
	if !loggedIn {
		return port.AgentAvailability{
			State: port.AgentAvailabilityUnavailable, Version: version,
			AccountState: port.AgentAccountLoggedOut, ProtocolState: port.AgentProtocolCompatible,
			Message: "Codex 尚未登录",
		}, apperr.Dependency(apperr.CodeAgentNotLoggedIn, errors.New("account/read returned null"), apperr.WithOp("agent.codex.account"))
	}
	return port.AgentAvailability{
		State: port.AgentAvailabilityAvailable, Version: version,
		AccountState: port.AgentAccountLoggedIn, ProtocolState: port.AgentProtocolCompatible,
		Message: "Codex 可用",
	}, nil
}

// StartSession 启动 app-server、完成握手并创建 thread。
func (provider *Provider) StartSession(ctx context.Context, request port.StartAgentSessionRequest) (port.AgentSession, error) {
	executable := provider.sessionExecutable(request.ExecutablePath)
	if provider == nil || executable == "" || !filepath.IsAbs(request.WorkingDirectory) {
		return port.AgentSession{}, executableInvalid()
	}
	if err := provider.verifier.Verify(ctx, executable); err != nil {
		return port.AgentSession{}, err
	}
	runtime, err := provider.startRuntime(ctx, executable)
	if err != nil {
		return port.AgentSession{}, err
	}
	response, err := runtime.startThread(ctx, request.WorkingDirectory, request.Prompt)
	if err != nil {
		runtime.close(context.Background())
		return port.AgentSession{}, err
	}
	runtime.id = request.SessionID
	if runtime.id == "" {
		runtime.id = uuid.NewString()
	}
	runtime.threadID = response.Thread.ID
	if err := provider.attachRuntime(runtime); err != nil {
		runtime.close(context.Background())
		return port.AgentSession{}, err
	}
	return port.AgentSession{ID: runtime.id, ProviderSessionID: runtime.threadID, Resumed: false}, nil
}

// ResumeSession 启动新 app-server 进程并恢复既有 thread。
func (provider *Provider) ResumeSession(ctx context.Context, request port.ResumeAgentSessionRequest) (port.AgentSession, error) {
	executable := provider.sessionExecutable(request.ExecutablePath)
	if provider == nil || executable == "" || request.SessionID == "" || request.ProviderSessionID == "" || !filepath.IsAbs(request.WorkingDirectory) {
		return port.AgentSession{}, executableInvalid()
	}
	if err := provider.verifier.Verify(ctx, executable); err != nil {
		return port.AgentSession{}, err
	}
	runtime, err := provider.startRuntime(ctx, executable)
	if err != nil {
		return port.AgentSession{}, err
	}
	response, err := runtime.resumeThread(ctx, request.ProviderSessionID, request.WorkingDirectory)
	if err != nil {
		runtime.close(context.Background())
		return port.AgentSession{}, apperr.Dependency(apperr.CodeAgentThreadNotFound, err, apperr.WithOp("agent.codex.thread.resume"))
	}
	runtime.id = request.SessionID
	runtime.threadID = response.Thread.ID
	if err := provider.attachRuntime(runtime); err != nil {
		runtime.close(context.Background())
		return port.AgentSession{}, err
	}
	return port.AgentSession{ID: runtime.id, ProviderSessionID: runtime.threadID, Resumed: true}, nil
}

// RunTurn 提交结构化 turn；成功仅表示 app-server 已接受请求。
func (provider *Provider) RunTurn(ctx context.Context, request port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	runtime, err := provider.getRuntime(request.SessionID)
	if err != nil {
		return nil, err
	}
	return runtime.runTurn(ctx, request)
}

// RespondApproval 把主持人本次决定交回当前 session 的原生审批桥。
func (provider *Provider) RespondApproval(ctx context.Context, request port.RespondAgentApprovalRequest) error {
	runtime, err := provider.getRuntime(request.SessionID)
	if err != nil {
		return err
	}
	return runtime.approvals.Respond(ctx, request)
}

// InterruptTurn 幂等中断当前 provider turn，并先拒绝悬挂审批。
func (provider *Provider) InterruptTurn(ctx context.Context, request port.InterruptAgentTurnRequest) error {
	runtime, err := provider.getRuntime(request.SessionID)
	if err != nil {
		return err
	}
	return runtime.interrupt(ctx, request.TurnID)
}

// CloseSession 先中断活动 turn，再关闭 stdin 并等待至多三秒。
func (provider *Provider) CloseSession(ctx context.Context, sessionID string) error {
	provider.mu.Lock()
	runtime, exists := provider.sessions[sessionID]
	if exists {
		delete(provider.sessions, sessionID)
	}
	provider.mu.Unlock()
	if !exists {
		return nil
	}
	return runtime.close(ctx)
}

// startRuntime 启动进程并完成 initialize/account/read，尚不创建 thread。
func (provider *Provider) startRuntime(ctx context.Context, executable string) (*sessionRuntime, error) {
	process, err := startProcess(Config{Command: executable, Args: []string{"app-server", "--stdio"}})
	if err != nil {
		return nil, apperr.Dependency(apperr.CodeAgentInitializeFailed, err, apperr.WithOp("agent.codex.process"))
	}
	runtime := newSessionRuntime(process)
	if err := runtime.initialize(ctx); err != nil {
		runtime.close(context.Background())
		return nil, err
	}
	return runtime, nil
}

// sessionExecutable 为单次 session 选用已保存设置，空值才回退构造默认值。
func (provider *Provider) sessionExecutable(configured string) string {
	if configured != "" {
		return configured
	}
	if provider == nil {
		return ""
	}
	return provider.executable
}

// attachRuntime 强制进程内只维护一个活动 app-server/session。
func (provider *Provider) attachRuntime(runtime *sessionRuntime) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.sessions) != 0 {
		return apperr.Biz(apperr.CodeAgentBusy, apperr.WithOp("agent.codex.session.attach"))
	}
	provider.sessions[runtime.id] = runtime
	return nil
}

// getRuntime 返回当前本地 session owner。
func (provider *Provider) getRuntime(sessionID string) (*sessionRuntime, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	runtime, exists := provider.sessions[sessionID]
	if !exists {
		return nil, apperr.Dependency(apperr.CodeAgentThreadNotFound, errors.New("session runtime missing"), apperr.WithOp("agent.codex.session.get"))
	}
	return runtime, nil
}

// unavailable 构造不泄漏账号和本机路径的不可用状态。
func unavailable(protocol port.AgentProtocolState, account port.AgentAccountState, message string) port.AgentAvailability {
	return port.AgentAvailability{State: port.AgentAvailabilityUnavailable, ProtocolState: protocol, AccountState: account, Message: message}
}

// executableInvalid 返回稳定 executable 错误。
func executableInvalid() error {
	return apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.codex.executable"))
}

// probeAccount 启动短探测进程，只读取登录布尔值后安全关闭。
func probeAccount(ctx context.Context, executable string) (bool, error) {
	process, err := startProcess(Config{Command: executable, Args: []string{"app-server", "--stdio"}})
	if err != nil {
		return false, apperr.Dependency(apperr.CodeAgentInitializeFailed, err, apperr.WithOp("agent.codex.probe.start"))
	}
	runtime := newSessionRuntime(process)
	defer runtime.close(context.Background())
	if err := runtime.initialize(ctx); err != nil {
		if apperr.Normalize(err).ErrorCode == apperr.CodeAgentNotLoggedIn.ErrorCode {
			return false, nil
		}
		return false, err
	}
	return runtime.loggedIn, nil
}

type threadResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

type accountResponse struct {
	Account            json.RawMessage `json:"account"`
	RequiresOpenAIAuth bool            `json:"requiresOpenaiAuth"`
}

var _ port.AgentProvider = (*Provider)(nil)
