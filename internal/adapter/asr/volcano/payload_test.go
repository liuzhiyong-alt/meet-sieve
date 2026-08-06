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

// TestBuildHeaders 验证实时协议只发送新版 APP Key Header，不再携带旧版凭据头。
func TestBuildHeaders(t *testing.T) {
	headers, err := BuildHeaders(Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "api-key"}, DefaultResourceID, "connect-id")
	if err != nil {
		t.Fatalf("构造 APP Key Header 失败：%v", err)
	}
	if headers.Get("X-Api-Key") != "api-key" || headers.Get("X-Api-Resource-Id") != DefaultResourceID || headers.Get("X-Api-Connect-Id") != "connect-id" {
		t.Fatalf("APP Key Header 错误：%v", safeHeaderKeys(headers))
	}
	if headers.Get("X-Api-App-Key") != "" || headers.Get("X-Api-Access-Key") != "" {
		t.Fatalf("新版连接不得携带旧版凭据 Header：%v", safeHeaderKeys(headers))
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
	if audio["format"] != "pcm" || audio["rate"] != float64(16000) || request["model_name"] != "bigmodel" || request["enable_nonstream"] != true || request["enable_speaker_info"] != true || request["ssd_version"] != "200" || request["show_utterances"] != true {
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
	if len(events) != 1 || events[0].Type != port.TranscriptionFinal || events[0].ProviderResultID != "1:200" || events[0].StartSample != 35200 || events[0].EndSample != 57600 || events[0].SpeakerLabel != "1" {
		t.Fatalf("final 归一化结果错误：%+v", events)
	}
	replayed, err := ParseTranscriptionEvents(payload, "local-session", "provider-session", 10, 32000, 64000)
	if err != nil || len(replayed) != 1 || replayed[0].ProviderResultID != events[0].ProviderResultID {
		t.Fatalf("累计响应中的历史 final 必须复用稳定幂等键：events=%+v err=%v", replayed, err)
	}
}

// TestParseTranscriptionEventsAcceptsNumericSpeaker 验证优化流式响应的数字说话人标识可被归一化。
func TestParseTranscriptionEventsAcceptsNumericSpeaker(t *testing.T) {
	payload := []byte(`{"result":{"utterances":[{"definite":false,"start_time":0,"end_time":500,"text":"测试","speaker_id":2}]}}`)
	events, err := ParseTranscriptionEvents(payload, "local-session", "provider-session", 1, 0, 16000)
	if err != nil {
		t.Fatalf("解析数字说话人失败：%v", err)
	}
	if len(events) != 1 || events[0].SpeakerLabel != "2" || events[0].Type != port.TranscriptionPartial {
		t.Fatalf("数字说话人归一化错误：%+v", events)
	}
}

// TestParseTranscriptionEventsReadsAdditionsSpeaker 验证优化流式把说话人放在 additions 时仍建立稳定轨道。
func TestParseTranscriptionEventsReadsAdditionsSpeaker(t *testing.T) {
	payload := []byte(`{"result":{"utterances":[{"definite":true,"start_time":0,"end_time":500,"text":"测试","additions":{"speaker":"speaker_1"}}]}}`)
	events, err := ParseTranscriptionEvents(payload, "local-session", "provider-session", 1, 0, 16000)
	if err != nil {
		t.Fatalf("解析 additions 说话人失败：%v", err)
	}
	if len(events) != 1 || events[0].SpeakerLabel != "speaker_1" || events[0].ProviderResultID != "speaker_1:0" {
		t.Fatalf("additions 说话人归一化错误：%+v", events)
	}
}

// TestParseTranscriptionEventsReadsCompatibleSpeakerShapes 验证根字段和 additions JSON 字符串都能建立轨道。
func TestParseTranscriptionEventsReadsCompatibleSpeakerShapes(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{name: "root speaker", payload: `{"result":{"utterances":[{"definite":true,"start_time":0,"end_time":500,"text":"测试","speaker":"speaker_2"}]}}`, want: "speaker_2"},
		{name: "additions speaker id", payload: `{"result":{"utterances":[{"definite":true,"start_time":0,"end_time":500,"text":"测试","additions":{"speaker_id":3}}]}}`, want: "3"},
		{name: "nested speaker info", payload: `{"result":{"utterances":[{"definite":true,"start_time":0,"end_time":500,"text":"测试","additions":{"speaker_info":{"speaker_id":"speaker_4"}}}]}}`, want: "speaker_4"},
		{name: "encoded additions", payload: `{"result":{"utterances":[{"definite":true,"start_time":0,"end_time":500,"text":"测试","additions":"{\"spk_id\":5}"}]}}`, want: "5"},
		{name: "official additions wins", payload: `{"result":{"utterances":[{"definite":true,"start_time":0,"end_time":500,"text":"测试","speaker_id":"legacy","additions":{"speaker":"speaker_6"}}]}}`, want: "speaker_6"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := ParseTranscriptionEvents([]byte(test.payload), "local-session", "provider-session", 1, 0, 16000)
			if err != nil {
				t.Fatalf("解析说话人字段失败：%v", err)
			}
			if len(events) != 1 || events[0].SpeakerLabel != test.want {
				t.Fatalf("说话人归一化错误：%+v", events)
			}
		})
	}
}

// TestParseTranscriptionEventsKeepsMissingSpeakerUnlabeled 验证供应商缺字段时不会伪造 speaker_0。
func TestParseTranscriptionEventsKeepsMissingSpeakerUnlabeled(t *testing.T) {
	payload := []byte(`{"result":{"utterances":[{"definite":true,"start_time":100,"end_time":600,"text":"测试"}]}}`)
	events, err := ParseTranscriptionEvents(payload, "local-session", "provider-session", 2, 0, 16000)
	if err != nil {
		t.Fatalf("解析无说话人 final 失败：%v", err)
	}
	if len(events) != 1 || events[0].SpeakerLabel != "" || events[0].ProviderResultID != "unlabeled:100" {
		t.Fatalf("无说话人 final 不应伪造匿名轨道：%+v", events)
	}
}

// TestParseTranscriptionEventsClampsNearWriteBoundary 验证 provider 的毫秒取整误差不会终止整场 ASR。
func TestParseTranscriptionEventsClampsNearWriteBoundary(t *testing.T) {
	payload := []byte(`{"result":{"utterances":[{"definite":true,"start_time":500,"end_time":1100,"text":"测试"}]}}`)
	events, err := parseTranscriptionEvents(payload, "local-session", "provider-session", 1, 0, 16000, maxTimestampToleranceSamples)
	if err != nil {
		t.Fatalf("边界容忍范围内的事件不应失败：%v", err)
	}
	if len(events) != 1 || events[0].EndSample != 16000 {
		t.Fatalf("事件终点必须钳制到实际写入边界：%+v", events)
	}
}

// TestParseTranscriptionEventsRejectsFarBeyondWriteBoundary 验证明显超前的 provider 时间不会写入未来 PCM。
func TestParseTranscriptionEventsRejectsFarBeyondWriteBoundary(t *testing.T) {
	payload := []byte(`{"result":{"utterances":[{"definite":true,"start_time":500,"end_time":1300,"text":"测试"}]}}`)
	if _, err := parseTranscriptionEvents(payload, "local-session", "provider-session", 1, 0, 16000, maxTimestampToleranceSamples); err == nil {
		t.Fatal("明显超前的 provider 时间必须失败")
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
