package agent_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/port"
	agentservice "meet-sieve/internal/service/agent"
)

func TestWakeWordTestCountsOnlyDistinctConsecutiveFinals(t *testing.T) {
	stream := &wakeTestStream{frames: []port.AudioFrame{{PCM: []byte{1, 0}, StartSample: 0}}}
	session := newWakeTestSession()
	service := agentservice.NewWakeWordTestService(agentservice.WakeWordTestDependencies{
		Guard: &wakeTestGuard{}, Capture: &wakeTestCapture{stream: stream},
		Credentials: validWakeTestCredentials,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber {
			return &wakeTestTranscriber{session: session}
		},
		WakeWord: func(context.Context) (string, error) { return "AI 助手", nil },
		Timeout:  time.Second,
	})

	started, err := service.Start(context.Background())
	if err != nil || started.State != agentservice.WakeWordTestRunning {
		t.Fatalf("启动真实唤醒测试失败: state=%+v err=%v", started, err)
	}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionPartial, ProviderResultID: "partial", Text: "AI 助手"}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionFinal, ProviderResultID: "one", Text: "AI 助手。"}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionFinal, ProviderResultID: "one", Text: "AI 助手。"}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionFinal, ProviderResultID: "miss", Text: "普通内容"}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionFinal, ProviderResultID: "two", Text: "AI 助手"}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionFinal, ProviderResultID: "three", Text: "AI 助手！"}
	session.events <- port.TranscriptionEvent{Type: port.TranscriptionFinal, ProviderResultID: "four", Text: "AI 助手。"}

	state := waitWakeTestState(t, service, agentservice.WakeWordTestPassed)
	if state.Matched != 3 || state.Required != 3 {
		t.Fatalf("连续三次计数不正确: %+v", state)
	}
	if stream.stopCount() != 1 || session.stopCount() != 1 {
		t.Fatalf("完成后必须释放采集和 ASR: capture=%d asr=%d", stream.stopCount(), session.stopCount())
	}
}

func TestWakeWordTestBlocksDuringActiveMeeting(t *testing.T) {
	service := agentservice.NewWakeWordTestService(agentservice.WakeWordTestDependencies{
		Guard: &wakeTestGuard{active: true}, Capture: &wakeTestCapture{},
		Credentials: validWakeTestCredentials,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber { return &wakeTestTranscriber{} },
		WakeWord:    func(context.Context) (string, error) { return "AI 助手", nil },
	})
	if _, err := service.Start(context.Background()); err == nil {
		t.Fatal("活动会议期间不应启动唤醒测试")
	}
}

func TestWakeWordTestStopReleasesResources(t *testing.T) {
	stream := &wakeTestStream{}
	session := newWakeTestSession()
	service := agentservice.NewWakeWordTestService(agentservice.WakeWordTestDependencies{
		Guard: &wakeTestGuard{}, Capture: &wakeTestCapture{stream: stream},
		Credentials: validWakeTestCredentials,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber {
			return &wakeTestTranscriber{session: session}
		},
		WakeWord: func(context.Context) (string, error) { return "AI 助手", nil },
		Timeout:  time.Second,
	})
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("停止失败: %v", err)
	}
	state := waitWakeTestState(t, service, agentservice.WakeWordTestStopped)
	if stream.stopCount() != 1 || session.stopCount() != 1 || state.Matched != 0 {
		t.Fatalf("停止后的资源或状态不正确: state=%+v capture=%d asr=%d", state, stream.stopCount(), session.stopCount())
	}
}

func waitWakeTestState(t *testing.T, service *agentservice.WakeWordTestService, expected agentservice.WakeWordTestStatus) agentservice.WakeWordTestState {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state := service.State()
		if state.State == expected {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待状态 %s 超时，当前 %+v", expected, service.State())
	return agentservice.WakeWordTestState{}
}

type wakeTestGuard struct{ active bool }

func (guard *wakeTestGuard) HasActiveMeeting(context.Context) (bool, error) { return guard.active, nil }

type wakeTestCapture struct{ stream *wakeTestStream }

func (capture *wakeTestCapture) ListInputDevices(context.Context) ([]port.InputDevice, error) {
	return []port.InputDevice{{ID: "default", IsDefault: true}}, nil
}
func (capture *wakeTestCapture) TestInputDevice(context.Context, string) error { return nil }
func (capture *wakeTestCapture) Start(context.Context, string, port.AudioFormat) (port.AudioStream, error) {
	if capture.stream == nil {
		return nil, errors.New("不应启动采集")
	}
	return capture.stream, nil
}

type wakeTestStream struct {
	mu      sync.Mutex
	frames  []port.AudioFrame
	stopped int
}

func (stream *wakeTestStream) ReadFrames(ctx context.Context) (port.AudioFrame, error) {
	stream.mu.Lock()
	if len(stream.frames) > 0 {
		frame := stream.frames[0]
		stream.frames = stream.frames[1:]
		stream.mu.Unlock()
		return frame, nil
	}
	stream.mu.Unlock()
	<-ctx.Done()
	return port.AudioFrame{}, io.EOF
}
func (stream *wakeTestStream) Stop(context.Context) error {
	stream.mu.Lock()
	stream.stopped++
	stream.mu.Unlock()
	return nil
}
func (stream *wakeTestStream) stopCount() int {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.stopped
}

type wakeTestTranscriber struct{ session *wakeTestSession }

func (transcriber *wakeTestTranscriber) Start(context.Context, port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	return transcriber.session, nil
}

type wakeTestSession struct {
	mu      sync.Mutex
	events  chan port.TranscriptionEvent
	stopped int
}

func newWakeTestSession() *wakeTestSession {
	return &wakeTestSession{events: make(chan port.TranscriptionEvent, 16)}
}
func (session *wakeTestSession) WriteFrame(context.Context, port.AudioFrame) error { return nil }
func (session *wakeTestSession) LastSentSample() int64                             { return 0 }
func (session *wakeTestSession) Events() <-chan port.TranscriptionEvent            { return session.events }
func (session *wakeTestSession) Stop(context.Context) error {
	session.mu.Lock()
	session.stopped++
	session.mu.Unlock()
	return nil
}
func (session *wakeTestSession) stopCount() int {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.stopped
}

func validWakeTestCredentials(context.Context) (transcriptdomain.Credentials, error) {
	return transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "test-key"}, nil
}
