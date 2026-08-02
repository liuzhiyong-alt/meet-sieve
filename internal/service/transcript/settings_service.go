package transcript

import (
	"context"
	"fmt"
	"strings"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/port"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// CredentialAction 表示保存凭据字段时无歧义的处理动作。
type CredentialAction string

const (
	// CredentialKeep 保留数据库中的原值。
	CredentialKeep CredentialAction = "keep"
	// CredentialReplace 用提交的新值替换原值。
	CredentialReplace CredentialAction = "replace"
	// CredentialClear 将字段明确置空。
	CredentialClear CredentialAction = "clear"
)

// CredentialChange 描述一个凭据字段的保存动作；只有 replace 使用 Value。
type CredentialChange struct {
	// Action 指定保留、替换或清空。
	Action CredentialAction
	// Value 仅在 Action 为 replace 时携带新明文。
	Value string
}

// SaveASRSettingsInput 是 ASR 设置事务允许修改的字段。
type SaveASRSettingsInput struct {
	// Mode 是保存后的当前鉴权方式。
	Mode transcriptdomain.AuthMode
	// AppID 描述 App ID 的显式修改动作。
	AppID CredentialChange
	// AccessToken 描述 Access Token 的显式修改动作。
	AccessToken CredentialChange
	// APIKey 描述新控制台 API Key 的显式修改动作。
	APIKey CredentialChange
}

// ASRSettingsView 是可安全返回给 Wails 的掩码设置投影。
type ASRSettingsView struct {
	// Mode 是当前鉴权方式。
	Mode transcriptdomain.AuthMode
	// AppIDConfigured 表示已保存 App ID。
	AppIDConfigured bool
	// AppIDMask 只展示固定掩码和末四位。
	AppIDMask string
	// AccessTokenConfigured 表示已保存 Access Token。
	AccessTokenConfigured bool
	// AccessTokenMask 只展示固定掩码和末四位。
	AccessTokenMask string
	// APIKeyConfigured 表示已保存 API Key。
	APIKeyConfigured bool
	// APIKeyMask 只展示固定掩码和末四位。
	APIKeyMask string
	// UpdatedAt 是设置最后更新时间的 Unix 毫秒值。
	UpdatedAt int64
}

// ConnectionProbeResult 明确区分“连接已建立”和“真实音频转写已验证”。
type ConnectionProbeResult struct {
	// Mode 是本次探测使用的鉴权方式。
	Mode transcriptdomain.AuthMode
	// ConnectionEstablished 表示 WebSocket 握手和初始化帧成功。
	ConnectionEstablished bool
	// RealAudioVerified 明确表示本连接探测没有发送真实音频。
	RealAudioVerified bool
	// LatencyMS 是建立并关闭探测 session 的耗时。
	LatencyMS int64
}

// TranscriberFactory 根据当前草稿凭据创建一个不共享会议状态的探测 adapter。
type TranscriberFactory func(transcriptdomain.Credentials) port.RealtimeTranscriber

// SettingsServiceDependencies 描述 ASR 设置、事务、时钟和连接探测依赖。
type SettingsServiceDependencies struct {
	// Repository 读写 SQLite settings 与活动会议事实。
	Repository *transcriptrepository.Repository
	// Transactions 提供设置保存的短事务。
	Transactions *database.TransactionManager
	// Clock 提供可测试的时间。
	Clock clock.Clock
	// Transcriber 为每次连接测试创建独立 adapter。
	Transcriber TranscriberFactory
}

// SettingsService 编排凭据掩码、显式变更动作和独立连接探测。
type SettingsService struct {
	repository   *transcriptrepository.Repository
	transactions *database.TransactionManager
	clock        clock.Clock
	transcriber  TranscriberFactory
}

// NewSettingsService 创建 ASR 设置服务；构造阶段不读取凭据或建立网络连接。
func NewSettingsService(dependencies SettingsServiceDependencies) *SettingsService {
	return &SettingsService{repository: dependencies.Repository, transactions: dependencies.Transactions, clock: dependencies.Clock, transcriber: dependencies.Transcriber}
}

// GetSettings 返回掩码投影，绝不返回凭证明文。
func (service *SettingsService) GetSettings(ctx context.Context) (ASRSettingsView, error) {
	if service == nil || service.repository == nil {
		return ASRSettingsView{}, fmt.Errorf("ASR 设置服务未初始化")
	}
	settings, err := service.repository.GetSettings(ctx)
	if err != nil {
		return ASRSettingsView{}, err
	}
	return mapSettingsView(settings), nil
}

// CurrentCredentials 仅供 Go 运行时读取当前模式所需明文，不允许映射到 Wails DTO。
func (service *SettingsService) CurrentCredentials(ctx context.Context) (transcriptdomain.Credentials, error) {
	if service == nil || service.repository == nil {
		return transcriptdomain.Credentials{}, fmt.Errorf("ASR 设置服务未初始化")
	}
	settings, err := service.repository.GetSettings(ctx)
	if err != nil {
		return transcriptdomain.Credentials{}, err
	}
	credentials := credentialsFromSettings(settings)
	if err = credentials.Validate(); err != nil {
		return transcriptdomain.Credentials{}, apperr.Biz(apperr.CodeASRSettingsInvalid, apperr.WithOp("transcript.settings.current_credentials"))
	}
	return credentials, nil
}

// SaveSettings 在一个短事务内阻止活动会议修改，并执行 keep/replace/clear。
func (service *SettingsService) SaveSettings(ctx context.Context, input SaveASRSettingsInput) (ASRSettingsView, error) {
	if service == nil || service.repository == nil || service.transactions == nil || service.clock == nil {
		return ASRSettingsView{}, fmt.Errorf("ASR 设置服务未初始化")
	}
	if !input.Mode.IsValid() || !isValidCredentialChange(input.AppID) || !isValidCredentialChange(input.AccessToken) || !isValidCredentialChange(input.APIKey) {
		return ASRSettingsView{}, apperr.Biz(apperr.CodeASRSettingsInvalid, apperr.WithOp("transcript.settings.validate"))
	}
	var updated models.Settings
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		active, err := service.repository.HasActiveMeeting(ctx, tx)
		if err != nil {
			return err
		}
		if active {
			return apperr.Biz(apperr.CodeASRSettingsChangeBlocked, apperr.WithOp("transcript.settings.active_meeting"))
		}
		current, err := service.repository.GetSettingsForUpdate(ctx, tx)
		if err != nil {
			return err
		}
		updated, err = applySettingsChanges(current, input, service.clock.Now().UnixMilli())
		if err != nil {
			return err
		}
		return service.repository.UpdateASRSettings(ctx, tx, updated)
	})
	if err != nil {
		return ASRSettingsView{}, err
	}
	return mapSettingsView(updated), nil
}

