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
	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
)

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
	finals := make(chan struct{}, 1)
	go observeRealEvents(session.Events(), finals)
	t.Logf("请在接下来的 %s 内对默认麦克风说一段完整中文句子；测试不会打印转写正文", duration)
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	captureRealFrames(t, ctx, deadline.C, stream, session)
	stopContext, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err = session.Stop(stopContext); err != nil {
		t.Fatalf("提交真实 ASR 尾帧失败：%v", err)
	}
	select {
	case <-finals:
	case <-time.After(time.Second):
		t.Fatal("真实音频未收到任何 final；请确认说话清晰、账号资源可用后重试")
	}
}

// realCredentials 从环境读取当前测试模式所需凭据，不打印值。
func realCredentials(t *testing.T) transcriptdomain.Credentials {
	t.Helper()
	credentials := transcriptdomain.Credentials{Mode: transcriptdomain.AuthMode(os.Getenv("MEETSIEVE_VOLC_AUTH_MODE")), AppID: os.Getenv("MEETSIEVE_VOLC_APP_ID"), AccessToken: os.Getenv("MEETSIEVE_VOLC_ACCESS_TOKEN"), APIKey: os.Getenv("MEETSIEVE_VOLC_API_KEY")}
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

// observeRealEvents 只统计 final，不打印敏感正文或 provider 原始错误。
func observeRealEvents(events <-chan port.TranscriptionEvent, finals chan<- struct{}) {
	for event := range events {
		if event.Type == port.TranscriptionFinal {
			select {
			case finals <- struct{}{}:
			default:
			}
		}
	}
}
