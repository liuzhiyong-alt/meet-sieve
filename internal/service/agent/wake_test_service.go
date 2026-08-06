package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	domainagent "meet-sieve/internal/domain/agent"
	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
)

const wakeWordTestRequired = 3

// WakeWordTestStatus 是真实唤醒测试的稳定进程内状态。
type WakeWordTestStatus string

const (
	WakeWordTestIdle    WakeWordTestStatus = "idle"
	WakeWordTestRunning WakeWordTestStatus = "running"
	WakeWordTestPassed  WakeWordTestStatus = "passed"
	WakeWordTestStopped WakeWordTestStatus = "stopped"
	WakeWordTestFailed  WakeWordTestStatus = "failed"
	WakeWordTestTimeout WakeWordTestStatus = "timeout"
)

// WakeWordTestState 是设置页可展示的脱敏测试投影。
type WakeWordTestState struct {
	State     WakeWordTestStatus
	Matched   int
	Required  int
	ASRState  string
	ErrorCode string
}

// WakeWordTestGuard 阻止测试和活动会议争用唯一麦克风。
type WakeWordTestGuard interface {
	HasActiveMeeting(context.Context) (bool, error)
}

// WakeWordTestDependencies 描述临时内存音频链路的真实依赖。
type WakeWordTestDependencies struct {
	Guard       WakeWordTestGuard
	Capture     port.AudioCapture
	Credentials func(context.Context) (transcriptdomain.Credentials, error)
	Transcriber func(transcriptdomain.Credentials) port.RealtimeTranscriber
	WakeWord    func(context.Context) (string, error)
	DeviceID    func(context.Context) (string, error)
	Publish     func(WakeWordTestState)
	Timeout     time.Duration
}

// WakeWordTestService 运行不落盘、不创建会议事实的真实三次 ASR 测试。
type WakeWordTestService struct {
	dependencies WakeWordTestDependencies
	mu           sync.Mutex
	state        WakeWordTestState
	cancel       context.CancelFunc
	done         chan struct{}
	starting     bool
}

// NewWakeWordTestService 创建单实例唤醒测试服务。
func NewWakeWordTestService(dependencies WakeWordTestDependencies) *WakeWordTestService {
	if dependencies.Timeout <= 0 {
		dependencies.Timeout = 60 * time.Second
	}
	return &WakeWordTestService{dependencies: dependencies, state: WakeWordTestState{State: WakeWordTestIdle, Required: wakeWordTestRequired, ASRState: "idle"}}
}

// Start 同步建立麦克风与 ASR，随后异步统计互异且连续匹配的 final。
func (service *WakeWordTestService) Start(ctx context.Context) (WakeWordTestState, error) {
	if err := service.validate(); err != nil {
		return WakeWordTestState{}, err
	}
	service.mu.Lock()
	if service.cancel != nil || service.starting {
		service.mu.Unlock()
		return WakeWordTestState{}, apperr.Biz(apperr.CodeAgentBusy, apperr.WithOp("agent.wake_test.start"))
	}
	service.starting = true
	service.mu.Unlock()
	started := false
	defer func() {
		if !started {
			service.mu.Lock()
			service.starting = false
			service.mu.Unlock()
		}
	}()
	active, err := service.dependencies.Guard.HasActiveMeeting(ctx)
	if err != nil {
		return WakeWordTestState{}, err
	}
	if active {
		return WakeWordTestState{}, apperr.Biz(apperr.CodeASRSettingsChangeBlocked, apperr.WithOp("agent.wake_test.active_meeting"))
	}
	wake, credentials, deviceID, err := service.loadInputs(ctx)
	if err != nil {
		return WakeWordTestState{}, err
	}
	format := port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}
	stream, err := service.dependencies.Capture.Start(ctx, deviceID, format)
	if err != nil {
		return WakeWordTestState{}, mapWakeTestAudioError(err)
	}
	session, err := service.dependencies.Transcriber(credentials).Start(ctx, port.RealtimeTranscriptionRequest{MeetingID: "wake-word-test", Format: format})
	if err != nil {
		_ = stream.Stop(context.Background())
		return WakeWordTestState{}, err
	}
	runContext, cancel := context.WithTimeout(context.Background(), service.dependencies.Timeout)
	done := make(chan struct{})
	service.mu.Lock()
	service.starting = false
	service.cancel, service.done = cancel, done
	service.state = WakeWordTestState{State: WakeWordTestRunning, Required: wakeWordTestRequired, ASRState: "connected"}
	state := service.state
	service.mu.Unlock()
	started = true
	service.publish(state)
	go service.run(runContext, stream, session, domainagent.NewWakeMatcher(wake), done)
	return state, nil
}

