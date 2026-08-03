package wails

import (
	"context"
	"fmt"

	domaingap "meet-sieve/internal/domain/gap"
	gaprepository "meet-sieve/internal/repository/gap"
	finalizationservice "meet-sieve/internal/service/finalization"
	gapservice "meet-sieve/internal/service/gap"
)

// GapServices 是 gap 页面需要的命令与查询集合。
type GapServices struct {
	Repository *gaprepository.Repository
	Processor  *finalizationservice.PostMeetingProcessor
	Conflicts  *gapservice.ConflictQueryService
	Resolution *gapservice.ResolutionService
	Clips      *gapservice.AudioClipService
}

// GapServiceProvider 返回当前工作目录的 gap 服务。
type GapServiceProvider func() (GapServices, error)

// GapBinding 暴露补转写状态、开始、停止、重试与冲突解决。
type GapBinding struct {
	services GapServiceProvider
	boundary *Boundary
}

// NewGapBinding 创建 gap binding。
func NewGapBinding(services GapServiceProvider, boundary *Boundary) *GapBinding {
	return &GapBinding{services: services, boundary: boundary}
}

// GetGapState 从 SQLite 重建 gap 状态。
func (binding *GapBinding) GetGapState(meetingID string) Result[GapStateDTO] {
	return Invoke(binding.boundary, "wails.gap.get", func(string) (GapStateDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return GapStateDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return GapStateDTO{}, err
		}
		record, err := services.Repository.ReadState(context.Background(), meetingID)
		return mapGapState(meetingID, record), err
	})
}

// StartGapCompensation 异步启动首轮串行补转写。
func (binding *GapBinding) StartGapCompensation(meetingID string, requestID string) Result[GapCommandDTO] {
	return binding.trigger(meetingID, nil, requestID, "wails.gap.start")
}

// RetryGapCompensation 只重试主持人明确选择的 failed gap。
func (binding *GapBinding) RetryGapCompensation(meetingID string, gapIDs []string, requestID string) Result[GapCommandDTO] {
	return binding.trigger(meetingID, gapIDs, requestID, "wails.gap.retry")
}

// trigger 校验外部 ID 后取得后台单 owner。
func (binding *GapBinding) trigger(meetingID string, gapIDs []string, requestID string, operation string) Result[GapCommandDTO] {
	return Invoke(binding.boundary, operation, func(string) (GapCommandDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return GapCommandDTO{}, err
		}
		if err := requireUUID("request ID", requestID); err != nil {
			return GapCommandDTO{}, err
		}
		for _, gapID := range gapIDs {
			if err := requireUUID("gap ID", gapID); err != nil {
				return GapCommandDTO{}, err
			}
		}
		services, err := binding.services()
		if err != nil {
			return GapCommandDTO{}, err
		}
		accepted := services.Processor.TriggerRequested(meetingID, requestID, gapIDs)
		if !accepted {
			return GapCommandDTO{}, fmt.Errorf("补转写任务已在运行或不可用")
		}
		return GapCommandDTO{Accepted: true}, nil
	})
}

// StopGapCompensation 取消当前 attempt；状态由 runner 持久收敛。
func (binding *GapBinding) StopGapCompensation(meetingID string, attemptID string) Result[GapCommandDTO] {
	return Invoke(binding.boundary, "wails.gap.stop", func(string) (GapCommandDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return GapCommandDTO{}, err
		}
		if err := requireUUID("attempt ID", attemptID); err != nil {
			return GapCommandDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return GapCommandDTO{}, err
		}
		state, err := services.Repository.ReadState(context.Background(), meetingID)
		if err != nil || state.Attempt == nil || state.Attempt.ID != attemptID {
			return GapCommandDTO{}, gaprepository.ErrConflict
		}
		return GapCommandDTO{Accepted: services.Processor.StopMeeting(meetingID)}, nil
	})
}

// GetGapConflict 返回不含绝对路径的实时双份证据。
func (binding *GapBinding) GetGapConflict(meetingID string, gapID string) Result[GapConflictDTO] {
	return Invoke(binding.boundary, "wails.gap.conflict.get", func(string) (GapConflictDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return GapConflictDTO{}, err
		}
		if err := requireUUID("gap ID", gapID); err != nil {
			return GapConflictDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return GapConflictDTO{}, err
		}
		evidence, err := services.Conflicts.GetConflict(context.Background(), meetingID, gapID)
		if err != nil {
			return GapConflictDTO{}, err
		}
		clip, err := services.Clips.Create(context.Background(), meetingID, evidence.AudioClipID)
		return mapGapConflict(evidence, clip), err
	})
}

