package agent

import (
	"strings"
	"sync"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
)

const (
	wakeSampleRate         int64 = 16_000
	wakeCommandWaitSamples       = 6 * wakeSampleRate
	// 唤醒词后的跨句收集只用短 VAD 防抖，避免 final 已到达后继续固定等待数秒。
	wakeEndSilenceSamples = 800 * wakeSampleRate / 1000
	wakeCommandMaxSamples = 60 * wakeSampleRate
)

// WakeCommandStatus 是会中语音指令收集器的稳定内存状态。
type WakeCommandStatus string

const (
	WakeCommandIdle       WakeCommandStatus = "idle"
	WakeCommandWaiting    WakeCommandStatus = "waiting_command"
	WakeCommandCollecting WakeCommandStatus = "collecting"
	WakeCommandBusy       WakeCommandStatus = "codex_busy"
	WakeCommandFailed     WakeCommandStatus = "failed"
)

// WakeCommand 是达到提交条件后的完整指令，不包含原始音频。
type WakeCommand struct {
	MeetingID    string
	CommandID    string
	Question     string
	UtteranceIDs []string
	WakeHash     string
}

// WakeCommandState 是 Wails 可展示的轻量状态，不包含指令文本或原始音频。
type WakeCommandState struct {
	MeetingID string
	State     WakeCommandStatus
	ErrorCode string
	Revision  uint64
}

// wakeCommandCollector 把跨 final 文本和本地 PCM 端点检测合并为单条 Codex 指令。
type wakeCommandCollector struct {
	mu            sync.Mutex
	status        WakeCommandStatus
	meetingID     string
	commandID     string
	wakeHash      string
	utteranceIDs  []string
	wakeEndSample int64
	commandStart  int64
	lastVoiceEnd  int64
	latestSample  int64
	questions     []string
	detector      adaptiveVoiceDetector
	prepared      *preparedWakeFinal
}

// preparedWakeFinal 保存 final 事务期间被锁定的收集器快照。
type preparedWakeFinal struct {
	token          string
	previous       wakeCollectorSnapshot
	previousStatus WakeCommandStatus
	accepted       bool
	command        *WakeCommand
}

// wakeCollectorSnapshot 是 final 事务失败时用于精确恢复的内存快照。
type wakeCollectorSnapshot struct {
	status        WakeCommandStatus
	meetingID     string
	commandID     string
	wakeHash      string
	utteranceIDs  []string
	wakeEndSample int64
	commandStart  int64
	lastVoiceEnd  int64
	latestSample  int64
	questions     []string
	detector      adaptiveVoiceDetector
}

// wakeFrameOutcome 汇总 PCM 推进后需要执行的提交或候选释放动作。
type wakeFrameOutcome struct {
	command          *WakeCommand
	releaseCommandID string
}

// observeFinal 接收已持久化 final；idle 时 matcher 必须非空，活动收集期间则直接追加文本。
func (collector *wakeCommandCollector) observeFinal(final agentrepository.WakeFinal, matcher *domainagent.WakeMatcher, wakeHash string) (bool, *WakeCommand) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	return collector.observeFinalLocked(final, matcher, wakeHash)
}

// prepareFinal 锁定收集器直到 final 事务提交或回滚，避免 PCM 与下一个 final 穿透事务边界。
func (collector *wakeCommandCollector) prepareFinal(final agentrepository.WakeFinal, matcher *domainagent.WakeMatcher, wakeHash string) (bool, string, int) {
	collector.mu.Lock()
	previous := collector.captureLocked()
	previousStatus := normalizedWakeStatus(collector.status)
	accepted, command := collector.observeFinalLocked(final, matcher, wakeHash)
	collector.prepared = &preparedWakeFinal{
		token: final.UtteranceID, previous: previous, previousStatus: previousStatus,
		accepted: accepted, command: command,
	}
	if !accepted {
		return false, "", 0
	}
	return true, collector.commandID, len(collector.utteranceIDs) - 1
}

