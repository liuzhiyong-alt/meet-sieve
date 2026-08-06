package agent

import (
	"context"
	"fmt"
	"sync/atomic"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
)

// WakeAskService 是 WakeObserver 唯一允许触发的主持人问答入口。
type WakeAskService interface {
	Ask(ctx context.Context, input AskInput) (AskResult, error)
}

// WakeObserver 只观察已提交 ASR final，并把匹配结果交给 TurnService。
type WakeObserver struct {
	repository *agentrepository.Repository
	turns      WakeAskService
	collector  wakeCommandCollector
	publish    func(WakeCommandState)
	release    func(string)
	revision   atomic.Uint64
}

// SetReleasePublisher 设置候选指令恢复为普通发言后的投影失效通知。
func (observer *WakeObserver) SetReleasePublisher(publisher func(meetingID string)) {
	if observer != nil {
		observer.release = publisher
	}
}

// NewWakeObserver 创建 final 唤醒观察器。
func NewWakeObserver(repository *agentrepository.Repository, turns WakeAskService) *WakeObserver {
	return &WakeObserver{repository: repository, turns: turns}
}

// SetPublisher 设置会中唤醒轻量状态出口；必须在开始观察音频前调用。
func (observer *WakeObserver) SetPublisher(publisher func(WakeCommandState)) {
	if observer != nil {
		observer.publish = publisher
	}
}

// Observe 读取持久 final，允许唤醒词与指令跨句，并在满足端点条件后异步提交。
func (observer *WakeObserver) Observe(ctx context.Context, utteranceID string) (bool, error) {
	if observer == nil || observer.repository == nil || observer.turns == nil || utteranceID == "" {
		return false, fmt.Errorf("唤醒观察器未初始化")
	}
	final, err := observer.repository.GetWakeFinal(ctx, utteranceID)
	if err != nil {
		return false, err
	}
	var matcher *domainagent.WakeMatcher
	var wakeHash string
	if observer.collector.statusValue() == WakeCommandIdle {
		settings, settingsErr := observer.repository.GetSettings(ctx)
		if settingsErr != nil {
			return false, settingsErr
		}
		wake, wakeErr := domainagent.NormalizeWakeWord(settings.WakeWord)
		if wakeErr != nil {
			return false, wakeErr
		}
		matcher, wakeHash = domainagent.NewWakeMatcher(wake), wake.Hash
	}
	_, previous := observer.collector.snapshot()
	accepted, command := observer.collector.observeFinal(final, matcher, wakeHash)
	observer.publishIfChanged(final.MeetingID, previous)
	observer.dispatch(command)
	return accepted, nil
}

// PrepareFinal 在 final 事务前锁定收集器并返回需要原子写入的用途关系。
func (observer *WakeObserver) PrepareFinal(ctx context.Context, candidate port.TranscriptFinalCandidate) port.TranscriptFinalClassification {
	if observer == nil || observer.repository == nil || observer.turns == nil || candidate.UtteranceID == "" {
		return port.TranscriptFinalClassification{}
	}
	matcher, wakeHash := observer.loadMatcher(ctx)
	accepted, commandID, position := observer.collector.prepareFinal(agentrepository.WakeFinal{
		UtteranceID: candidate.UtteranceID, MeetingID: candidate.MeetingID, Text: candidate.Text,
		StartSample: candidate.StartSample, EndSample: candidate.EndSample,
	}, matcher, wakeHash)
	return port.TranscriptFinalClassification{
		Token: candidate.UtteranceID, CommandID: commandID, Position: position, Candidate: accepted,
	}
}

// CommitFinal 在 final 和候选关系提交后发布状态并允许触发 Codex。
func (observer *WakeObserver) CommitFinal(token string) {
	if observer == nil || token == "" {
		return
	}
	meetingID, previous, command, _ := observer.collector.commitPrepared(token)
	observer.publishIfChanged(meetingID, previous)
	observer.dispatch(command)
}

