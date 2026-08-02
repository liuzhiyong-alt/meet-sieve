package volcano

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/port"
)

// TestBuildHeaders 验证实时协议只发送官方明确支持的 legacy Header。
func TestBuildHeaders(t *testing.T) {
	legacy, err := BuildHeaders(Credentials{Mode: transcriptdomain.AuthModeLegacy, AppID: "app-id", AccessToken: "access-token"}, DefaultResourceID, "connect-id")
	if err != nil {
		t.Fatalf("构造 legacy Header 失败：%v", err)
	}
	if legacy.Get("X-Api-App-Key") != "app-id" || legacy.Get("X-Api-Access-Key") != "access-token" || legacy.Get("X-Api-Key") != "" {
		t.Fatalf("legacy Header 错误：%v", safeHeaderKeys(legacy))
	}
	apiKey, err := BuildHeaders(Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "api-key"}, DefaultResourceID, "connect-id")
	if err == nil || apiKey != nil {
		t.Fatalf("未经官方实时协议证明的 API Key 不得构造握手 Header：%v", safeHeaderKeys(apiKey))
	}
}

// TestBuildInitialPayload 验证用户无法通过请求改变固定 PCM 格式与模型字段。
func TestBuildInitialPayload(t *testing.T) {
	data, err := BuildInitialPayload(port.RealtimeTranscriptionRequest{MeetingID: "meeting-id", StartSample: 0, Format: port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}})
	if err != nil {
		t.Fatalf("构造初始化请求失败：%v", err)
	}
	var payload map[string]any
	if err = json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("解析初始化请求失败：%v", err)
	}
	audio := payload["audio"].(map[string]any)
	request := payload["request"].(map[string]any)
	if audio["format"] != "pcm" || audio["rate"] != float64(16000) || request["model_name"] != "bigmodel" || request["show_utterances"] != true {
		t.Fatalf("初始化固定字段错误：%s", data)
	}
}

// TestParseTranscriptionEvents 使用脱敏官方形状验证 definite final 与绝对样本映射。
func TestParseTranscriptionEvents(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "..", "tests", "fixtures", "asr", "volcano", "legacy_final_response.json")
	payload, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("读取火山 fixture 失败：%v", err)
	}
	events, err := ParseTranscriptionEvents(payload, "local-session", "provider-session", 9, 32000, 64000)
	if err != nil {
		t.Fatalf("解析火山 fixture 失败：%v", err)
	}
	if len(events) != 1 || events[0].Type != port.TranscriptionFinal || events[0].ProviderResultID != "9:0" || events[0].StartSample != 35200 || events[0].EndSample != 57600 || events[0].SpeakerLabel != "1" {
		t.Fatalf("final 归一化结果错误：%+v", events)
	}
}

// safeHeaderKeys 仅向断言输出 Header 名，不允许测试失败泄露凭据值。
func safeHeaderKeys(headers http.Header) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	return keys
}
