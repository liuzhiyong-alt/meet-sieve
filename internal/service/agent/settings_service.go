package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
)

// AgentSettingsView 是不含账号、Codex home 或凭据的设置投影。
type AgentSettingsView struct {
	WakeWord       string
	ExecutablePath string
	ProxyPort      int
	Availability   port.AgentAvailability
	ProbedAt       int64
	UpdatedAt      int64
}

// SaveAgentSettingsInput 是设置页允许修改的 Codex 启动配置。
type SaveAgentSettingsInput struct {
	WakeWord       string
	ExecutablePath string
	ProxyPort      int
}

// SettingsService 管理唤醒词、单 executable 和脱敏可用性探测。
type SettingsService struct {
	repository *agentrepository.Repository
	provider   port.AgentProvider
	clock      clock.Clock
}

// NewSettingsService 创建 Codex 设置服务；构造阶段不探测外部进程。
func NewSettingsService(repository *agentrepository.Repository, provider port.AgentProvider, currentClock clock.Clock) *SettingsService {
	return &SettingsService{repository: repository, provider: provider, clock: currentClock}
}

// Get 返回当前设置和 SQLite 中最近一次脱敏探测结果。
func (service *SettingsService) Get(ctx context.Context) (AgentSettingsView, error) {
	if service == nil || service.repository == nil {
		return AgentSettingsView{}, fmt.Errorf("Codex 设置服务未初始化")
	}
	settings, err := service.repository.GetSettings(ctx)
	if err != nil {
		return AgentSettingsView{}, err
	}
	return AgentSettingsView{
		WakeWord: settings.WakeWord, ExecutablePath: stringValue(settings.CodexExecutablePath),
		ProxyPort: proxyPortValue(settings.CodexProxyPort),
		Availability: port.AgentAvailability{
			State: port.AgentAvailabilityState(settings.CodexAvailabilityState), Version: settings.CodexVersion,
			AccountState:  port.AgentAccountState(settings.CodexAccountState),
			ProtocolState: port.AgentProtocolState(settings.CodexProtocolState), Message: settings.CodexProbeMessage,
		},
		ProbedAt: optionalInt64Value(settings.CodexProbedAt), UpdatedAt: settings.UpdatedAt,
	}, nil
}

// Save 规范化唤醒词并校验 executable 是单个可执行文件，不接受附加参数。
func (service *SettingsService) Save(ctx context.Context, input SaveAgentSettingsInput) (AgentSettingsView, error) {
	if service == nil || service.repository == nil || service.clock == nil {
		return AgentSettingsView{}, fmt.Errorf("Codex 设置服务未初始化")
	}
	wake, err := domainagent.NormalizeWakeWord(input.WakeWord)
	if err != nil {
		return AgentSettingsView{}, apperr.Biz(apperr.CodeAgentWakeWordInvalid, apperr.WithOp("agent.settings.wake_word"))
	}
	executablePath, err := validateExecutableSetting(input.ExecutablePath)
	if err != nil {
		return AgentSettingsView{}, err
	}
	proxyPort, err := validateProxyPort(input.ProxyPort)
	if err != nil {
		return AgentSettingsView{}, err
	}
	if err := service.repository.UpdateSettings(ctx, wake.Value, executablePath, proxyPort, service.clock.Now().UnixMilli()); err != nil {
		return AgentSettingsView{}, err
	}
	return service.Get(ctx)
}

// Probe 使用已保存 executable 执行 schema、握手和登录探测。
func (service *SettingsService) Probe(ctx context.Context) (port.AgentAvailability, error) {
	if service == nil || service.repository == nil || service.provider == nil || service.clock == nil {
		return port.AgentAvailability{}, fmt.Errorf("Codex 设置服务未初始化")
	}
	settings, err := service.repository.GetSettings(ctx)
	if err != nil {
		return port.AgentAvailability{}, err
	}
	availability, probeErr := service.provider.CheckAvailability(ctx, port.AgentAvailabilityRequest{
		ExecutablePath: stringValue(settings.CodexExecutablePath), ProxyPort: proxyPortValue(settings.CodexProxyPort),
	})
	availability = normalizeAvailability(availability, probeErr)
	if err := service.repository.UpdateProbeSnapshot(ctx, string(availability.State), availability.Version, string(availability.AccountState), string(availability.ProtocolState), availability.Message, service.clock.Now().UnixMilli()); err != nil {
		return availability, err
	}
	return availability, probeErr
}

// normalizeAvailability 防止 provider 失败时把零值或底层错误写入稳定设置投影。
func normalizeAvailability(value port.AgentAvailability, probeErr error) port.AgentAvailability {
	if !value.State.Valid() {
		value.State = port.AgentAvailabilityUnavailable
	}
	if !value.AccountState.Valid() {
		value.AccountState = port.AgentAccountUnknown
	}
	if !value.ProtocolState.Valid() {
		value.ProtocolState = port.AgentProtocolUnchecked
	}
	if strings.TrimSpace(value.Message) == "" {
		if probeErr != nil {
			value.Message = "检测失败，请重试"
		} else {
			value.Message = "检测完成"
		}
	}
	return value
}

// validateExecutableSetting 解析裸命令或绝对路径，但不拆分 shell 参数。
func validateExecutableSetting(value string) (*string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if strings.ContainsAny(trimmed, "\r\n\x00") {
		return nil, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.settings.executable"))
	}
	if !filepath.IsAbs(trimmed) {
		if strings.ContainsAny(trimmed, `/\\`) || strings.Contains(trimmed, " ") {
			return nil, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.settings.executable"))
		}
		// 裸命令由 Codex Launcher 使用桌面增强 PATH 解析，保存阶段不依赖当前进程 PATH。
		return &trimmed, nil
	}
	info, err := os.Stat(trimmed)
	if err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		return nil, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.settings.executable"))
	}
	return &trimmed, nil
}

// stringValue 安全读取可选字符串。
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// optionalInt64Value 安全读取可选时间戳；未检测时返回零。
func optionalInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// proxyPortValue 将数据库中的 NULL 映射为 UI 和启动链统一使用的直连值。
func proxyPortValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

// validateProxyPort 只接受本机 HTTP(S) 代理的有效端口；零值表示不使用代理。
func validateProxyPort(value int) (*int, error) {
	if value == 0 {
		return nil, nil
	}
	if value < 1 || value > 65535 {
		return nil, apperr.Biz(apperr.CodeAgentProxyPortInvalid, apperr.WithOp("agent.settings.proxy"))
	}
	return &value, nil
}
