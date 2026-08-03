package fileflash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainaudio "meet-sieve/internal/domain/audio"
	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

// TestAdapter_UsesFixedProtocolAndNormalizesSegments 验证请求协议、音频字节和样本映射。
func TestAdapter_UsesFixedProtocolAndNormalizesSegments(t *testing.T) {
	wav, path, digest := writeTestWAV(t, 16000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v3/auc/bigmodel/recognize/flash" {
			t.Errorf("请求目标错误：%s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("X-Api-Resource-Id") != ResourceID || request.Header.Get("X-Api-Request-Id") != "11111111-1111-4111-8111-111111111111" || request.Header.Get("X-Api-Sequence") != "-1" {
			t.Errorf("固定 Header 错误：%v", request.Header)
		}
		if request.Header.Get("X-Api-App-Key") != "app" || request.Header.Get("X-Api-Access-Key") != "token" {
			t.Error("legacy 鉴权 Header 缺失")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("读取请求失败：%v", err)
			return
		}
		var payload struct {
			Audio struct {
				Data    string `json:"data"`
				Format  string `json:"format"`
				Rate    int    `json:"rate"`
				Bits    int    `json:"bits"`
				Channel int    `json:"channel"`
			} `json:"audio"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("请求 JSON 无效：%v", err)
			return
		}
		decoded, err := base64.StdEncoding.DecodeString(payload.Audio.Data)
		if err != nil || string(decoded) != string(wav) {
			t.Error("Base64 音频与本地 WAV 不一致")
		}
		if payload.Audio.Format != "wav" || payload.Audio.Rate != 16000 || payload.Audio.Bits != 16 || payload.Audio.Channel != 1 {
			t.Errorf("固定音频参数错误：%+v", payload.Audio)
		}
		writer.Header().Set("X-Api-Status-Code", "20000000")
		writer.Header().Set("X-Tt-Logid", "private-log-id-12345678")
		_, _ = writer.Write([]byte(`{"result":{"utterances":[{"text":"测试","start_time":0,"end_time":1000,"speaker_id":"1"}]}}`))
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	client.Timeout = 5 * time.Second
	adapter := newAdapter(transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeLegacy, AppID: "app", AccessToken: "token"}, server.URL+"/api/v3/auc/bigmodel/recognize/flash", client)
	result, err := adapter.Transcribe(context.Background(), port.FileTranscriptionRequest{
		MeetingID: "meeting", RequestID: "11111111-1111-4111-8111-111111111111", AudioPath: path,
		AudioSHA256: digest, CoreStartSample: 0, CoreEndSample: 16000,
		AudioStartSample: 0, AudioEndSample: 16000, SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("文件转写失败：%v", err)
	}
	if result.NoSpeech || result.ProviderLogIDSuffix != "12345678" || len(result.Segments) != 1 {
		t.Fatalf("规范化结果错误：%+v", result)
	}
	segment := result.Segments[0]
	if segment.Text != "测试" || segment.StartSample != 0 || segment.EndSample != 16000 || segment.SpeakerID != "1" {
		t.Fatalf("分句映射错误：%+v", segment)
	}
}

// TestAdapter_TreatsNoSpeechAsSuccessfulResult 验证静音状态不会伪装成依赖失败。
func TestAdapter_TreatsNoSpeechAsSuccessfulResult(t *testing.T) {
	_, path, digest := writeTestWAV(t, 1600)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Api-Status-Code", "20000003")
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	adapter := newAdapter(transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "api-key"}, server.URL, server.Client())
	result, err := adapter.Transcribe(context.Background(), port.FileTranscriptionRequest{
		MeetingID: "meeting", RequestID: "22222222-2222-4222-8222-222222222222", AudioPath: path,
		AudioSHA256: digest, CoreStartSample: 0, CoreEndSample: 1600,
		AudioStartSample: 0, AudioEndSample: 1600, SampleRate: 16000,
	})
	if err != nil || !result.NoSpeech || len(result.Segments) != 0 {
		t.Fatalf("静音结果错误：result=%+v err=%v", result, err)
	}
}

// TestParseResponse_MapsOfficialFailuresToStableError 验证官方失败状态不泄漏正文并统一为可重试依赖错误。
func TestParseResponse_MapsOfficialFailuresToStableError(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"45000001", "45000002", "45000151", "55000031", "99999999", ""} {
		status := status
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			response := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"X-Api-Status-Code": []string{status}, "X-Tt-Logid": []string{"private-full-log-id"}},
				Body:       io.NopCloser(strings.NewReader(`{"private":"body"}`)),
			}
			_, err := parseResponse(response, port.FileTranscriptionRequest{})
			var appError *apperr.AppError
			if !errors.As(err, &appError) || appError.ErrorCode != apperr.CodeGapTranscriptionRejected.ErrorCode {
				t.Fatalf("状态 %q 映射错误：%v", status, err)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatalf("错误泄漏 provider 内容：%v", err)
			}
		})
	}
}

// TestParseResponse_RejectsOversizeAndInvalidRanges 验证响应体上限和分句单调边界。
func TestParseResponse_RejectsOversizeAndInvalidRanges(t *testing.T) {
	t.Parallel()
	request := port.FileTranscriptionRequest{SampleRate: 16000, AudioStartSample: 0, AudioEndSample: 16000}
	tests := []struct {
		name string
		body []byte
	}{
		{name: "oversize", body: bytes.Repeat([]byte("x"), maxResponseBytes+1)},
		{name: "invalid_utf8", body: []byte{0xff}},
		{name: "backward", body: []byte(`{"result":{"utterances":[{"text":"一","start_time":500,"end_time":700},{"text":"二","start_time":600,"end_time":800}]}}`)},
		{name: "outside", body: []byte(`{"result":{"utterances":[{"text":"越界","start_time":0,"end_time":1001}]}}`)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Api-Status-Code": []string{"20000000"}}, Body: io.NopCloser(bytes.NewReader(test.body))}
			if _, err := parseResponse(response, request); err == nil {
				t.Fatalf("非法响应必须失败：%s", test.name)
			}
		})
	}
}

// writeTestWAV 生成真实固定格式 WAV 并返回内容、路径和 SHA-256。
func writeTestWAV(t *testing.T, samples int64) ([]byte, string, string) {
	t.Helper()
	header, err := domainaudio.EncodeCanonicalWAVHeader(samples)
	if err != nil {
		t.Fatalf("生成 WAV header 失败：%v", err)
	}
	wav := append(header, make([]byte, samples*2)...)
	path := filepath.Join(t.TempDir(), "gap.wav")
	if err := os.WriteFile(path, wav, 0o600); err != nil {
		t.Fatalf("写入测试 WAV 失败：%v", err)
	}
	hash := sha256.Sum256(wav)
	return wav, path, hex.EncodeToString(hash[:])
}
