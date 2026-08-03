package wails

import (
	"context"
	"fmt"
	"time"

	meetingservice "meet-sieve/internal/service/meeting"
)

// MeetingServiceProvider 返回当前工作目录唯一的会议事务服务和录音运行时。
type MeetingServiceProvider func() (*meetingservice.Service, *meetingservice.RuntimeService, *meetingservice.RecoveryService, error)

// MeetingEventEmitter 把版本化会议状态事件发送给当前 Wails 窗口。
type MeetingEventEmitter func(ctx context.Context, name string, data any)

// MeetingBinding 暴露创建草稿、开始、活动查询和安全结束的最小契约。
type MeetingBinding struct {
	services        MeetingServiceProvider
	contextProvider ContextProvider
	emit            MeetingEventEmitter
	boundary        *Boundary
}

// NewMeetingBinding 创建会议 Binding，不在构造阶段打开数据库或麦克风。
func NewMeetingBinding(services MeetingServiceProvider, contextProvider ContextProvider, emit MeetingEventEmitter, boundary *Boundary) *MeetingBinding {
	return &MeetingBinding{services: services, contextProvider: contextProvider, emit: emit, boundary: boundary}
}

// GetCreateDraft 返回不会消耗每日序号的开始页草稿。
func (binding *MeetingBinding) GetCreateDraft() Result[MeetingCreateDraftDTO] {
	return Invoke(binding.boundary, "wails.meeting.draft", func(_ string) (MeetingCreateDraftDTO, error) {
		meetings, _, _, err := binding.services()
		if err != nil {
			return MeetingCreateDraftDTO{}, err
		}
		draft, err := meetings.GetCreateDraft(context.Background())
		return MeetingCreateDraftDTO{SuggestedMeetingNo: draft.SuggestedMeetingNo, DefaultSubject: draft.DefaultSubject}, err
	})
}

// GetActiveMeeting 在页面刷新后返回真实活动会议，而不是依赖前端内存状态。
func (binding *MeetingBinding) GetActiveMeeting() Result[ActiveMeetingDTO] {
	return Invoke(binding.boundary, "wails.meeting.active", func(_ string) (ActiveMeetingDTO, error) {
		meetings, _, _, err := binding.services()
		if err != nil {
			return ActiveMeetingDTO{}, err
		}
		active, err := meetings.GetActiveMeeting(context.Background())
		if err != nil || active == nil {
			return ActiveMeetingDTO{}, err
		}
		projection := mapMeetingProjectionDTO(*active)
		return ActiveMeetingDTO{Active: true, Meeting: &projection}, nil
	})
}

// GetLatestInterruptedMeeting 返回恢复页所需的最近中断会议，不允许从该投影续录。
func (binding *MeetingBinding) GetLatestInterruptedMeeting() Result[ActiveMeetingDTO] {
	return Invoke(binding.boundary, "wails.meeting.interrupted", func(_ string) (ActiveMeetingDTO, error) {
		meetings, _, _, err := binding.services()
		if err != nil {
			return ActiveMeetingDTO{}, err
		}
		interrupted, err := meetings.GetLatestInterruptedMeeting(context.Background())
		if err != nil || interrupted == nil {
			return ActiveMeetingDTO{}, err
		}
		projection := mapMeetingProjectionDTO(*interrupted)
		return ActiveMeetingDTO{Meeting: &projection}, nil
	})
}

