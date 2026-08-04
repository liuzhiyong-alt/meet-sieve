// Package fileflash 实现火山大模型录音文件极速版的稳定业务 adapter。
package fileflash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	domainaudio "meet-sieve/internal/domain/audio"
	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

const (
	// Endpoint 是极速文件识别唯一允许访问的生产入口。
	Endpoint = "https://openspeech.bytedance.com/api/v3/auc/bigmodel/recognize/flash"
	// ResourceID 是极速版大模型固定资源标识。
	ResourceID       = "volc.bigasr.auc_turbo"
	maxWAVBytes      = 18 * 1024 * 1024
	maxResponseBytes = 2 * 1024 * 1024
)

// Adapter 实现 provider 无关的 FileTranscriber port。
type Adapter struct {
	credentials transcriptdomain.Credentials
	endpoint    string
	client      *http.Client
}

// CredentialLoader 在每次显式文件转写请求前读取当前安全凭据。
type CredentialLoader func(context.Context) (transcriptdomain.Credentials, error)

// DynamicAdapter 延迟读取凭据，避免应用装配阶段因尚未配置 ASR 而失败。
type DynamicAdapter struct {
	load CredentialLoader
}

// NewDynamicAdapter 创建不缓存明文凭据的文件转写 adapter。
func NewDynamicAdapter(loader CredentialLoader) *DynamicAdapter {
	return &DynamicAdapter{load: loader}
}

// Transcribe 读取当前凭据后执行唯一一次文件转写请求。
func (adapter *DynamicAdapter) Transcribe(ctx context.Context, request port.FileTranscriptionRequest) (port.FileTranscriptionResult, error) {
	if adapter == nil || adapter.load == nil {
		return port.FileTranscriptionResult{}, fmt.Errorf("文件转写凭据提供器不可用")
	}
	credentials, err := adapter.load(ctx)
	if err != nil {
		return port.FileTranscriptionResult{}, err
	}
	return NewAdapter(credentials).Transcribe(ctx, request)
}

// NewAdapter 创建只访问火山固定 HTTPS endpoint 的文件识别 adapter。
func NewAdapter(credentials transcriptdomain.Credentials) *Adapter {
	return newAdapter(credentials, Endpoint, &http.Client{Timeout: 180 * time.Second})
}

// newAdapter 仅供同包契约测试替换为本机 TLS server。
func newAdapter(credentials transcriptdomain.Credentials, endpoint string, client *http.Client) *Adapter {
	if client != nil {
		copyClient := *client
		if copyClient.Timeout <= 0 {
			copyClient.Timeout = 180 * time.Second
		}
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error {
			return fmt.Errorf("文件转写禁止重定向")
		}
		client = &copyClient
	}
	return &Adapter{credentials: credentials, endpoint: endpoint, client: client}
}