// Stop 取消当前测试并等待采集和 ASR 资源释放。
func (service *WakeWordTestService) Stop(ctx context.Context) error {
	service.mu.Lock()
	cancel, done := service.cancel, service.done
	service.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// State 返回可由页面重建的当前进程内状态。
func (service *WakeWordTestService) State() WakeWordTestState {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.state
}

// run 用有界队列连接采集与远端写入，并复用会中 6/3/60 秒完整指令判定。
func (service *WakeWordTestService) run(ctx context.Context, stream port.AudioStream, session port.RealtimeTranscriptionSession, matcher *domainagent.WakeMatcher, done chan struct{}) {
	frames := make(chan port.AudioFrame, 64)
	observedFrames := make(chan port.AudioFrame, 64)
	errors := make(chan error, 2)
	go readWakeTestFrames(ctx, stream, frames, observedFrames, errors)
	go writeWakeTestFrames(ctx, session, frames, errors)
	defer service.finishRun(stream, session, done)
	seen := make(map[string]struct{})
	collector := &wakeCommandCollector{}
	matched := 0
	for {
		select {
		case <-ctx.Done():
			status := WakeWordTestStopped
			if ctx.Err() == context.DeadlineExceeded {
				status = WakeWordTestTimeout
			}
			service.setTerminal(status, matched, "stopped", "")
			return
		case err := <-errors:
			if err != nil && err != context.Canceled {
				service.setTerminal(WakeWordTestFailed, matched, "failed", apperr.Normalize(err).ErrorCode)
				return
			}
		case frame := <-observedFrames:
			if command := collector.observeFrame(frame); command != nil {
				matched = service.completeWakeTestCommand(collector, matched)
				if matched >= wakeWordTestRequired {
					service.setTerminal(WakeWordTestPassed, matched, "connected", "")
					return
				}
			}
		case event, ok := <-session.Events():
			if !ok {
				service.setTerminal(WakeWordTestFailed, matched, "failed", apperr.CodeASRStreamInterrupted.ErrorCode)
				return
			}
			if event.Type == port.TranscriptionFailed {
				service.setTerminal(WakeWordTestFailed, matched, "failed", wakeTestFailureCode(event))
				return
			}
			if event.Type != port.TranscriptionFinal || alreadySeenWakeFinal(seen, event) {
				continue
			}
			_, command := collector.observeFinal(agentrepository.WakeFinal{
				UtteranceID: wakeTestResultID(event), MeetingID: "wake-word-test", Text: event.Text,
				StartSample: event.StartSample, EndSample: event.EndSample,
			}, matcher, "wake-test")
			if command != nil {
				matched = service.completeWakeTestCommand(collector, matched)
			}
			if matched >= wakeWordTestRequired {
				service.setTerminal(WakeWordTestPassed, matched, "connected", "")
				return
			}
		}
	}
}

// completeWakeTestCommand 只验证完整语音收集链路，不执行 Codex，也不写入会议事实。
func (service *WakeWordTestService) completeWakeTestCommand(collector *wakeCommandCollector, matched int) int {
	collector.complete()
	matched++
	service.setProgress(matched)
	return matched
}

// loadInputs 校验唤醒词和凭据，并解析当前或系统默认麦克风。
func (service *WakeWordTestService) loadInputs(ctx context.Context) (domainagent.WakeWord, transcriptdomain.Credentials, string, error) {
	wakeValue, err := service.dependencies.WakeWord(ctx)
	if err != nil {
		return domainagent.WakeWord{}, transcriptdomain.Credentials{}, "", err
	}
	wake, err := domainagent.NormalizeWakeWord(wakeValue)
	if err != nil {
		return domainagent.WakeWord{}, transcriptdomain.Credentials{}, "", apperr.Biz(apperr.CodeAgentWakeWordInvalid, apperr.WithOp("agent.wake_test.wake_word"))
	}
	credentials, err := service.dependencies.Credentials(ctx)
	if err != nil {
		return domainagent.WakeWord{}, transcriptdomain.Credentials{}, "", err
	}
	deviceID, err := service.resolveDeviceID(ctx)
	return wake, credentials, deviceID, err
}

// resolveDeviceID 优先显式设置，否则使用系统默认设备或列表第一项。
func (service *WakeWordTestService) resolveDeviceID(ctx context.Context) (string, error) {
	if service.dependencies.DeviceID != nil {
		value, err := service.dependencies.DeviceID(ctx)
		if err != nil || value != "" {
			return value, err
		}
	}
	devices, err := service.dependencies.Capture.ListInputDevices(ctx)
	if err != nil {
		return "", err
	}
	for _, device := range devices {
		if device.IsDefault {
			return device.ID, nil
		}
	}
	if len(devices) == 0 {
		return "", apperr.Biz(apperr.CodeMeetingAudioDeviceUnavailable, apperr.WithOp("agent.wake_test.device"))
	}
	return devices[0].ID, nil
}

// finishRun 以短独立上下文释放资源，并允许下一次测试启动。
func (service *WakeWordTestService) finishRun(stream port.AudioStream, session port.RealtimeTranscriptionSession, done chan struct{}) {
	cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = stream.Stop(cleanup)
	_ = session.Stop(cleanup)
	service.mu.Lock()
	service.cancel, service.done = nil, nil
	service.mu.Unlock()
	close(done)
}

// setProgress 更新连续命中次数。
func (service *WakeWordTestService) setProgress(matched int) {
	service.mu.Lock()
	service.state.Matched = matched
	state := service.state
	service.mu.Unlock()
	service.publish(state)
}

// setTerminal 写入稳定终态并发布一次完整快照。
func (service *WakeWordTestService) setTerminal(status WakeWordTestStatus, matched int, asrState string, errorCode string) {
	service.mu.Lock()
	service.state = WakeWordTestState{State: status, Matched: matched, Required: wakeWordTestRequired, ASRState: asrState, ErrorCode: errorCode}
	state := service.state
	service.mu.Unlock()
	service.publish(state)
}

// publish 在未配置 UI 发布器时保持服务可独立测试。
func (service *WakeWordTestService) publish(state WakeWordTestState) {
	if service.dependencies.Publish != nil {
		service.dependencies.Publish(state)
	}
}

// validate 检查所有真实链路依赖。
func (service *WakeWordTestService) validate() error {
	if service == nil || service.dependencies.Guard == nil || service.dependencies.Capture == nil || service.dependencies.Credentials == nil || service.dependencies.Transcriber == nil || service.dependencies.WakeWord == nil {
		return fmt.Errorf("唤醒词测试服务未初始化")
	}
	return nil
}

// readWakeTestFrames 把麦克风 PCM 放入固定容量队列，满时立即失败。
func readWakeTestFrames(ctx context.Context, stream port.AudioStream, frames chan<- port.AudioFrame, observed chan<- port.AudioFrame, failures chan<- error) {
	for {
		frame, err := stream.ReadFrames(ctx)
		if err != nil {
			select {
			case failures <- err:
			default:
			}
			return
		}
		select {
		case observed <- frame:
		case <-ctx.Done():
			return
		default:
			select {
			case failures <- apperr.Biz(apperr.CodeASREventBackpressure, apperr.WithOp("agent.wake_test.observer")):
			default:
			}
			return
		}
		select {
		case frames <- frame:
		case <-ctx.Done():
			return
		default:
			select {
			case failures <- apperr.Biz(apperr.CodeASREventBackpressure, apperr.WithOp("agent.wake_test.frames")):
			default:
			}
			return
		}
	}
}

// wakeTestResultID 返回测试 final 的稳定幂等身份。
func wakeTestResultID(event port.TranscriptionEvent) string {
	if event.ProviderResultID != "" {
		return event.ProviderResultID
	}
	return event.ResultID
}

// mapWakeTestAudioError 把底层麦克风错误转换为 Wails 可展示的稳定业务错误。
func mapWakeTestAudioError(cause error) error {
	if errors.Is(cause, port.ErrAudioPermissionDenied) {
		return apperr.Dependency(apperr.CodeMeetingAudioPermissionDenied, cause, apperr.WithOp("agent.wake_test.capture"))
	}
	return apperr.Dependency(apperr.CodeMeetingAudioDeviceUnavailable, cause, apperr.WithOp("agent.wake_test.capture"))
}

// writeWakeTestFrames 只把内存帧发送给临时 ASR session。
func writeWakeTestFrames(ctx context.Context, session port.RealtimeTranscriptionSession, frames <-chan port.AudioFrame, failures chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-frames:
			if err := session.WriteFrame(ctx, frame); err != nil {
				select {
				case failures <- err:
				default:
				}
				return
			}
		}
	}
}

// alreadySeenWakeFinal 按 provider result ID 对 final 幂等。
func alreadySeenWakeFinal(seen map[string]struct{}, event port.TranscriptionEvent) bool {
	id := event.ProviderResultID
	if id == "" {
		id = event.ResultID
	}
	if id == "" {
		return true
	}
	if _, exists := seen[id]; exists {
		return true
	}
	seen[id] = struct{}{}
	return false
}

// wakeTestFailureCode 提取 adapter 已脱敏的稳定失败码。
func wakeTestFailureCode(event port.TranscriptionEvent) string {
	if event.Failure != nil && event.Failure.Code != "" {
		return event.Failure.Code
	}
	return apperr.CodeASRStreamInterrupted.ErrorCode
}
