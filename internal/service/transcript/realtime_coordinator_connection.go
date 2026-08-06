package transcript

import (
	"context"
	"fmt"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
	"meet-sieve/models"

	"gorm.io/gorm"
)

func (coordinator *RealtimeCoordinator) connect(inputStart int64, reconnectCount int) error {
	coordinator.mu.Lock()
	coordinator.generation++
	generation := coordinator.generation
	meetingID, credentials, runContext := coordinator.meetingID, coordinator.credentials, coordinator.ctx
	coordinator.mu.Unlock()
	sessionID := coordinator.dependencies.IDs.New()
	if sessionID == "" {
		return fmt.Errorf("生成 ASR session ID 失败")
	}
	transport, err := credentials.Mode.Transport()
	if err != nil {
		return err
	}
	now := coordinator.dependencies.Clock.Now().UnixMilli()
	row := models.ASRSession{ID: sessionID, MeetingID: meetingID, Provider: "volcano", State: "connecting", StartedAt: now, ReconnectCount: reconnectCount, TransportMode: string(transport), InputStartSample: inputStart, LastSentSample: inputStart, LastFinalSample: inputStart, CreatedAt: now, UpdatedAt: now}
	if err = coordinator.dependencies.Transactions.WithinTransaction(runContext, func(tx *gorm.DB) error {
		return coordinator.dependencies.Repository.CreateSession(runContext, tx, row, "connecting")
	}); err != nil {
		return err
	}
	transcriber := coordinator.dependencies.Transcriber(credentials)
	if transcriber == nil {
		coordinator.finishSession(sessionID, "failed", "reconnecting", pointerString(apperr.CodeASRStreamInterrupted.ErrorCode))
		return fmt.Errorf("实时转写 adapter 不可用")
	}
	request := port.RealtimeTranscriptionRequest{MeetingID: meetingID, Format: port.AudioFormat{SampleRate: 16000, BitsPerSample: 16, Channels: 1}, StartSample: inputStart}
	remote, err := coordinator.startTranscriber(runContext, transcriber, request)
	if err != nil {
		coordinator.finishSession(sessionID, "failed", "reconnecting", pointerString(errorCodeOf(err)))
		return err
	}
	physical := &physicalSession{id: sessionID, generation: generation, inputStart: inputStart, remote: remote, senderDone: make(chan struct{})}
	coordinator.mu.Lock()
	if coordinator.stopping {
		coordinator.mu.Unlock()
		_ = remote.Stop(context.Background())
		return fmt.Errorf("实时转写已进入停止流程")
	}
	coordinator.current = physical
	coordinator.reconnectCount = reconnectCount
	coordinator.mu.Unlock()
	coordinator.markStreaming(physical, "")
	if err = coordinator.closeOpenGap(runContext, inputStart); err != nil {
		_ = remote.Stop(context.Background())
		return err
	}
	coordinator.publishState("streaming", "")
	go coordinator.sendFrames(physical)
	go coordinator.readEvents(physical)
	return nil
}

type transcriberStartResult struct {
	remote port.RealtimeTranscriptionSession
	err    error
}

// startTranscriber 为完整 adapter Start 设置硬边界；迟到成功的连接会立即关闭，避免资源泄漏。
func (coordinator *RealtimeCoordinator) startTranscriber(ctx context.Context, transcriber port.RealtimeTranscriber, request port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	attemptContext, cancel := context.WithTimeout(ctx, coordinator.dependencies.ConnectTimeout)
	defer cancel()
	result := make(chan transcriberStartResult)
	go func() {
		remote, err := transcriber.Start(attemptContext, request)
		select {
		case result <- transcriberStartResult{remote: remote, err: err}:
		case <-attemptContext.Done():
			stopLateTranscriber(remote)
		}
	}()
	select {
	case started := <-result:
		return started.remote, started.err
	case <-attemptContext.Done():
		return nil, apperr.Dependency(apperr.CodeASRConnectTimeout, attemptContext.Err(), apperr.WithOp("transcript.realtime.connect"))
	}
}

// stopLateTranscriber 回收在超时后才返回的物理连接，且不阻塞重连监督器。
func stopLateTranscriber(remote port.RealtimeTranscriptionSession) {
	if remote == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = remote.Stop(ctx)
}

// sendFrames 是当前物理连接唯一 PCM 消费者和 writer。
func (coordinator *RealtimeCoordinator) sendFrames(current *physicalSession) {
	defer close(current.senderDone)
	for {
		frame, ok, err := coordinator.queue.Take(coordinator.ctx)
		if err != nil || !ok {
			return
		}
		if err = current.remote.WriteFrame(coordinator.ctx, frame); err != nil {
			resumeAt := frame.StartSample + int64(len(frame.PCM)/2)
			coordinator.reportFailure(realtimeFailure{generation: current.generation, code: errorCodeOf(err), retryable: true, reason: transcriptdomain.GapDisconnected, resumeAt: resumeAt, cause: err})
			return
		}
		lastSent := current.remote.LastSentSample()
		if lastSent > current.inputStart {
			coordinator.advanceSent(current, lastSent)
		}
	}
}