// commitPrepared 提交 final 内存状态并返回事务后才能执行的异步动作。
func (collector *wakeCommandCollector) commitPrepared(token string) (string, WakeCommandStatus, *WakeCommand, bool) {
	prepared := collector.prepared
	if prepared == nil {
		return "", WakeCommandIdle, nil, false
	}
	if prepared.token != token {
		collector.restoreLocked(prepared.previous)
		collector.prepared = nil
		collector.mu.Unlock()
		return "", WakeCommandIdle, nil, false
	}
	meetingID := collector.meetingID
	previous := prepared.previousStatus
	command := prepared.command
	accepted := prepared.accepted
	collector.prepared = nil
	collector.mu.Unlock()
	return meetingID, previous, command, accepted
}

// rollbackPrepared 恢复 final 事务前快照并解除收集器锁。
func (collector *wakeCommandCollector) rollbackPrepared(_ string) {
	prepared := collector.prepared
	if prepared == nil {
		return
	}
	// token 不匹配表示调用链已失序；仍恢复快照并解锁，避免永久阻塞 PCM 与后续 final。
	collector.restoreLocked(prepared.previous)
	collector.prepared = nil
	collector.mu.Unlock()
}

// observeFinalLocked 接收一条 final；调用方必须持有 collector.mu。
func (collector *wakeCommandCollector) observeFinalLocked(final agentrepository.WakeFinal, matcher *domainagent.WakeMatcher, wakeHash string) (bool, *WakeCommand) {
	if collector.status == WakeCommandBusy || final.MeetingID == "" || final.EndSample <= final.StartSample {
		return false, nil
	}
	if collector.status == "" || collector.status == WakeCommandIdle {
		if matcher == nil {
			return false, nil
		}
		matched, question := matcher.MatchPrefix(final.Text)
		if !matched {
			return false, nil
		}
		collector.arm(final, wakeHash, question)
		return true, collector.maybeSubmitLocked()
	}
	if final.MeetingID != collector.meetingID {
		return false, nil
	}
	question := strings.TrimSpace(final.Text)
	if question == "" {
		return false, nil
	}
	collector.questions = append(collector.questions, question)
	collector.utteranceIDs = append(collector.utteranceIDs, final.UtteranceID)
	collector.status = WakeCommandCollecting
	if collector.commandStart == 0 {
		collector.commandStart = final.StartSample
	}
	collector.lastVoiceEnd = maxInt64(collector.lastVoiceEnd, final.EndSample)
	return true, collector.maybeSubmitLocked()
}

// observeFrame 使用已成功写入本地录音的 PCM 更新 6/3/60 秒样本计时。
func (collector *wakeCommandCollector) observeFrame(frame port.AudioFrame) *WakeCommand {
	return collector.observeFrameOutcome(frame).command
}

// observeFrameOutcome 使用已成功写入本地录音的 PCM 更新计时并返回后续动作。
func (collector *wakeCommandCollector) observeFrameOutcome(frame port.AudioFrame) wakeFrameOutcome {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	frameEnd := frame.StartSample + int64(len(frame.PCM)/2)
	collector.latestSample = maxInt64(collector.latestSample, frameEnd)
	if collector.status == "" || collector.status == WakeCommandIdle || collector.status == WakeCommandBusy {
		collector.detector.observe(frame)
		return wakeFrameOutcome{}
	}
	if collector.detector.observe(frame) {
		collector.lastVoiceEnd = maxInt64(collector.lastVoiceEnd, frameEnd)
		if collector.status == WakeCommandWaiting && frame.StartSample >= collector.wakeEndSample {
			collector.status = WakeCommandCollecting
			collector.commandStart = frame.StartSample
		}
	}
	if collector.status == WakeCommandWaiting && frameEnd-collector.wakeEndSample >= wakeCommandWaitSamples {
		commandID := collector.commandID
		collector.resetLocked()
		return wakeFrameOutcome{releaseCommandID: commandID}
	}
	if collector.status == WakeCommandCollecting && collector.commandStart > 0 && frameEnd-collector.commandStart >= wakeCommandMaxSamples {
		commandID := collector.commandID
		collector.resetLocked()
		return wakeFrameOutcome{releaseCommandID: commandID}
	}
	return wakeFrameOutcome{command: collector.maybeSubmitLocked()}
}

// complete 在 Codex 执行和媒体恢复结束后重新开放下一次唤醒检测。
func (collector *wakeCommandCollector) complete() {
	collector.mu.Lock()
	collector.resetLocked()
	collector.mu.Unlock()
}

