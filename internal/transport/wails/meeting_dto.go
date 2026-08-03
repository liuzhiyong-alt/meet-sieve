package wails

import "meet-sieve/models"

// MeetingCreateDraftDTO 是开始页不会消耗序号的默认草稿。
type MeetingCreateDraftDTO struct {
	SuggestedMeetingNo string `json:"suggested_meeting_no"`
	DefaultSubject     string `json:"default_subject"`
}

// StartMeetingDTO 是 Wails 开始会议的稳定输入契约。
type StartMeetingDTO struct {
	MeetingNo                 string   `json:"meeting_no"`
	SuggestedMeetingNo        string   `json:"suggested_meeting_no"`
	Subject                   string   `json:"subject"`
	MemberIDs                 []string `json:"member_ids"`
	TemporaryParticipantNames []string `json:"temporary_participant_names"`
	MicrophoneID              string   `json:"microphone_id"`
	LocalTimezone             string   `json:"local_timezone"`
	ASRMode                   string   `json:"asr_mode"`
	LANEnabled                bool     `json:"lan_enabled"`
	LANInterfaceID            string   `json:"lan_interface_id"`
}

// MeetingProjectionDTO 是不泄漏工作目录绝对路径的会议运行状态。
type MeetingProjectionDTO struct {
	ID               string `json:"id"`
	MeetingNo        string `json:"meeting_no"`
	Subject          string `json:"subject"`
	LifecycleState   string `json:"lifecycle_state"`
	LocalSaveState   string `json:"local_save_state"`
	RealtimeASRState string `json:"realtime_asr_state"`
	AgentState       string `json:"agent_state"`
	StartedAt        *int64 `json:"started_at,omitempty"`
	EndedAt          *int64 `json:"ended_at,omitempty"`
	UpdatedAt        int64  `json:"updated_at"`
}

// ActiveMeetingDTO 明确区分“没有活动会议”和活动会议投影。
type ActiveMeetingDTO struct {
	Active  bool                  `json:"active"`
	Meeting *MeetingProjectionDTO `json:"meeting,omitempty"`
}

// MeetingStateEventDTO 是录音或本地保存单一状态轴的事件数据。
type MeetingStateEventDTO struct {
	MeetingID string `json:"meeting_id"`
	State     string `json:"state"`
}

// mapMeetingProjectionDTO 将数据库模型转换为安全前端投影。
func mapMeetingProjectionDTO(meeting models.Meeting) MeetingProjectionDTO {
	return MeetingProjectionDTO{
		ID: meeting.ID, MeetingNo: meeting.MeetingNo, Subject: meeting.Subject,
		LifecycleState: meeting.LifecycleState, LocalSaveState: meeting.LocalSaveState,
		RealtimeASRState: meeting.RealtimeASRState,
		AgentState:       meeting.AgentState,
		StartedAt:        meeting.StartedAt, EndedAt: meeting.EndedAt, UpdatedAt: meeting.UpdatedAt,
	}
}
