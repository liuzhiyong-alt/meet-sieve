package transcript

import (
	"context"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"

	"gorm.io/gorm"
)

// sentCheckpointSamples 保持每两秒持久化一次实际发送边界，独立于启动缓冲容量。
const sentCheckpointSamples int64 = 2 * transcriptdomain.SampleRate

func (coordinator *RealtimeCoordinator) persistFinal(ctx context.Context, event port.TranscriptionEvent) error {
	rangeValue, err := transcriptdomain.NewSampleRange(event.StartSample, event.EndSample)
	if err != nil {
		return err
	}
	var speaker *string
	if event.SpeakerLabel != "" {
		speaker = &event.SpeakerLabel
	}
	_, err = coordinator.dependencies.Events.PersistFinal(ctx, FinalInput{MeetingID: coordinator.meetingID, ASRSessionID: event.SessionID, ProviderResultID: event.ProviderResultID, Text: event.Text, Range: rangeValue, SpeakerLabel: speaker, LastSentSample: event.LastSentSample})
	if err == nil {
		coordinator.mu.Lock()
		if event.EndSample > coordinator.lastFinal {
			coordinator.lastFinal = event.EndSample
		}
		coordinator.mu.Unlock()
	}
	return err
}

// finalPersistFailure 保留持久化错误的稳定码和可恢复性，避免把临时 SQLite 失败误判为永久不可用。
func finalPersistFailure(err error) realtimeFailure {
	appErr := apperr.Normalize(err)
	if appErr == nil {
		return realtimeFailure{code: apperr.CodeASREventPersistFailed.ErrorCode, retryable: true, reason: transcriptdomain.GapBackpressure}
	}
	return realtimeFailure{
		code: appErr.ErrorCode, retryable: appErr.Retryable,
		reason: transcriptdomain.GapBackpressure, cause: err,
	}
}

// handleFinalPersistFailure 隔离单条无效 final；其他持久化失败仍交给重连协调器。
func (coordinator *RealtimeCoordinator) handleFinalPersistFailure(event port.TranscriptionEvent, err error) {
	appErr := apperr.Normalize(err)
	if appErr == nil || appErr.ErrorCode != apperr.CodeASRFinalInvalid.ErrorCode {
		coordinator.reportFailure(finalPersistFailure(err))
		return
	}
	sampleRange, rangeErr := transcriptdomain.NewSampleRange(event.StartSample, event.EndSample)
	if rangeErr != nil {
		coordinator.reportFailure(finalPersistFailure(err))
		return
	}
	sessionID := event.SessionID
	_, persistErr := coordinator.dependencies.Events.PersistGap(context.Background(), GapInput{
		MeetingID: coordinator.meetingID, ASRSessionID: &sessionID,
		Range: sampleRange, Reason: transcriptdomain.GapInvalidFinal,
	})
	if persistErr != nil {
		coordinator.reportFailure(finalPersistFailure(persistErr))
	}
}

// advanceSent 更新内存精确边界，并每推进两秒 latest-wins 持久化检查点。
func (coordinator *RealtimeCoordinator) advanceSent(current *physicalSession, sample int64) {
	coordinator.mu.Lock()
	previous := coordinator.lastSent
	if sample > coordinator.lastSent {
		coordinator.lastSent = sample
	}
	coordinator.mu.Unlock()
	if sample-previous < sentCheckpointSamples && sample%sentCheckpointSamples != 0 {
		return
	}
	now := coordinator.dependencies.Clock.Now().UnixMilli()
	_ = coordinator.dependencies.Transactions.WithinTransaction(coordinator.ctx, func(tx *gorm.DB) error {
		return coordinator.dependencies.Repository.AdvanceSessionSentSample(coordinator.ctx, tx, coordinator.meetingID, current.id, sample, now)
	})
}

// checkpointSent 强制保存停止时的最新实际发送边界。
func (coordinator *RealtimeCoordinator) checkpointSent(current *physicalSession, sample int64) {
	if sample < current.inputStart {
		return
	}
	coordinator.mu.Lock()
	if sample > coordinator.lastSent {
		coordinator.lastSent = sample
	}
	coordinator.mu.Unlock()
	now := coordinator.dependencies.Clock.Now().UnixMilli()
	_ = coordinator.dependencies.Transactions.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return coordinator.dependencies.Repository.AdvanceSessionSentSample(context.Background(), tx, coordinator.meetingID, current.id, sample, now)
	})
}

// markStreaming 原子更新物理 session 与会议状态。
func (coordinator *RealtimeCoordinator) markStreaming(current *physicalSession, providerSessionID string) {
	coordinator.mu.Lock()
	isCurrent := coordinator.current == current && current != nil && current.generation == coordinator.generation && !coordinator.stopping && !coordinator.unavailable
	coordinator.mu.Unlock()
	if !isCurrent {
		return
	}
	now := coordinator.dependencies.Clock.Now().UnixMilli()
	_ = coordinator.dependencies.Transactions.WithinTransaction(coordinator.ctx, func(tx *gorm.DB) error {
		return coordinator.dependencies.Repository.MarkSessionStreaming(coordinator.ctx, tx, coordinator.meetingID, current.id, providerSessionID, now)
	})
}

