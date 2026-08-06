//go:build asrreal

package asr_test

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	volcanoasr "meet-sieve/internal/adapter/asr/volcano"
	"meet-sieve/internal/adapter/audio/malgo"
	audiodomain "meet-sieve/internal/domain/audio"
	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
)

const realFileProbeSamples = 20 * audiodomain.SampleRate

// TestRealVolcanoASRWithDefaultMicrophone 使用真实默认麦克风和火山账号验证至少一条 final。
func TestRealVolcanoASRWithDefaultMicrophone(t *testing.T) {
	if os.Getenv("MEETSIEVE_ASR_REAL") != "1" {
		t.Fatal("真实 ASR 测试必须显式设置 MEETSIEVE_ASR_REAL=1")
	}
	credentials := realCredentials(t)
	duration := realCaptureDuration(t)
	enumerator := malgo.NewEnumerator()
	devices, err := enumerator.ListInputDevices(context.Background())
	if err != nil || len(devices) == 0 {
		t.Fatalf("没有可用真实麦克风：count=%d err=%v", len(devices), err)
	}
	device := devices[0]
	for _, candidate := range devices {
		if candidate.IsDefault {
			device = candidate
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration+30*time.Second)
	defer cancel()
	stream, err := enumerator.Start(ctx, device.ID, port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1})
	if err != nil {
		t.Fatalf("打开真实麦克风失败：%v", err)
	}
	defer stream.Stop(context.Background())
	adapter := volcanoasr.NewAdapter(volcanoasr.AdapterConfig{Endpoint: volcanoasr.DefaultEndpoint, ResourceID: volcanoasr.DefaultResourceID, Credentials: credentials, ConnectTimeout: 10 * time.Second}, identity.NewUUIDGenerator())
	session, err := adapter.Start(ctx, port.RealtimeTranscriptionRequest{MeetingID: identity.NewUUIDGenerator().New(), Format: port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}})
	if err != nil {
		t.Fatalf("建立真实火山实时 ASR 连接失败：%v", err)
	}
	results := make(chan realEventSummary, 1)
	go observeRealEvents(session.Events(), results)
	t.Logf("请在接下来的 %s 内对默认麦克风说一段完整中文句子；测试不会打印转写正文", duration)
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	captureRealFrames(t, ctx, deadline.C, stream, session)
	stopContext, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err = session.Stop(stopContext); err != nil {
		t.Fatalf("提交真实 ASR 尾帧失败：%v", err)
	}
	assertRealSpeakerSummary(t, <-results, true)
}

// TestRealVolcanoASRWithRecordedWAV 使用真实录音回放验证火山协议至少返回一条识别结果。
func TestRealVolcanoASRWithRecordedWAV(t *testing.T) {
	if os.Getenv("MEETSIEVE_ASR_REAL") != "1" {
		t.Fatal("真实 ASR 测试必须显式设置 MEETSIEVE_ASR_REAL=1")
	}
	path := os.Getenv("MEETSIEVE_ASR_WAV")
	if path == "" {
		t.Fatal("MEETSIEVE_ASR_WAV 必须指向待验证的标准录音")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取真实录音失败：%v", err)
	}
	pcm, err := audiodomain.DecodeCanonicalWAV(content)
	if err != nil {
		t.Fatalf("真实录音格式无效：%v", err)
	}
	if len(pcm) > realFileProbeSamples*2 {
		pcm = pcm[:realFileProbeSamples*2]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	adapter := volcanoasr.NewAdapter(volcanoasr.AdapterConfig{
		Endpoint: volcanoasr.DefaultEndpoint, ResourceID: volcanoasr.DefaultResourceID,
		Credentials: realCredentials(t), ConnectTimeout: 10 * time.Second,
	}, identity.NewUUIDGenerator())
	session, err := adapter.Start(ctx, port.RealtimeTranscriptionRequest{
		MeetingID: identity.NewUUIDGenerator().New(),
		Format:    port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1},
	})
	if err != nil {
		t.Fatalf("建立真实火山实时 ASR 连接失败：%v", err)
	}
	results := make(chan realEventSummary, 1)
	go observeRealEvents(session.Events(), results)
	playPCMInRealtime(t, ctx, session, pcm)
	stopContext, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err = session.Stop(stopContext); err != nil {
		t.Fatalf("提交真实 ASR 尾帧失败：%v", err)
	}
	assertRealSpeakerSummary(t, <-results, false)
}

