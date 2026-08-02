package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

const (
	recordingSampleRate = 16000
	recordingMaxSamples = recordingSampleRate * 60
)

// RecordingResult 是停止声纹录音后交给统一规范化流水线的 PCM。
type RecordingResult struct {
	PCM         []byte
	SampleRate  int
	DurationMS  int64
	AutoStopped bool
}

// RecordingSnapshot 是录音界面可轮询的真实采集进度与当前 PCM 峰值。
type RecordingSnapshot struct {
	Level      float64
	DurationMS int64
}

// Recorder 管理单个会前短录音会话；不负责 WAV、质量或 embedding。
type Recorder struct {
	capture  port.AudioCapture
	mu       sync.Mutex
	starting bool
	session  *recordingSession
}

// recordingSession 保存一次后台读取的结果与完成信号。
type recordingSession struct {
	stream     port.AudioStream
	cancel     context.CancelFunc
	done       chan struct{}
	result     RecordingResult
	err        error
	levelBits  atomic.Uint64
	durationMS atomic.Int64
}

// NewRecorder 创建只允许一个活动会话的声纹录音服务。
func NewRecorder(capture port.AudioCapture) *Recorder {
	return &Recorder{capture: capture}
}

// Start 打开 16 kHz、16-bit、单声道设备流并开始后台收集。
func (recorder *Recorder) Start(ctx context.Context, deviceID string) error {
	if recorder == nil || recorder.capture == nil {
		return fmt.Errorf("声纹录音服务依赖未初始化")
	}
	if !recorder.reserveStart() {
		return apperr.Biz(apperr.CodeVoiceRecordingBusy, apperr.WithOp("voice.recording.start"))
	}
	stream, err := recorder.capture.Start(ctx, deviceID, port.AudioFormat{
		SampleRate: recordingSampleRate, BitsPerSample: 16, Channels: 1,
	})
	if err != nil {
		recorder.finishStart(nil)
		return apperr.Dependency(apperr.CodeVoiceDeviceUnavailable, err, apperr.WithOp("voice.recording.device_start"))
	}
	readContext, cancel := context.WithCancel(ctx)
	session := &recordingSession{stream: stream, cancel: cancel, done: make(chan struct{})}
	recorder.finishStart(session)
	go collectRecording(readContext, session)
	return nil
}

// Stop 停止当前录音并返回已收集 PCM；重复设备 Stop 由 Port 保证幂等。
func (recorder *Recorder) Stop(ctx context.Context) (RecordingResult, error) {
	recorder.mu.Lock()
	session := recorder.session
	recorder.mu.Unlock()
	if session == nil {
		return RecordingResult{}, apperr.Biz(apperr.CodeVoiceRecordingBusy, apperr.WithOp("voice.recording.stop_missing"))
	}
	session.cancel()
	stopErr := session.stream.Stop(ctx)
	select {
	case <-ctx.Done():
		return RecordingResult{}, ctx.Err()
	case <-session.done:
	}
	recorder.clearSession(session)
	if session.err != nil {
		return RecordingResult{}, session.err
	}
	if stopErr != nil {
		return RecordingResult{}, apperr.Dependency(apperr.CodeVoiceDeviceUnavailable, stopErr, apperr.WithOp("voice.recording.stop"))
	}
	session.result.PCM = append([]byte(nil), session.result.PCM...)
	return session.result, nil
}

// Cancel 停止当前录音并丢弃已收集 PCM；没有活动会话时保持幂等成功。
func (recorder *Recorder) Cancel(ctx context.Context) error {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	session := recorder.session
	recorder.mu.Unlock()
	if session == nil {
		return nil
	}
	session.cancel()
	stopErr := session.stream.Stop(ctx)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-session.done:
	}
	recorder.clearSession(session)
	if stopErr != nil {
		return apperr.Dependency(apperr.CodeVoiceDeviceUnavailable, stopErr, apperr.WithOp("voice.recording.cancel"))
	}
	return nil
}

// Snapshot 返回活动录音最近一帧的归一化峰值和已采集时长；没有会话时返回零值。
func (recorder *Recorder) Snapshot() RecordingSnapshot {
	if recorder == nil {
		return RecordingSnapshot{}
	}
	recorder.mu.Lock()
	session := recorder.session
	recorder.mu.Unlock()
	if session == nil {
		return RecordingSnapshot{}
	}
	return RecordingSnapshot{
		Level:      math.Float64frombits(session.levelBits.Load()),
		DurationMS: session.durationMS.Load(),
	}
}

// reserveStart 在调用设备前占用启动状态，使并发 Start 能立即失败。
func (recorder *Recorder) reserveStart() bool {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.starting || recorder.session != nil {
		return false
	}
	recorder.starting = true
	return true
}

// finishStart 结束设备启动阶段，并登记成功会话。
func (recorder *Recorder) finishStart(session *recordingSession) {
	recorder.mu.Lock()
	recorder.starting = false
	recorder.session = session
	recorder.mu.Unlock()
}

// clearSession 仅清除调用方等待完成的同一个会话。
func (recorder *Recorder) clearSession(session *recordingSession) {
	recorder.mu.Lock()
	if recorder.session == session {
		recorder.session = nil
	}
	recorder.mu.Unlock()
}

// collectRecording 持续读取连续 PCM，并在 60 秒处精确停止。
func collectRecording(ctx context.Context, session *recordingSession) {
	defer close(session.done)
	pcm := make([]byte, 0, recordingMaxSamples*2)
	for len(pcm) < recordingMaxSamples*2 {
		frame, err := session.stream.ReadFrames(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			session.err = apperr.Dependency(apperr.CodeVoiceDeviceUnavailable, err, apperr.WithOp("voice.recording.read"))
			return
		}
		if len(frame.PCM)%2 != 0 {
			session.err = fmt.Errorf("录音 PCM 帧边界不完整")
			return
		}
		remaining := recordingMaxSamples*2 - len(pcm)
		if len(frame.PCM) >= remaining {
			accepted := frame.PCM[:remaining]
			pcm = append(pcm, accepted...)
			updateRecordingSnapshot(session, accepted, len(pcm))
			session.result.AutoStopped = true
			_ = session.stream.Stop(context.Background())
			break
		}
		pcm = append(pcm, frame.PCM...)
		updateRecordingSnapshot(session, frame.PCM, len(pcm))
	}
	session.result = RecordingResult{
		PCM: pcm, SampleRate: recordingSampleRate,
		DurationMS:  int64(len(pcm)/2) * 1000 / recordingSampleRate,
		AutoStopped: session.result.AutoStopped,
	}
}

// updateRecordingSnapshot 从真实 int16 PCM 计算最近帧峰值，并发布累计录音时长。
func updateRecordingSnapshot(session *recordingSession, frame []byte, totalBytes int) {
	var peak int32
	for offset := 0; offset+1 < len(frame); offset += 2 {
		sample := int32(int16(binary.LittleEndian.Uint16(frame[offset:])))
		if sample < 0 {
			sample = -sample
		}
		if sample > peak {
			peak = sample
		}
	}
	session.levelBits.Store(math.Float64bits(float64(peak) / 32768))
	session.durationMS.Store(int64(totalBytes/2) * 1000 / recordingSampleRate)
}
