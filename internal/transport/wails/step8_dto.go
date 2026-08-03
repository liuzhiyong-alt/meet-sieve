package wails

// Step8ChangedEventDTO 是四类会后事件共享的安全失效通知。
type Step8ChangedEventDTO struct {
	MeetingID string `json:"meeting_id"`
	State     string `json:"state"`
	Stage     string `json:"stage,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Revision  uint64 `json:"revision"`
	Completed int    `json:"completed,omitempty"`
	Total     int    `json:"total,omitempty"`
	Retryable bool   `json:"retryable"`
}

// FinalizationStateDTO 是核心本地收尾的可恢复投影。
type FinalizationStateDTO struct {
	MeetingID string `json:"meeting_id"`
	State     string `json:"state"`
	Stage     string `json:"stage"`
	ErrorCode string `json:"error_code,omitempty"`
	Revision  uint64 `json:"revision"`
}

// GapItemDTO 是不包含文件路径的缺口明细。
type GapItemDTO struct {
	ID           string `json:"id"`
	StartSample  int64  `json:"start_sample"`
	EndSample    int64  `json:"end_sample"`
	State        string `json:"state"`
	AttemptCount int    `json:"attempt_count"`
	ErrorCode    string `json:"error_code,omitempty"`
}

// GapStateDTO 是 gap 聚合和活动请求状态。
type GapStateDTO struct {
	MeetingID       string       `json:"meeting_id"`
	State           string       `json:"state"`
	Gaps            []GapItemDTO `json:"gaps"`
	ActiveAttemptID string       `json:"active_attempt_id,omitempty"`
	Revision        int64        `json:"revision"`
}

// GapCommandDTO 返回后台命令是否取得 owner。
type GapCommandDTO struct {
	Accepted bool `json:"accepted"`
}

// GapConflictUtteranceDTO 是冲突页的当前正式文字。
type GapConflictUtteranceDTO struct {
	ID           string `json:"id"`
	Seq          int64  `json:"seq"`
	OriginalText string `json:"original_text"`
	CurrentText  string `json:"current_text"`
	StartSample  int64  `json:"start_sample"`
	EndSample    int64  `json:"end_sample"`
	TextRevision int    `json:"text_revision"`
}

// GapCandidateDTO 是文件 ASR 的有限规范化候选。
type GapCandidateDTO struct {
	Text        string `json:"text"`
	SpeakerID   string `json:"speaker_id,omitempty"`
	StartSample int64  `json:"start_sample"`
	EndSample   int64  `json:"end_sample"`
}

// GapConflictDTO 不泄漏绝对路径或资产 ID，只返回短期回放 URL 和双份证据。
type GapConflictDTO struct {
	GapID            string                    `json:"gap_id"`
	Revision         int64                     `json:"revision"`
	CoreStartSample  int64                     `json:"core_start_sample"`
	CoreEndSample    int64                     `json:"core_end_sample"`
	AudioStartSample int64                     `json:"audio_start_sample"`
	AudioEndSample   int64                     `json:"audio_end_sample"`
	AudioClipURL     string                    `json:"audio_clip_url"`
	AudioClipExpires int64                     `json:"audio_clip_expires_at"`
	Candidates       []GapCandidateDTO         `json:"candidates"`
	Existing         []GapConflictUtteranceDTO `json:"existing"`
	Context          []GapConflictUtteranceDTO `json:"context"`
}

// GapResolutionEditDTO 是主持人明确提交的逐条 revision 更新。
type GapResolutionEditDTO struct {
	TargetID         string `json:"target_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Text             string `json:"text"`
}

// MinuteVersionDTO 是不可变纪要版本的安全前端投影。
type MinuteVersionDTO struct {
	ID              string `json:"id"`
	VersionNo       int    `json:"version_no"`
	Source          string `json:"source"`
	ContentMarkdown string `json:"content_markdown"`
	State           string `json:"state"`
	IsCurrent       bool   `json:"is_current"`
	ConfirmedAt     *int64 `json:"confirmed_at,omitempty"`
	CreatedAt       int64  `json:"created_at"`
}

// MinutesStateDTO 是当前、候选、失败和运行态的组合投影。
type MinutesStateDTO struct {
	MeetingID       string            `json:"meeting_id"`
	State           string            `json:"state"`
	Current         *MinuteVersionDTO `json:"current,omitempty"`
	LatestCandidate *MinuteVersionDTO `json:"latest_candidate,omitempty"`
	RecentErrorCode string            `json:"recent_error_code,omitempty"`
	TurnID          string            `json:"turn_id,omitempty"`
	RuntimeState    string            `json:"runtime_state"`
	ProjectionState string            `json:"projection_state"`
	Revision        uint64            `json:"revision"`
}

// MinuteMutationDTO 返回版本写入结果和独立文件投影状态。
type MinuteMutationDTO struct {
	Version         MinuteVersionDTO `json:"version"`
	ProjectionState string           `json:"projection_state"`
	ProjectionError string           `json:"projection_error,omitempty"`
}

// MinuteVersionPageDTO 返回基于版本号游标的历史页。
type MinuteVersionPageDTO struct {
	Items      []MinuteVersionDTO `json:"items"`
	NextCursor int                `json:"next_cursor,omitempty"`
}