// TestConnection 使用未保存草稿建立独立 session；不发送用户音频也不修改 SQLite。
func (service *SettingsService) TestConnection(ctx context.Context, credentials transcriptdomain.Credentials) (ConnectionProbeResult, error) {
	if service == nil || service.transcriber == nil || service.clock == nil {
		return ConnectionProbeResult{}, fmt.Errorf("ASR 连接测试服务未初始化")
	}
	if err := credentials.Validate(); err != nil {
		return ConnectionProbeResult{}, apperr.Biz(apperr.CodeASRSettingsInvalid, apperr.WithOp("transcript.settings.probe_validate"))
	}
	if _, err := credentials.Mode.Transport(); err != nil {
		return ConnectionProbeResult{}, apperr.Dependency(apperr.CodeASRProtocolIncompatible, err, apperr.WithOp("transcript.settings.probe_transport"))
	}
	startedAt := service.clock.Now()
	transcriber := service.transcriber(credentials)
	if transcriber == nil {
		return ConnectionProbeResult{}, fmt.Errorf("ASR 连接测试 adapter 不可用")
	}
	session, err := transcriber.Start(ctx, port.RealtimeTranscriptionRequest{MeetingID: "connection-probe", Format: port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}, StartSample: 0})
	if err != nil {
		return ConnectionProbeResult{}, err
	}
	if err = session.Stop(ctx); err != nil {
		return ConnectionProbeResult{}, err
	}
	latency := service.clock.Now().Sub(startedAt).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	return ConnectionProbeResult{Mode: credentials.Mode, ConnectionEstablished: true, RealAudioVerified: false, LatencyMS: latency}, nil
}

// applySettingsChanges 把显式字段动作应用到当前值；允许用户明确清空凭据，连接时再校验完整性。
func applySettingsChanges(current models.Settings, input SaveASRSettingsInput, updatedAt int64) (models.Settings, error) {
	mode := string(input.Mode)
	current.VolcAuthMode = &mode
	current.VolcAPIAppKey = applyCredentialChange(current.VolcAPIAppKey, input.AppID)
	current.VolcAPIAccessKey = applyCredentialChange(current.VolcAPIAccessKey, input.AccessToken)
	current.VolcAPIKey = applyCredentialChange(current.VolcAPIKey, input.APIKey)
	current.UpdatedAt = updatedAt
	return current, nil
}

// applyCredentialChange 执行一个凭据动作；输入已由边界校验。
func applyCredentialChange(current *string, change CredentialChange) *string {
	switch change.Action {
	case CredentialReplace:
		value := strings.TrimSpace(change.Value)
		return &value
	case CredentialClear:
		return nil
	default:
		return current
	}
}

// isValidCredentialChange 拒绝 replace 空值，以及 keep/clear 携带的模糊正文。
func isValidCredentialChange(change CredentialChange) bool {
	switch change.Action {
	case CredentialKeep, CredentialClear:
		return change.Value == ""
	case CredentialReplace:
		return strings.TrimSpace(change.Value) != ""
	default:
		return false
	}
}

// credentialsFromSettings 仅在 Go 服务内部恢复当前模式需要的明文凭据。
func credentialsFromSettings(settings models.Settings) transcriptdomain.Credentials {
	credentials := transcriptdomain.Credentials{Mode: transcriptdomain.AuthMode(valueOrEmpty(settings.VolcAuthMode)), AppID: valueOrEmpty(settings.VolcAPIAppKey), AccessToken: valueOrEmpty(settings.VolcAPIAccessKey), APIKey: valueOrEmpty(settings.VolcAPIKey)}
	return credentials
}

// mapSettingsView 只暴露是否配置和末四位掩码。
func mapSettingsView(settings models.Settings) ASRSettingsView {
	return ASRSettingsView{Mode: transcriptdomain.AuthMode(valueOrEmpty(settings.VolcAuthMode)), AppIDConfigured: settings.VolcAPIAppKey != nil, AppIDMask: maskCredential(settings.VolcAPIAppKey), AccessTokenConfigured: settings.VolcAPIAccessKey != nil, AccessTokenMask: maskCredential(settings.VolcAPIAccessKey), APIKeyConfigured: settings.VolcAPIKey != nil, APIKeyMask: maskCredential(settings.VolcAPIKey), UpdatedAt: settings.UpdatedAt}
}

// maskCredential 只保留末四位；短凭据只显示固定掩码，避免等价暴露。
func maskCredential(value *string) string {
	if value == nil || *value == "" {
		return ""
	}
	runes := []rune(*value)
	if len(runes) <= 4 {
		return "••••"
	}
	return "••••" + string(runes[len(runes)-4:])
}

// valueOrEmpty 安全读取可空设置字段。
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