// readEvents 归一化处理 lifecycle、partial、final 与安全失败事件。
func (coordinator *RealtimeCoordinator) readEvents(current *physicalSession) {
	for event := range current.remote.Events() {
		if !coordinator.isCurrentGeneration(current) {
			return
		}
		event.MeetingID, event.SessionID, event.Generation = coordinator.meetingID, current.id, current.generation
		switch event.Type {
		case port.TranscriptionSessionStarted:
			coordinator.markStreaming(current, event.ProviderSessionID)
		case port.TranscriptionPartial:
			coordinator.partials.Accept(event)
		case port.TranscriptionFinal:
			if !coordinator.finals.TrySubmit(event) {
				coordinator.reportFailure(realtimeFailure{generation: current.generation, code: apperr.CodeASREventBackpressure.ErrorCode, reason: transcriptdomain.GapBackpressure, cause: fmt.Errorf("final 处理队列已满")})
				return
			}
			coordinator.partials.Clear(event)
		case port.TranscriptionFailed:
			failure := realtimeFailure{generation: current.generation, code: apperr.CodeASRStreamInterrupted.ErrorCode, retryable: true, reason: transcriptdomain.GapDisconnected}
			if event.Failure != nil {
				failure.code, failure.retryable, failure.cause = event.Failure.Code, event.Failure.Retryable, event.Failure.Cause
			}
			coordinator.reportFailure(failure)
			return
		}
	}
	coordinator.mu.Lock()
	stopping := coordinator.stopping
	coordinator.mu.Unlock()
	if !stopping {
		coordinator.reportFailure(realtimeFailure{generation: current.generation, code: apperr.CodeASRStreamInterrupted.ErrorCode, retryable: true, reason: transcriptdomain.GapDisconnected, cause: fmt.Errorf("ASR 事件流已关闭")})
	}
}

// isCurrentGeneration 拒绝旧物理 session 在重连或暂停后到达的迟到事件。
func (coordinator *RealtimeCoordinator) isCurrentGeneration(current *physicalSession) bool {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return current != nil && coordinator.current == current && coordinator.generation == current.generation
}

// supervise 串行取得物理 session 失败处理权，避免 reader/writer 双重重连。
func (coordinator *RealtimeCoordinator) supervise() {
	for {
		select {
		case <-coordinator.ctx.Done():
			return
		case failure := <-coordinator.failures:
			coordinator.handleFailure(failure)
		}
	}
}

// handleFailure 关闭旧 session，并按固定退避创建新物理连接。
func (coordinator *RealtimeCoordinator) handleFailure(failure realtimeFailure) {
	coordinator.mu.Lock()
	if coordinator.stopping || coordinator.unavailable || (failure.generation != 0 && failure.generation != coordinator.generation) {
		coordinator.mu.Unlock()
		return
	}
	current := coordinator.current
	coordinator.current = nil
	lastSent := coordinator.lastSent
	resumeAt := failure.resumeAt
	if resumeAt < lastSent {
		resumeAt = lastSent
	}
	startAttempt := coordinator.reconnectCount
	coordinator.mu.Unlock()
	sessionID := ""
	if current != nil {
		sessionID = current.id
	}
	coordinator.publishFailureReport(failure, sessionID, startAttempt)
	if current != nil {
		closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = current.remote.Stop(closeContext)
		cancel()
		coordinator.finishSession(current.id, "failed", "reconnecting", pointerString(failure.code))
		coordinator.openGap(lastSent, failure.reason, &current.id)
	}
	if current != nil {
		coordinator.partials.ClearAll(coordinator.meetingID, current.id, current.generation)
	}
	if !failure.retryable {
		coordinator.markUnavailable(failure.code)
		return
	}
	coordinator.publishState("reconnecting", failure.code)
	for attempt := startAttempt; attempt < len(coordinator.dependencies.Backoff); attempt++ {
		if err := coordinator.dependencies.Wait(coordinator.ctx, coordinator.dependencies.Backoff[attempt]); err != nil {
			return
		}
		if err := coordinator.connect(resumeAt, attempt+1); err == nil {
			return
		} else {
			failure.code = errorCodeOf(err)
		}
	}
	coordinator.markUnavailable(failure.code)
}

// publishFailureReport 只上报已经取得状态机处理权的失败，避免 reader/writer 重复记录。
func (coordinator *RealtimeCoordinator) publishFailureReport(failure realtimeFailure, sessionID string, reconnectCount int) {
	if coordinator.dependencies.ReportFailure == nil {
		return
	}
	coordinator.dependencies.ReportFailure(RealtimeFailureReport{
		MeetingID: coordinator.meetingID, SessionID: sessionID, ReconnectCount: reconnectCount,
		ErrorCode: failure.code, Cause: failure.cause,
	})
}

// reportFailure 无阻塞提交安全失败；队列异常饱和时直接标记不可用。
func (coordinator *RealtimeCoordinator) reportFailure(failure realtimeFailure) {
	coordinator.mu.Lock()
	if failure.generation == 0 {
		failure.generation = coordinator.generation
	}
	coordinator.mu.Unlock()
	select {
	case coordinator.failures <- failure:
	default:
		coordinator.markUnavailable(apperr.CodeASREventBackpressure.ErrorCode)
	}
}

// persistFinal 把 adapter final 映射到统一事件事务，并推进安全 final 游标。
