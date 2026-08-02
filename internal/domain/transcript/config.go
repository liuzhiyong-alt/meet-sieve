package transcript

import (
	"fmt"
	"strings"
)

// AuthMode 表示火山实时 ASR 的已确认鉴权方式。
type AuthMode string

const (
	// AuthModeLegacy 使用 App ID 与 Access Token 的 Seed 二进制协议。
	AuthModeLegacy AuthMode = "legacy"
	// AuthModeAPIKey 仅保存新控制台凭据；当前官方实时协议未证明该模式可用。
	AuthModeAPIKey AuthMode = "api_key"
)

// TransportMode 标识 session 实际使用的 wire transport，绝不包含凭据。
type TransportMode string

const (
	// TransportSeedV1 表示火山大模型流式语音识别 Seed v1 二进制协议。
	TransportSeedV1 TransportMode = "seed_v1"
)

// Credentials 保存当前模式建立实时 ASR 所需的明文凭据。
// 该值只允许在 Go 内存和受控 SQLite settings 中流转，不得进入 DTO、日志或错误正文。
type Credentials struct {
	Mode        AuthMode
	AppID       string
	AccessToken string
	APIKey      string
}

// Validate 严格校验当前鉴权模式的必填字段，另一模式凭据不参与当前连接。
func (credentials Credentials) Validate() error {
	switch credentials.Mode {
	case AuthModeLegacy:
		if strings.TrimSpace(credentials.AppID) == "" || strings.TrimSpace(credentials.AccessToken) == "" {
			return fmt.Errorf("legacy 实时转写凭据不完整")
		}
	case AuthModeAPIKey:
		if strings.TrimSpace(credentials.APIKey) == "" {
			return fmt.Errorf("API Key 实时转写凭据不完整")
		}
	default:
		return fmt.Errorf("实时转写鉴权方式无效")
	}
	return nil
}

// RuntimeConfig 是不可由用户页面修改的实时转写技术边界。
type RuntimeConfig struct {
	Endpoint              string
	ResourceID            string
	PCMQueueSamples       int64
	FinalQueueCapacity    int
	FinalPersistTimeoutMS int64
	TailTimeoutMS         int64
	ReconnectBackoffMS    []int64
}

// Validate 检查冻结的实时转写配置未被错误装配或静默降级。
func (config RuntimeConfig) Validate() error {
	if config.Endpoint == "" || config.ResourceID == "" || config.PCMQueueSamples != 32000 || config.FinalQueueCapacity != 128 || config.FinalPersistTimeoutMS != 5000 || config.TailTimeoutMS != 15000 {
		return fmt.Errorf("实时转写配置不符合冻结技术边界")
	}
	want := []int64{1000, 2000, 4000, 8000, 15000}
	if len(config.ReconnectBackoffMS) != len(want) {
		return fmt.Errorf("实时转写重连退避配置不正确")
	}
	for index, value := range want {
		if config.ReconnectBackoffMS[index] != value {
			return fmt.Errorf("实时转写重连退避配置不正确")
		}
	}
	return nil
}

// IsValid 返回鉴权方式是否在本 Step 冻结枚举中。
func (mode AuthMode) IsValid() bool { return mode == AuthModeLegacy || mode == AuthModeAPIKey }

// Transport 返回已获官方协议证明的 transport；未确认模式不得猜测或套用其他 API 的 Header。
func (mode AuthMode) Transport() (TransportMode, error) {
	switch mode {
	case AuthModeLegacy:
		return TransportSeedV1, nil
	case AuthModeAPIKey:
		return "", fmt.Errorf("API Key 尚无已确认的火山实时 WebSocket 鉴权协议")
	default:
		return "", fmt.Errorf("未知实时转写鉴权方式")
	}
}

// ProviderMillisecondsToSample 严格把 provider 相对毫秒映射到会话绝对样本边界。
func ProviderMillisecondsToSample(inputStartSample int64, providerMS int64, lastSentSample int64) (int64, error) {
	if inputStartSample < 0 || providerMS < 0 || lastSentSample < inputStartSample {
		return 0, fmt.Errorf("转写时间映射参数无效")
	}
	sample := inputStartSample + (providerMS*SampleRate+500)/1000
	if sample < inputStartSample || sample > lastSentSample {
		return 0, fmt.Errorf("转写时间范围超出已发送 PCM")
	}
	return sample, nil
}
