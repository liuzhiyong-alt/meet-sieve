package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

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
	Availability   port.AgentAvailability
	UpdatedAt      int64
}

// SaveAgentSettingsInput 是设置页允许修改的两个字段。
type SaveAgentSettingsInput struct {
	WakeWord       string
	ExecutablePath string
}

// SettingsService 管理唤醒词、单 executable 和脱敏可用性探测。
type SettingsService struct {
	repository *agentrepository.Repository
	provider   port.AgentProvider
	clock      clock.Clock
	mu         sync.Mutex
	lastProbe  port.AgentAvailability
}

// NewSettingsService 创建 Codex 设置服务；构造阶段不探测外部进程。
func NewSettingsService(repository *agentrepository.Repository, provider port.AgentProvider, currentClock clock.Clock) *SettingsService {
	return &SettingsService{
		repository: repository, provider: provider, clock: currentClock,
		lastProbe: port.AgentAvailability{
			State: port.AgentAvailabilityUnchecked, AccountState: port.AgentAccountUnknown,
			ProtocolState: port.AgentProtocolUnchecked, Message: "尚未检测",
		},
	}
}

// Get 返回当前设置和进程内最近一次脱敏探测结果。
func (service *SettingsService) Get(ctx context.Context) (AgentSettingsView, error) {
	if service == nil || service.repository == nil {
		return AgentSettingsView{}, fmt.Errorf("Codex 设置服务未初始化")
	}
	settings, err := service.repository.GetSettings(ctx)
	if err != nil {
		return AgentSettingsView{}, err
	}
	service.mu.Lock()
	availability := service.lastProbe
	service.mu.Unlock()
	return AgentSettingsView{
		WakeWord: settings.WakeWord, ExecutablePath: stringValue(settings.CodexExecutablePath),
		Availability: availability, UpdatedAt: settings.UpdatedAt,
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
	if err := service.repository.UpdateSettings(ctx, wake.Value, executablePath, service.clock.Now().UnixMilli()); err != nil {
		return AgentSettingsView{}, err
	}
	service.mu.Lock()
	service.lastProbe = port.AgentAvailability{
		State: port.AgentAvailabilityUnchecked, AccountState: port.AgentAccountUnknown,
		ProtocolState: port.AgentProtocolUnchecked, Message: "设置已更新，尚未检测",
	}
	service.mu.Unlock()
	return service.Get(ctx)
}

// Probe 使用已保存 executable 执行 schema、握手和登录探测。
func (service *SettingsService) Probe(ctx context.Context) (port.AgentAvailability, error) {
	if service == nil || service.repository == nil || service.provider == nil {
		return port.AgentAvailability{}, fmt.Errorf("Codex 设置服务未初始化")
	}
	settings, err := service.repository.GetSettings(ctx)
	if err != nil {
		return port.AgentAvailability{}, err
	}
	availability, probeErr := service.provider.CheckAvailability(ctx, port.AgentAvailabilityRequest{ExecutablePath: stringValue(settings.CodexExecutablePath)})
	service.mu.Lock()
	service.lastProbe = availability
	service.mu.Unlock()
	return availability, probeErr
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
	resolved := trimmed
	if !filepath.IsAbs(trimmed) {
		if strings.ContainsAny(trimmed, `/\\`) || strings.Contains(trimmed, " ") {
			return nil, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.settings.executable"))
		}
		path, err := exec.LookPath(trimmed)
		if err != nil {
			return nil, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.settings.executable"))
		}
		resolved = path
	}
	info, err := os.Stat(resolved)
	if err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		return nil, apperr.Biz(apperr.CodeAgentExecutableInvalid, apperr.WithOp("agent.settings.executable"))
	}
	return &resolved, nil
}

// stringValue 安全读取可选字符串。
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