// statusValue 返回并发安全的当前收集状态。
func (collector *wakeCommandCollector) statusValue() WakeCommandStatus {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if collector.status == "" {
		return WakeCommandIdle
	}
	return collector.status
}

// snapshot 返回当前会议和状态；只用于比较是否需要发布 UI 事件。
func (collector *wakeCommandCollector) snapshot() (string, WakeCommandStatus) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	status := collector.status
	if status == "" {
		status = WakeCommandIdle
	}
	return collector.meetingID, status
}

// arm 记录一次新唤醒；同一 final 已含指令时直接进入 collecting。
func (collector *wakeCommandCollector) arm(final agentrepository.WakeFinal, wakeHash string, question string) {
	collector.meetingID = final.MeetingID
	collector.commandID = final.UtteranceID
	collector.wakeHash = wakeHash
	collector.utteranceIDs = []string{final.UtteranceID}
	collector.wakeEndSample = final.EndSample
	collector.lastVoiceEnd = final.EndSample
	collector.status = WakeCommandWaiting
	if strings.TrimSpace(question) != "" {
		collector.questions = []string{strings.TrimSpace(question)}
		collector.commandStart = final.StartSample
		collector.status = WakeCommandCollecting
	}
}

// maybeSubmitLocked 在已有 final 文本且最后语音后累计静音达到 3 秒时生成一次提交。
func (collector *wakeCommandCollector) maybeSubmitLocked() *WakeCommand {
	if collector.status != WakeCommandCollecting || len(collector.questions) == 0 || collector.latestSample-collector.lastVoiceEnd < wakeEndSilenceSamples {
		return nil
	}
	command := &WakeCommand{
		MeetingID: collector.meetingID, CommandID: collector.commandID,
		Question:     strings.Join(collector.questions, "，"),
		UtteranceIDs: append([]string(nil), collector.utteranceIDs...), WakeHash: collector.wakeHash,
	}
	collector.status = WakeCommandBusy
	return command
}

// resetLocked 清理上一条指令的全部短期文本和计时状态。
func (collector *wakeCommandCollector) resetLocked() {
	collector.status = WakeCommandIdle
	collector.meetingID, collector.commandID, collector.wakeHash = "", "", ""
	collector.wakeEndSample, collector.commandStart, collector.lastVoiceEnd = 0, 0, 0
	collector.questions, collector.utteranceIDs = nil, nil
}

// captureLocked 复制当前状态；调用方必须持有 collector.mu。
func (collector *wakeCommandCollector) captureLocked() wakeCollectorSnapshot {
	return wakeCollectorSnapshot{
		status: collector.status, meetingID: collector.meetingID, commandID: collector.commandID,
		wakeHash: collector.wakeHash, utteranceIDs: append([]string(nil), collector.utteranceIDs...),
		wakeEndSample: collector.wakeEndSample, commandStart: collector.commandStart,
		lastVoiceEnd: collector.lastVoiceEnd, latestSample: collector.latestSample,
		questions: append([]string(nil), collector.questions...), detector: collector.detector,
	}
}

// restoreLocked 恢复事务前状态；调用方必须持有 collector.mu。
func (collector *wakeCommandCollector) restoreLocked(snapshot wakeCollectorSnapshot) {
	collector.status, collector.meetingID = snapshot.status, snapshot.meetingID
	collector.commandID, collector.wakeHash = snapshot.commandID, snapshot.wakeHash
	collector.utteranceIDs = append([]string(nil), snapshot.utteranceIDs...)
	collector.wakeEndSample, collector.commandStart = snapshot.wakeEndSample, snapshot.commandStart
	collector.lastVoiceEnd, collector.latestSample = snapshot.lastVoiceEnd, snapshot.latestSample
	collector.questions = append([]string(nil), snapshot.questions...)
	collector.detector = snapshot.detector
}

// normalizedWakeStatus 把零值状态映射为稳定 idle。
func normalizedWakeStatus(status WakeCommandStatus) WakeCommandStatus {
	if status == "" {
		return WakeCommandIdle
	}
	return status
}

// maxInt64 返回两个样本位置中的较大值。
func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