// RollbackFinal 撤销未提交 final 对内存收集器的影响。
func (observer *WakeObserver) RollbackFinal(token string) {
	if observer != nil && token != "" {
		observer.collector.rollbackPrepared(token)
	}
}

// loadMatcher 仅在 idle 状态加载当前冻结唤醒词；读取失败时把 final 当普通会议内容。
func (observer *WakeObserver) loadMatcher(ctx context.Context) (*domainagent.WakeMatcher, string) {
	if observer.collector.statusValue() != WakeCommandIdle {
		return nil, ""
	}
	settings, err := observer.repository.GetSettings(ctx)
	if err != nil {
		return nil, ""
	}
	wake, err := domainagent.NormalizeWakeWord(settings.WakeWord)
	if err != nil {
		return nil, ""
	}
	return domainagent.NewWakeMatcher(wake), wake.Hash
}

// ObserveFrame 观察已落盘 PCM；达到 3 秒静音时提交已收集的 final 文本。
func (observer *WakeObserver) ObserveFrame(frame port.AudioFrame) {
	if observer == nil || observer.turns == nil {
		return
	}
	meetingID, previous := observer.collector.snapshot()
	outcome := observer.collector.observeFrameOutcome(frame)
	if outcome.command != nil {
		meetingID = outcome.command.MeetingID
	}
	observer.publishIfChanged(meetingID, previous)
	if outcome.releaseCommandID != "" {
		observer.releaseCandidates(meetingID, outcome.releaseCommandID)
	}
	observer.dispatch(outcome.command)
}

// Status 返回会中唤醒收集状态，供后续 Wails 状态投影和测试使用。
func (observer *WakeObserver) Status() WakeCommandStatus {
	if observer == nil {
		return WakeCommandIdle
	}
	return observer.collector.statusValue()
}

// dispatch 在独立 goroutine 执行 Codex；媒体暂停由 TurnService 的语音生命周期负责。
func (observer *WakeObserver) dispatch(command *WakeCommand) {
	if command == nil {
		return
	}
	go func() {
		triggerID := command.UtteranceIDs[0]
		_, err := observer.turns.Ask(context.Background(), AskInput{
			MeetingID: command.MeetingID, Question: command.Question, Trigger: "wake_word",
			TriggerUtteranceID: &triggerID, TriggerUtteranceIDs: command.UtteranceIDs,
			VoiceCommandID: command.CommandID,
			IdempotencyKey: "wake:" + command.CommandID + ":" + command.WakeHash,
		})
		observer.collector.complete()
		if err != nil {
			observer.releaseCandidates(command.MeetingID, command.CommandID)
			observer.publishState(command.MeetingID, WakeCommandFailed, apperr.Normalize(err).ErrorCode)
			return
		}
		observer.publishState(command.MeetingID, WakeCommandIdle, "")
	}()
}

// releaseCandidates 释放尚未消费的关系，并在事务成功后让会议投影重新拉取。
func (observer *WakeObserver) releaseCandidates(meetingID string, commandID string) {
	if observer == nil || meetingID == "" || commandID == "" {
		return
	}
	if err := observer.repository.ReleaseVoiceCommandCandidates(context.Background(), commandID); err == nil && observer.release != nil {
		observer.release(meetingID)
	}
}

// publishIfChanged 只在状态迁移时发布，避免每个 PCM 帧产生 Wails 事件。
func (observer *WakeObserver) publishIfChanged(meetingID string, previous WakeCommandStatus) {
	_, current := observer.collector.snapshot()
	if current != previous {
		observer.publishState(meetingID, current, "")
	}
}

// publishState 发布不含指令文本的递增快照。
func (observer *WakeObserver) publishState(meetingID string, state WakeCommandStatus, errorCode string) {
	if observer == nil || observer.publish == nil || meetingID == "" {
		return
	}
	observer.publish(WakeCommandState{MeetingID: meetingID, State: state, ErrorCode: errorCode, Revision: observer.revision.Add(1)})
}