// ResolveGapConflict 提交枚举动作与逐条 revision edit。
func (binding *GapBinding) ResolveGapConflict(meetingID string, gapID string, revision int64, resolution string, edits []GapResolutionEditDTO, requestID string) Result[GapStateDTO] {
	return Invoke(binding.boundary, "wails.gap.conflict.resolve", func(string) (GapStateDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return GapStateDTO{}, err
		}
		if err := requireUUID("gap ID", gapID); err != nil {
			return GapStateDTO{}, err
		}
		if err := requireUUID("request ID", requestID); err != nil {
			return GapStateDTO{}, err
		}
		commandEdits := make([]gaprepository.ResolutionEdit, 0, len(edits))
		for _, edit := range edits {
			if err := requireUUID("target ID", edit.TargetID); err != nil {
				return GapStateDTO{}, err
			}
			commandEdits = append(commandEdits, gaprepository.ResolutionEdit{TargetID: edit.TargetID, ExpectedRevision: edit.ExpectedRevision, Text: edit.Text})
		}
		services, err := binding.services()
		if err != nil {
			return GapStateDTO{}, err
		}
		err = services.Resolution.Resolve(context.Background(), gapservice.ResolutionCommand{MeetingID: meetingID, GapID: gapID, Revision: revision, Resolution: domaingap.Resolution(resolution), Edits: commandEdits, RequestID: requestID, OperatorID: "host"})
		if err != nil {
			return GapStateDTO{}, err
		}
		state, err := services.Repository.ReadState(context.Background(), meetingID)
		return mapGapState(meetingID, state), err
	})
}

// mapGapState 转换明细并用最大 updated_at 作为重载 revision。
func mapGapState(meetingID string, record gaprepository.StateRecord) GapStateDTO {
	result := GapStateDTO{MeetingID: meetingID, State: record.Aggregate, Gaps: make([]GapItemDTO, 0, len(record.Gaps))}
	for _, gap := range record.Gaps {
		errorCode := ""
		if gap.LastErrorCode != nil {
			errorCode = *gap.LastErrorCode
		}
		result.Gaps = append(result.Gaps, GapItemDTO{ID: gap.ID, StartSample: gap.StartSample, EndSample: gap.EndSample, State: gap.State, AttemptCount: gap.AttemptCount, ErrorCode: errorCode})
		if gap.UpdatedAt > result.Revision {
			result.Revision = gap.UpdatedAt
		}
	}
	if record.Attempt != nil {
		result.ActiveAttemptID = record.Attempt.ID
	}
	return result
}

// mapGapConflict 转换规范化候选和当前事实。
func mapGapConflict(value gapservice.ConflictEvidence, clip gapservice.AudioClipResult) GapConflictDTO {
	result := GapConflictDTO{GapID: value.GapID, Revision: value.Revision, CoreStartSample: value.CoreStartSample, CoreEndSample: value.CoreEndSample, AudioStartSample: value.AudioStartSample, AudioEndSample: value.AudioEndSample, AudioClipURL: clip.URL, AudioClipExpires: clip.ExpiresAt, Candidates: make([]GapCandidateDTO, 0, len(value.Candidates)), Existing: mapConflictRows(value.Existing), Context: mapConflictRows(value.Context)}
	for _, candidate := range value.Candidates {
		result.Candidates = append(result.Candidates, GapCandidateDTO{Text: candidate.Text, SpeakerID: candidate.SpeakerID, StartSample: candidate.StartSample, EndSample: candidate.EndSample})
	}
	return result
}

// mapConflictRows 转换当前 utterance 证据。
func mapConflictRows(values []gaprepository.ConflictUtteranceRow) []GapConflictUtteranceDTO {
	result := make([]GapConflictUtteranceDTO, 0, len(values))
	for _, value := range values {
		result = append(result, GapConflictUtteranceDTO{ID: value.ID, Seq: value.Seq, OriginalText: value.OriginalText, CurrentText: value.CurrentText, StartSample: value.StartSample, EndSample: value.EndSample, TextRevision: value.TextRevision})
	}
	return result
}