// StartMeeting 执行真实预检与首帧提交，并分别发布录音和本地保存状态。
func (binding *MeetingBinding) StartMeeting(input StartMeetingDTO) Result[MeetingProjectionDTO] {
	return Invoke(binding.boundary, "wails.meeting.start", func(_ string) (MeetingProjectionDTO, error) {
		_, runtimeService, _, err := binding.services()
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		asrMode := input.ASRMode
		if asrMode == "" {
			asrMode = "realtime"
		}
		meeting, err := runtimeService.StartMeeting(ctx, meetingservice.StartMeetingInput{
			CreatePreparingInput: meetingservice.CreatePreparingInput{
				MeetingNo: input.MeetingNo, SuggestedMeetingNo: input.SuggestedMeetingNo,
				Subject: input.Subject, MemberIDs: input.MemberIDs,
				TemporaryParticipantNames: input.TemporaryParticipantNames, LocalTimezone: input.LocalTimezone,
			},
			DeviceID: input.MicrophoneID, ASRMode: asrMode,
			LANEnabled: input.LANEnabled, LANInterfaceID: input.LANInterfaceID,
		})
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		binding.emitStates(ctx, meeting.ID, meeting.LifecycleState, meeting.LocalSaveState, meeting.RealtimeASRState)
		return mapMeetingProjectionDTO(meeting), nil
	})
}

// EndMeeting 复用唯一收尾流程，并分别发布录音和本地保存终态。
func (binding *MeetingBinding) EndMeeting(meetingID string) Result[MeetingProjectionDTO] {
	return Invoke(binding.boundary, "wails.meeting.end", func(_ string) (MeetingProjectionDTO, error) {
		_, runtimeService, _, err := binding.services()
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		meeting, err := runtimeService.EndMeeting(ctx, meetingID)
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		binding.emitStates(ctx, meeting.ID, meeting.LifecycleState, meeting.LocalSaveState, meeting.RealtimeASRState)
		return mapMeetingProjectionDTO(meeting), nil
	})
}

// RetryMeetingRecovery 重试中断会议的文件对账和合并，不恢复原录音 stream。
func (binding *MeetingBinding) RetryMeetingRecovery(meetingID string) Result[MeetingProjectionDTO] {
	return Invoke(binding.boundary, "wails.meeting.recovery.retry", func(_ string) (MeetingProjectionDTO, error) {
		meetings, _, recovery, err := binding.services()
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return MeetingProjectionDTO{}, err
		}
		if _, err := recovery.RetryInterruptedMeeting(ctx, meetingID); err != nil {
			return MeetingProjectionDTO{}, err
		}
		meeting, err := meetings.GetLatestInterruptedMeeting(ctx)
		if err != nil || meeting == nil {
			return MeetingProjectionDTO{}, err
		}
		binding.emitStates(ctx, meeting.ID, meeting.LifecycleState, meeting.LocalSaveState, meeting.RealtimeASRState)
		return mapMeetingProjectionDTO(*meeting), nil
	})
}

// currentContext 返回 Wails 生命周期根 context，避免录音绑定到短生命请求对象。
func (binding *MeetingBinding) currentContext() (context.Context, error) {
	if binding == nil || binding.contextProvider == nil {
		return nil, fmt.Errorf("Wails context 尚未就绪")
	}
	ctx := binding.contextProvider()
	if ctx == nil {
		return nil, fmt.Errorf("Wails context 尚未就绪")
	}
	return ctx, nil
}

// emitStates 以两个独立事件表达录音生命周期和本地保存状态。
func (binding *MeetingBinding) emitStates(ctx context.Context, meetingID string, lifecycleState string, localSaveState string, realtimeASRState string) {
	if binding.emit == nil {
		return
	}
	now := time.Now()
	binding.emit(ctx, "meeting.recording.changed", NewEvent(
		"meeting.recording.changed", now, 0, MeetingStateEventDTO{MeetingID: meetingID, State: lifecycleState},
	))
	binding.emit(ctx, "meeting.local_save.changed", NewEvent(
		"meeting.local_save.changed", now, 0, MeetingStateEventDTO{MeetingID: meetingID, State: localSaveState},
	))
	binding.emit(ctx, "meeting.asr.changed", NewEvent(
		"meeting.asr.changed", now, 0, MeetingStateEventDTO{MeetingID: meetingID, State: realtimeASRState},
	))
}
