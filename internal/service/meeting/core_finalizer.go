package meeting

import (
	"context"

	domainfinalization "meet-sieve/internal/domain/finalization"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/models"
)

// CoreFinalizer 严格按阶段完成本地事实收尾，不等待任何会后网络任务。
type CoreFinalizer struct {
	service  *RuntimeService
	meeting  models.Meeting
	segments []CompletedSegment
}

// Run 顺序执行停止、尾部 final、资产、合并、投影和 saved 提交。
func (finalizer *CoreFinalizer) Run(ctx context.Context) (models.Meeting, error) {
	if err := finalizer.stopCapture(ctx); err != nil {
		return models.Meeting{}, err
	}
	if err := finalizer.waitTailFinal(ctx); err != nil {
		return models.Meeting{}, err
	}
	if err := finalizer.persistTranscript(ctx); err != nil {
		return models.Meeting{}, err
	}
	if err := finalizer.mergeRecording(ctx); err != nil {
		return models.Meeting{}, err
	}
	if err := finalizer.flushRawRecord(ctx); err != nil {
		return models.Meeting{}, err
	}
	return finalizer.commitSaved(ctx)
}

// stopCapture 中断会中 Agent turn 后幂等停止录音设备。
func (finalizer *CoreFinalizer) stopCapture(ctx context.Context) error {
	finalizer.service.setFinalizationState(finalizer.meeting.ID, "running", domainfinalization.StageStopCapture, "")
	if finalizer.service.agentTurns != nil {
		_ = finalizer.service.agentTurns.InterruptMeeting(ctx, finalizer.meeting.ID)
	}
	segments, err := finalizer.service.coordinator.Stop(ctx)
	if err != nil {
		return finalizer.service.failFinalizing(finalizer.meeting.ID, err, apperr.CodeMeetingRecordingWriteFailed, "meeting.end.stop")
	}
	finalizer.segments = segments
	return nil
}

// waitTailFinal 等待实时转写尾部终结；内部 15 秒边界会生成精确 tail gap。
func (finalizer *CoreFinalizer) waitTailFinal(ctx context.Context) error {
	finalizer.service.setFinalizationState(finalizer.meeting.ID, "running", domainfinalization.StageWaitTailFinal, "")
	if finalizer.service.transcript == nil {
		return nil
	}
	if err := finalizer.service.transcript.Stop(ctx, finalizer.meeting.ID, recordingEndSample(finalizer.segments)); err != nil {
		return finalizer.service.failFinalizing(finalizer.meeting.ID, err, apperr.CodeASREventPersistFailed, "meeting.end.transcript")
	}
	return nil
}

// persistTranscript 登记全部已完成 microphone 分片。
func (finalizer *CoreFinalizer) persistTranscript(ctx context.Context) error {
	finalizer.service.setFinalizationState(finalizer.meeting.ID, "running", domainfinalization.StagePersistTranscript, "")
	if err := finalizer.service.persistSegments(ctx, finalizer.meeting.ID, finalizer.segments); err != nil {
		return finalizer.service.failFinalizing(finalizer.meeting.ID, err, apperr.CodeMeetingRecordingWriteFailed, "meeting.end.segment_asset")
	}
	return nil
}

// mergeRecording 复用匹配的 ready recording.wav，否则合并并验证。
func (finalizer *CoreFinalizer) mergeRecording(ctx context.Context) error {
	finalizer.service.setFinalizationState(finalizer.meeting.ID, "running", domainfinalization.StageMergeRecording, "")
	if err := finalizer.service.mergeAndPersistFinal(ctx, finalizer.meeting, finalizer.segments); err != nil {
		return finalizer.service.failFinalizing(finalizer.meeting.ID, err, apperr.CodeMeetingRecordingMergeFailed, "meeting.end.merge")
	}
	return nil
}

// flushRawRecord 保证本地 saved 前 Markdown 已追上 SQLite。
func (finalizer *CoreFinalizer) flushRawRecord(ctx context.Context) error {
	finalizer.service.setFinalizationState(finalizer.meeting.ID, "running", domainfinalization.StageFlushRawRecord, "")
	if finalizer.service.rawRecord == nil {
		return nil
	}
	if err := finalizer.service.rawRecord.Flush(ctx, finalizer.meeting.ID); err != nil {
		return finalizer.service.failFinalizing(finalizer.meeting.ID, err, apperr.CodeRawRecordRefreshFailed, "meeting.end.raw_record_flush")
	}
	return nil
}

// commitSaved 以核心收尾完成时间写 ended_at，并读取真实持久投影。
func (finalizer *CoreFinalizer) commitSaved(ctx context.Context) (models.Meeting, error) {
	finalizer.service.setFinalizationState(finalizer.meeting.ID, "running", domainfinalization.StageCommitLocalSaved, "")
	endedAt := finalizer.service.clock.Now().UnixMilli()
	if err := finalizer.service.repository.CompleteMeeting(ctx, finalizer.meeting.ID, endedAt); err != nil {
		return models.Meeting{}, finalizer.service.failFinalizing(finalizer.meeting.ID, err, apperr.CodeMeetingRecordingWriteFailed, "meeting.end.state_commit")
	}
	result, err := finalizer.service.repository.GetMeeting(ctx, finalizer.meeting.ID)
	if err == nil {
		finalizer.service.setFinalizationState(finalizer.meeting.ID, "completed", domainfinalization.StageCommitLocalSaved, "")
	}
	return result, err
}