// realCredentials 从环境读取当前测试模式所需凭据，不打印值。
func realCredentials(t *testing.T) transcriptdomain.Credentials {
	t.Helper()
	credentials := transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: os.Getenv("MEETSIEVE_VOLC_API_KEY")}
	if err := credentials.Validate(); err != nil {
		t.Fatalf("真实火山凭据环境变量不完整：%v", err)
	}
	return credentials
}

// realCaptureDuration 读取 3～60 秒显式采集时长，默认 10 秒。
func realCaptureDuration(t *testing.T) time.Duration {
	t.Helper()
	seconds := 10
	if value := os.Getenv("MEETSIEVE_ASR_REAL_SECONDS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 3 || parsed > 60 {
			t.Fatal("MEETSIEVE_ASR_REAL_SECONDS 必须为 3～60")
		}
		seconds = parsed
	}
	return time.Duration(seconds) * time.Second
}

// captureRealFrames 持续转发真实 PCM，直到采集时长结束。
func captureRealFrames(t *testing.T, ctx context.Context, deadline <-chan time.Time, stream port.AudioStream, session port.RealtimeTranscriptionSession) {
	t.Helper()
	for {
		select {
		case <-deadline:
			return
		default:
		}
		frame, err := stream.ReadFrames(ctx)
		if err != nil {
			t.Fatalf("读取真实 PCM 失败：%v", err)
		}
		if err = session.WriteFrame(ctx, frame); err != nil {
			t.Fatalf("发送真实 PCM 失败：%v", err)
		}
	}
}

type realEventSummary struct {
	PartialCount  int
	FinalCount    int
	SpeakerLabels map[string]struct{}
}

// observeRealEvents 统计脱敏识别事实，不打印正文或 provider 原始错误。
func observeRealEvents(events <-chan port.TranscriptionEvent, results chan<- realEventSummary) {
	summary := realEventSummary{SpeakerLabels: make(map[string]struct{})}
	for event := range events {
		switch event.Type {
		case port.TranscriptionPartial:
			summary.PartialCount++
		case port.TranscriptionFinal:
			summary.FinalCount++
		}
		if event.SpeakerLabel != "" {
			summary.SpeakerLabels[event.SpeakerLabel] = struct{}{}
		}
	}
	results <- summary
}

// assertRealSpeakerSummary 同时验证转写和真实匿名 Speaker，防止用默认 speaker_0 伪造通过。
func assertRealSpeakerSummary(t *testing.T, summary realEventSummary, requireFinal bool) {
	t.Helper()
	if requireFinal && summary.FinalCount == 0 {
		t.Fatal("真实音频未收到 final；请确认说话清晰、账号资源可用后重试")
	}
	if !requireFinal && summary.FinalCount+summary.PartialCount == 0 {
		t.Fatal("真实录音未收到任何 partial/final")
	}
	if len(summary.SpeakerLabels) == 0 {
		t.Fatal("火山响应未提供任何可解析 Speaker 标签；请结合 X-Tt-Logid 核对资源权限和字段路径")
	}
	t.Logf("脱敏 ASR 取证：partial=%d final=%d speaker_tracks=%d", summary.PartialCount, summary.FinalCount, len(summary.SpeakerLabels))
}

// playPCMInRealtime 按 200ms 帧节奏回放标准 PCM，避免用突发写入掩盖流式协议问题。
func playPCMInRealtime(t *testing.T, ctx context.Context, session port.RealtimeTranscriptionSession, pcm []byte) {
	t.Helper()
	const frameSamples = audiodomain.SampleRate / 5
	for startSample := int64(0); startSample*2 < int64(len(pcm)); startSample += frameSamples {
		endSample := startSample + frameSamples
		if endSample*2 > int64(len(pcm)) {
			endSample = int64(len(pcm) / 2)
		}
		frame := port.AudioFrame{StartSample: startSample, PCM: pcm[startSample*2 : endSample*2]}
		if err := session.WriteFrame(ctx, frame); err != nil {
			t.Fatalf("回放真实 PCM 失败：%v", err)
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatalf("回放真实 PCM 超时：%v", ctx.Err())
		case <-timer.C:
		}
	}
}