// finishCurrent 终结当前物理 session；没有当前 session 时仅更新会议状态。
func (coordinator *RealtimeCoordinator) finishCurrent(sessionState string, meetingState string, errorCode *string) {
	coordinator.mu.Lock()
	current := coordinator.current
	coordinator.current = nil
	coordinator.mu.Unlock()
	if current != nil {
		coordinator.finishSession(current.id, sessionState, meetingState, errorCode)
		return
	}
	now := coordinator.dependencies.Clock.Now().UnixMilli()
	_ = coordinator.dependencies.Transactions.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return coordinator.dependencies.Repository.UpdateMeetingASRState(context.Background(), tx, coordinator.meetingID, meetingState, now)
	})
}

// finishSession 保存物理连接终态。
func (coordinator *RealtimeCoordinator) finishSession(sessionID string, sessionState string, meetingState string, errorCode *string) {
	now := coordinator.dependencies.Clock.Now().UnixMilli()
	_ = coordinator.dependencies.Transactions.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return coordinator.dependencies.Repository.FinishSession(context.Background(), tx, coordinator.meetingID, sessionID, sessionState, meetingState, errorCode, now)
	})
}

// openGap 仅记录尚未闭合的样本起点；空区间不会落库。
func (coordinator *RealtimeCoordinator) openGap(start int64, reason transcriptdomain.GapReason, sessionID *string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.gapStart == nil {
		coordinator.gapStart = &start
		coordinator.gapReason = reason
		coordinator.gapSessionID = sessionID
	}
}

// openTailGap 在停止异常时从最后 final 开启 tail timeout gap。
func (coordinator *RealtimeCoordinator) openTailGap(end int64) {
	coordinator.mu.Lock()
	start := coordinator.lastFinal
	coordinator.mu.Unlock()
	if start < end {
		coordinator.openGap(start, transcriptdomain.GapTailTimeout, nil)
	}
}

// persistOpenGap 用录音结束样本关闭当前缺口。
func (coordinator *RealtimeCoordinator) persistOpenGap(ctx context.Context, end int64) error {
	coordinator.mu.Lock()
	if coordinator.gapStart == nil || *coordinator.gapStart >= end {
		coordinator.gapStart = nil
		coordinator.mu.Unlock()
		return nil
	}
	start, reason, sessionID := *coordinator.gapStart, coordinator.gapReason, coordinator.gapSessionID
	coordinator.gapStart = nil
	coordinator.mu.Unlock()
	rangeValue, err := transcriptdomain.NewSampleRange(start, end)
	if err != nil {
		return err
	}
	_, err = coordinator.dependencies.Events.PersistGap(ctx, GapInput{MeetingID: coordinator.meetingID, ASRSessionID: sessionID, Range: rangeValue, Reason: reason})
	return err
}

// closeOpenGap 在新 session 接管首样本时关闭断线区间；空区间只清除内存状态。
func (coordinator *RealtimeCoordinator) closeOpenGap(ctx context.Context, end int64) error {
	coordinator.mu.Lock()
	if coordinator.gapStart == nil {
		coordinator.mu.Unlock()
		return nil
	}
	if *coordinator.gapStart >= end {
		coordinator.gapStart = nil
		coordinator.gapSessionID = nil
		coordinator.mu.Unlock()
		return nil
	}
	start, reason, sessionID := *coordinator.gapStart, coordinator.gapReason, coordinator.gapSessionID
	coordinator.gapStart = nil
	coordinator.gapSessionID = nil
	coordinator.mu.Unlock()
	rangeValue, err := transcriptdomain.NewSampleRange(start, end)
	if err != nil {
		return err
	}
	_, err = coordinator.dependencies.Events.PersistGap(ctx, GapInput{MeetingID: coordinator.meetingID, ASRSessionID: sessionID, Range: rangeValue, Reason: reason})
	return err
}

// markUnavailable 关闭实时旁路但不改变录音状态。
func (coordinator *RealtimeCoordinator) markUnavailable(errorCode string) {
	coordinator.mu.Lock()
	if coordinator.unavailable {
		coordinator.mu.Unlock()
		return
	}
	coordinator.unavailable = true
	coordinator.mu.Unlock()
	coordinator.publishState("unavailable", errorCode)
	now := coordinator.dependencies.Clock.Now().UnixMilli()
	_ = coordinator.dependencies.Transactions.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return coordinator.dependencies.Repository.UpdateMeetingASRState(context.Background(), tx, coordinator.meetingID, "unavailable", now)
	})
}

// publishState 发布安全业务状态。
func (coordinator *RealtimeCoordinator) publishState(state string, errorCode string) {
	if coordinator.dependencies.PublishState != nil {
		coordinator.dependencies.PublishState(coordinator.meetingID, state, errorCode)
	}
}

// waitReconnect 执行可取消的真实退避，不使用阻塞 sleep。
func waitReconnect(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// errorCodeOf 只提取稳定错误码，不传播 cause 正文。
func errorCodeOf(err error) string {
	appErr := apperr.Normalize(err)
	if appErr == nil || appErr.ErrorCode == "" {
		return apperr.CodeASRStreamInterrupted.ErrorCode
	}
	return appErr.ErrorCode
}

// pointerString 创建稳定错误码指针。
func pointerString(value string) *string { return &value }
