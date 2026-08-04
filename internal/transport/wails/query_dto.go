package wails

import queryservice "meet-sieve/internal/service/query"

// MeetingListInputDTO 是会议记录查询的外部输入。
type MeetingListInputDTO struct {
	Search string `json:"search"`
	Status string `json:"status"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// SeqPageInputDTO 是长列表按事件序号读取的外部输入。
type SeqPageInputDTO struct {
	MeetingID string `json:"meeting_id"`
	AfterSeq  int64  `json:"after_seq"`
	BeforeSeq int64  `json:"before_seq"`
	Limit     int    `json:"limit"`
}

// MeetingPrimaryActionDTO 是前端只负责导航的稳定会议主动作。
type MeetingPrimaryActionDTO struct {
	Kind           string `json:"kind"`
	Label          string `json:"label"`
	TargetID       string `json:"target_id,omitempty"`
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason,omitempty"`
}

// MeetingSummaryDTO 是首页和记录页共用的安全会议摘要。
type MeetingSummaryDTO struct {
	ID                   string                  `json:"id"`
	MeetingNo            string                  `json:"meeting_no"`
	Subject              string                  `json:"subject"`
	StartedAt            int64                   `json:"started_at"`
	EndedAt              *int64                  `json:"ended_at,omitempty"`
	LifecycleState       string                  `json:"lifecycle_state"`
	LocalSaveState       string                  `json:"local_save_state"`
	RealtimeASRState     string                  `json:"realtime_asr_state"`
	GapState             string                  `json:"gap_state"`
	AgentState           string                  `json:"agent_state"`
	MinuteState          string                  `json:"minute_state"`
	LANState             string                  `json:"lan_state"`
	Participants         []string                `json:"participants"`
	ParticipantMemberIDs []string                `json:"participant_member_ids"`
	HighestStatus        string                  `json:"highest_status"`
	PrimaryAction        MeetingPrimaryActionDTO `json:"primary_action"`
}

// MeetingPageDTO 是不含总数的会议记录页。
type MeetingPageDTO struct {
	Items          []MeetingSummaryDTO `json:"items"`
	NextCursor     string              `json:"next_cursor,omitempty"`
	PreviousCursor string              `json:"previous_cursor,omitempty"`
}

// HomeDTO 是首页继续处理和最近会议投影。
type HomeDTO struct {
	Continuation   *MeetingSummaryDTO  `json:"continuation,omitempty"`
	Remaining      int                 `json:"remaining"`
	RecentMeetings []MeetingSummaryDTO `json:"recent_meetings"`
}

// MeetingDetailDTO 是会议详情全部状态轴和能力投影。
type MeetingDetailDTO struct {
	Summary            MeetingSummaryDTO `json:"summary"`
	CanPlayAudio       bool              `json:"can_play_audio"`
	CanRetranscribe    bool              `json:"can_retranscribe"`
	CanDeleteRecording bool              `json:"can_delete_recording"`
	CanDeleteMeeting   bool              `json:"can_delete_meeting"`
	DisabledReason     string            `json:"disabled_reason,omitempty"`
}

// TranscriptItemDTO 是原始记录的有限事实投影。
type TranscriptItemDTO struct {
	Seq         int64  `json:"seq"`
	Kind        string `json:"kind"`
	OccurredAt  int64  `json:"occurred_at"`
	Text        string `json:"text,omitempty"`
	SpeakerName string `json:"speaker_name,omitempty"`
	StartSample int64  `json:"start_sample,omitempty"`
	EndSample   int64  `json:"end_sample,omitempty"`
}

// TranscriptPageDTO 是原始记录 seq 页。
type TranscriptPageDTO struct {
	Items     []TranscriptItemDTO `json:"items"`
	HasMore   bool                `json:"has_more"`
	AfterSeq  int64               `json:"after_seq,omitempty"`
	BeforeSeq int64               `json:"before_seq,omitempty"`
}

// ContentItemDTO 是消息、附件、链接或公开 AI 回答投影。
type ContentItemDTO struct {
	Seq           int64  `json:"seq"`
	Kind          string `json:"kind"`
	OccurredAt    int64  `json:"occurred_at"`
	EntityID      string `json:"entity_id"`
	DisplayName   string `json:"display_name,omitempty"`
	Text          string `json:"text,omitempty"`
	ResourceKind  string `json:"resource_kind,omitempty"`
	ResourceName  string `json:"resource_name,omitempty"`
	ResourceState string `json:"resource_state,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	DisplayURL    string `json:"display_url,omitempty"`
}

// ContentPageDTO 是会议内容 seq 页。
type ContentPageDTO struct {
	Items     []ContentItemDTO `json:"items"`
	HasMore   bool             `json:"has_more"`
	AfterSeq  int64            `json:"after_seq,omitempty"`
	BeforeSeq int64            `json:"before_seq,omitempty"`
}

// InterruptedRecoveryDTO 是中断恢复入口的稳定摘要。
type InterruptedRecoveryDTO struct {
	Meeting          MeetingSummaryDTO `json:"meeting"`
	CanRetry         bool              `json:"can_retry"`
	DisabledReason   string            `json:"disabled_reason,omitempty"`
	SegmentCount     int               `json:"segment_count"`
	DurationSamples  int64             `json:"duration_samples"`
	SampleRate       int               `json:"sample_rate"`
	FirstSequence    int               `json:"first_sequence"`
	LastSequence     int               `json:"last_sequence"`
	GapCount         int               `json:"gap_count"`
	PendingGapCount  int               `json:"pending_gap_count"`
	ReadyFileCount   int               `json:"ready_file_count"`
	FailedFileCount  int               `json:"failed_file_count"`
	DeletedFileCount int               `json:"deleted_file_count"`
	FailureStage     string            `json:"failure_stage,omitempty"`
}

// mapMeetingSummaryDTO 转换 Repository 行并复制 slice，避免跨层共享可变数据。
func mapMeetingSummaryDTO(row queryservice.MeetingSummary) MeetingSummaryDTO {
	return MeetingSummaryDTO{
		ID: row.ID, MeetingNo: row.MeetingNo, Subject: row.Subject, StartedAt: row.StartedAt, EndedAt: row.EndedAt,
		LifecycleState: row.LifecycleState, LocalSaveState: row.LocalSaveState, RealtimeASRState: row.RealtimeASRState,
		GapState: row.GapState, AgentState: row.AgentState, MinuteState: row.MinuteState, LANState: row.LANState,
		Participants: append([]string(nil), row.Participants...), ParticipantMemberIDs: append([]string(nil), row.ParticipantMemberIDs...), HighestStatus: string(row.HighestStatus),
		PrimaryAction: MeetingPrimaryActionDTO{
			Kind: row.PrimaryAction.Kind, Label: row.PrimaryAction.Label, TargetID: row.PrimaryAction.TargetID,
			Enabled: row.PrimaryAction.Enabled, DisabledReason: row.PrimaryAction.DisabledReason,
		},
	}
}

// mapMeetingPageDTO 转换会议记录页。
func mapMeetingPageDTO(page queryservice.MeetingPage) MeetingPageDTO {
	items := make([]MeetingSummaryDTO, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, mapMeetingSummaryDTO(item))
	}
	return MeetingPageDTO{Items: items, NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor}
}