// Transcribe 读取并校验可信 WAV，然后返回规范化的相对样本结果。
func (adapter *Adapter) Transcribe(ctx context.Context, request port.FileTranscriptionRequest) (port.FileTranscriptionResult, error) {
	if err := adapter.validate(request); err != nil {
		return port.FileTranscriptionResult{}, err
	}
	wav, err := readVerifiedWAV(request)
	if err != nil {
		return port.FileTranscriptionResult{}, err
	}
	body, err := buildRequestBody(request, wav)
	if err != nil {
		return port.FileTranscriptionResult{}, apperr.Dependency(apperr.CodeGapAudioInvalid, err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, adapter.endpoint, bytes.NewReader(body))
	if err != nil {
		return port.FileTranscriptionResult{}, apperr.Dependency(apperr.CodeGapTranscriptionRejected, err)
	}
	setHeaders(httpRequest, adapter.credentials, request.RequestID)
	response, err := adapter.client.Do(httpRequest)
	if err != nil {
		return port.FileTranscriptionResult{}, mapRequestError(err)
	}
	defer response.Body.Close()
	return parseResponse(response, request)
}

// validate 检查 adapter 配置与业务请求未越过冻结边界。
func (adapter *Adapter) validate(request port.FileTranscriptionRequest) error {
	if adapter == nil || adapter.client == nil || strings.TrimSpace(adapter.endpoint) == "" {
		return fmt.Errorf("文件转写 adapter 配置无效")
	}
	if err := adapter.credentials.Validate(); err != nil {
		return apperr.Biz(apperr.CodeASRSettingsInvalid)
	}
	if adapter.credentials.Mode != transcriptdomain.AuthModeAPIKey {
		return apperr.Biz(apperr.CodeASRSettingsInvalid)
	}
	if request.MeetingID == "" || len(request.RequestID) != 36 || !filepath.IsAbs(request.AudioPath) ||
		len(request.AudioSHA256) != 64 || request.SampleRate != domainaudio.SampleRate ||
		request.CoreStartSample < request.AudioStartSample || request.CoreEndSample <= request.CoreStartSample ||
		request.AudioEndSample < request.CoreEndSample || request.AudioStartSample < 0 {
		return apperr.Biz(apperr.CodeGapAudioInvalid)
	}
	return nil
}

// readVerifiedWAV 在编码前校验大小、哈希、格式和请求声明的样本范围。
func readVerifiedWAV(request port.FileTranscriptionRequest) ([]byte, error) {
	wav, err := os.ReadFile(request.AudioPath)
	if err != nil {
		return nil, apperr.Dependency(apperr.CodeGapAudioUnavailable, err)
	}
	if len(wav) > maxWAVBytes {
		return nil, apperr.Biz(apperr.CodeGapRequestTooLarge)
	}
	digest := sha256.Sum256(wav)
	if hex.EncodeToString(digest[:]) != request.AudioSHA256 {
		return nil, apperr.Biz(apperr.CodeGapAudioInvalid)
	}
	pcm, err := domainaudio.DecodeCanonicalWAV(wav)
	if err != nil {
		return nil, apperr.Dependency(apperr.CodeGapAudioInvalid, err)
	}
	if int64(len(pcm)/2) != request.AudioEndSample-request.AudioStartSample {
		return nil, apperr.Biz(apperr.CodeGapAudioInvalid)
	}
	return wav, nil
}

// buildRequestBody 只构造极速文件接口允许的本地 Base64 音频请求。
func buildRequestBody(request port.FileTranscriptionRequest, wav []byte) ([]byte, error) {
	payload := map[string]any{
		"user": map[string]any{"uid": request.MeetingID},
		"audio": map[string]any{
			"format": "wav", "data": base64.StdEncoding.EncodeToString(wav),
			"rate": domainaudio.SampleRate, "bits": domainaudio.BitDepth, "channel": domainaudio.Channels,
		},
		"request": map[string]any{
			"model_name": "bigmodel", "enable_itn": true, "enable_punc": true, "show_utterances": true,
		},
	}
	return json.Marshal(payload)
}

// setHeaders 使用统一 APP Key 构造请求 Header，不把凭据返回业务层。
func setHeaders(request *http.Request, credentials transcriptdomain.Credentials, requestID string) {
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Api-Resource-Id", ResourceID)
	request.Header.Set("X-Api-Request-Id", requestID)
	request.Header.Set("X-Api-Sequence", "-1")
	request.Header.Set("X-Api-Key", credentials.APIKey)
}

type providerResponse struct {
	Result struct {
		Utterances []providerUtterance `json:"utterances"`
	} `json:"result"`
}

type providerUtterance struct {
	Text      string `json:"text"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	SpeakerID string `json:"speaker_id"`
}

// parseResponse 根据稳定业务状态解析有限响应，不保留 Header 或原始正文。
func parseResponse(response *http.Response, request port.FileTranscriptionRequest) (port.FileTranscriptionResult, error) {
	status := response.Header.Get("X-Api-Status-Code")
	logSuffix := suffix(response.Header.Get("X-Tt-Logid"), 8)
	if status == "20000003" {
		return port.FileTranscriptionResult{ProviderLogIDSuffix: logSuffix, NoSpeech: true}, nil
	}
	if response.StatusCode != http.StatusOK || status != "20000000" {
		return port.FileTranscriptionResult{}, apperr.Dependency(apperr.CodeGapTranscriptionRejected, fmt.Errorf("provider status rejected"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes || !utf8.Valid(body) {
		return port.FileTranscriptionResult{}, apperr.Dependency(apperr.CodeGapTranscriptionRejected, fmt.Errorf("provider response invalid"))
	}
	var provider providerResponse
	if err := json.Unmarshal(body, &provider); err != nil {
		return port.FileTranscriptionResult{}, apperr.Dependency(apperr.CodeGapTranscriptionRejected, err)
	}
	segments, err := normalizeSegments(provider.Result.Utterances, request)
	if err != nil {
		return port.FileTranscriptionResult{}, apperr.Dependency(apperr.CodeGapTranscriptionRejected, err)
	}
	return port.FileTranscriptionResult{ProviderLogIDSuffix: logSuffix, Segments: segments}, nil
}

// normalizeSegments 把 provider 毫秒转换为相对切片起点的整数样本范围。
func normalizeSegments(values []providerUtterance, request port.FileTranscriptionRequest) ([]port.FileTranscriptionSegment, error) {
	segments := make([]port.FileTranscriptionSegment, 0, len(values))
	lastEnd := int64(0)
	maxSamples := request.AudioEndSample - request.AudioStartSample
	for _, value := range values {
		start := (value.StartTime*int64(request.SampleRate) + 500) / 1000
		end := (value.EndTime*int64(request.SampleRate) + 500) / 1000
		if strings.TrimSpace(value.Text) == "" || value.StartTime < 0 || end <= start || start < lastEnd || end > maxSamples {
			return nil, fmt.Errorf("provider utterance range invalid")
		}
		segments = append(segments, port.FileTranscriptionSegment{
			Text: value.Text, SpeakerID: value.SpeakerID, StartSample: start, EndSample: end,
		})
		lastEnd = end
	}
	return segments, nil
}

// mapRequestError 把取消、超时和其他网络失败映射为安全稳定错误。
func mapRequestError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return apperr.Dependency(apperr.CodeGapTranscriptionTimeout, err)
	}
	if errors.Is(err, context.Canceled) {
		return apperr.Dependency(apperr.CodeGapTranscriptionCancelled, err)
	}
	return apperr.Dependency(apperr.CodeGapTranscriptionRejected, err)
}

// suffix 只保留 provider 排障标识的末尾有限字符。
func suffix(value string, length int) string {
	if len(value) <= length {
		return value
	}
	return value[len(value)-length:]
}
