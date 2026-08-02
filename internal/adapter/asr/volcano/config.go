package volcano

import (
	"fmt"
	"net/http"
	"strings"

	transcriptdomain "meet-sieve/internal/domain/transcript"
)

const (
	// DefaultEndpoint 是火山大模型双向流式 Seed ASR 的固定入口。
	DefaultEndpoint = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel"
	// DefaultResourceID 是当前方案使用的豆包流式识别时长版资源。
	DefaultResourceID = "volc.seedasr.sauc.duration"
)

// Credentials 是领域凭据值的 adapter 别名，避免厂商层重新定义业务鉴权语义。
type Credentials = transcriptdomain.Credentials

// BuildHeaders 构造仅用于 WebSocket 握手的认证 Header。
func BuildHeaders(credentials Credentials, resourceID string, connectID string) (http.Header, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(resourceID) == "" || strings.TrimSpace(connectID) == "" {
		return nil, fmt.Errorf("实时转写握手参数不完整")
	}
	if credentials.Mode != transcriptdomain.AuthModeLegacy {
		return nil, fmt.Errorf("API Key 尚无已确认的火山实时 WebSocket 鉴权协议")
	}
	headers := http.Header{}
	headers.Set("X-Api-Resource-Id", resourceID)
	headers.Set("X-Api-Connect-Id", connectID)
	headers.Set("X-Api-App-Key", credentials.AppID)
	headers.Set("X-Api-Access-Key", credentials.AccessToken)
	return headers, nil
}
