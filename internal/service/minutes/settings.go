package minutes

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"meet-sieve/internal/infra/clock"
	minutesrepository "meet-sieve/internal/repository/minutes"
)

const maxMinutePromptBytes = 20_000

// DefaultPrompt 是用户未自定义时使用的业务会议纪要要求。
const DefaultPrompt = `请生成一份简洁、清晰、可执行的中文会议纪要：

1. 首先总结会议中已经明确达成的结论。
2. 按议题整理主要讨论内容，保留关键观点、分歧、风险和待确认事项。
3. 提取会议中明确提出的待办事项；只有在会议中明确提及时，才填写负责人和截止时间。
4. 列出会议中提供的相关资料。
5. 区分已经确认的事实、尚未确定的事项和个人建议。
6. 避免简单重复原始记录，优先保留对后续决策和执行有价值的信息。`

// SettingsView 是会议纪要设置页可安全展示的投影。
type SettingsView struct {
	Prompt    string
	IsDefault bool
	UpdatedAt int64
}

// SettingsService 独立读取和保存会议纪要生成要求。
type SettingsService struct {
	repository *minutesrepository.Repository
	clock      clock.Clock
}

// NewSettingsService 创建会议纪要设置服务。
func NewSettingsService(repository *minutesrepository.Repository, appClock clock.Clock) *SettingsService {
	return &SettingsService{repository: repository, clock: appClock}
}

// Get 返回已保存要求；未配置时回填当前默认内容。
func (service *SettingsService) Get(ctx context.Context) (SettingsView, error) {
	if service == nil || service.repository == nil {
		return SettingsView{}, fmt.Errorf("会议纪要设置服务不可用")
	}
	settings, err := service.repository.GetSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	prompt, isDefault := resolveMinutePrompt(settings.MinutePrompt)
	return SettingsView{Prompt: prompt, IsDefault: isDefault, UpdatedAt: settings.UpdatedAt}, nil
}

// Save 保存用户输入的业务要求；空内容表示恢复当前内置默认要求。
func (service *SettingsService) Save(ctx context.Context, prompt string) (SettingsView, error) {
	if service == nil || service.repository == nil || service.clock == nil {
		return SettingsView{}, fmt.Errorf("会议纪要设置服务不可用")
	}
	prompt = strings.TrimSpace(prompt)
	if len(prompt) > maxMinutePromptBytes || !utf8.ValidString(prompt) {
		return SettingsView{}, fmt.Errorf("会议纪要要求不能超过 %d 字节，且必须是有效文本", maxMinutePromptBytes)
	}
	updatedAt := service.clock.Now().UnixMilli()
	if prompt == "" {
		if err := service.repository.ResetMinutePrompt(ctx, updatedAt); err != nil {
			return SettingsView{}, err
		}
		return SettingsView{Prompt: DefaultPrompt, IsDefault: true, UpdatedAt: updatedAt}, nil
	}
	if err := service.repository.UpdateMinutePrompt(ctx, prompt, updatedAt); err != nil {
		return SettingsView{}, err
	}
	return SettingsView{Prompt: prompt, IsDefault: false, UpdatedAt: updatedAt}, nil
}

// CurrentPrompt 返回一次生成实际使用的要求，未配置时使用内置默认值。
func (service *SettingsService) CurrentPrompt(ctx context.Context) (string, error) {
	view, err := service.Get(ctx)
	return view.Prompt, err
}

// resolveMinutePrompt 返回当前有效业务要求及其是否来自内置默认值。
func resolveMinutePrompt(prompt *string) (string, bool) {
	if prompt == nil || strings.TrimSpace(*prompt) == "" {
		return DefaultPrompt, true
	}
	return *prompt, false
}
