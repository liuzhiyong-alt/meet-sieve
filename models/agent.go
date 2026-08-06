package models

// AgentSession 映射会议内 Codex 会话。
type AgentSession struct {
	ID                   string  `gorm:"column:id"`
	MeetingID            string  `gorm:"column:meeting_id"`
	Provider             string  `gorm:"column:provider"`
	ThreadID             *string `gorm:"column:thread_id"`
	CWDRelativePath      string  `gorm:"column:cwd_relative_path"`
	State                string  `gorm:"column:state"`
	ResumedFromSessionID *string `gorm:"column:resumed_from_session_id"`
	StartedAt            int64   `gorm:"column:started_at"`
	EndedAt              *int64  `gorm:"column:ended_at"`
	LastErrorCode        *string `gorm:"column:last_error_code"`
	CreatedAt            int64   `gorm:"column:created_at"`
	UpdatedAt            int64   `gorm:"column:updated_at"`
}

// TableName 返回 AgentSession 的显式数据库表名。
func (AgentSession) TableName() string { return "agent_sessions" }

// AgentTurn 映射 Codex 的一次可重试工作单元。
type AgentTurn struct {
	ID              string  `gorm:"column:id"`
	MeetingID       string  `gorm:"column:meeting_id"`
	AgentSessionID  string  `gorm:"column:agent_session_id"`
	ProviderTurnID  *string `gorm:"column:provider_turn_id"`
	Kind            string  `gorm:"column:kind"`
	State           string  `gorm:"column:state"`
	IdempotencyKey  string  `gorm:"column:idempotency_key"`
	QuestionEventID *string `gorm:"column:question_event_id"`
	AnswerEventID   *string `gorm:"column:answer_event_id"`
	StartedAt       *int64  `gorm:"column:started_at"`
	EndedAt         *int64  `gorm:"column:ended_at"`
	LastErrorCode   *string `gorm:"column:last_error_code"`
	CreatedAt       int64   `gorm:"column:created_at"`
	UpdatedAt       int64   `gorm:"column:updated_at"`
}

// TableName 返回 AgentTurn 的显式数据库表名。
func (AgentTurn) TableName() string { return "agent_turns" }

// AgentVoiceCommandUtterance 映射一条语音指令对持久 ASR final 的用途关系。
type AgentVoiceCommandUtterance struct {
	ID          string  `gorm:"column:id"`
	MeetingID   string  `gorm:"column:meeting_id"`
	CommandID   string  `gorm:"column:command_id"`
	UtteranceID string  `gorm:"column:utterance_id"`
	AgentTurnID *string `gorm:"column:agent_turn_id"`
	Position    int     `gorm:"column:position"`
	State       string  `gorm:"column:state"`
	CreatedAt   int64   `gorm:"column:created_at"`
	UpdatedAt   int64   `gorm:"column:updated_at"`
}

// TableName 返回 AgentVoiceCommandUtterance 的显式数据库表名。
func (AgentVoiceCommandUtterance) TableName() string { return "agent_voice_command_utterances" }

// SyncBatch 映射同步到 Codex 的事件批次。
type SyncBatch struct {
	ID             string  `gorm:"column:id"`
	MeetingID      string  `gorm:"column:meeting_id"`
	AgentSessionID string  `gorm:"column:agent_session_id"`
	FromSeq        int64   `gorm:"column:from_seq"`
	ToSeq          int64   `gorm:"column:to_seq"`
	IdempotencyKey string  `gorm:"column:idempotency_key"`
	State          string  `gorm:"column:state"`
	AttemptCount   int     `gorm:"column:attempt_count"`
	LastErrorCode  *string `gorm:"column:last_error_code"`
	CreatedAt      int64   `gorm:"column:created_at"`
	UpdatedAt      int64   `gorm:"column:updated_at"`
}

// TableName 返回 SyncBatch 的显式数据库表名。
func (SyncBatch) TableName() string { return "sync_batches" }

// ContextSnapshot 映射发送给 Codex 前的上下文快照。
type ContextSnapshot struct {
	ID             string `gorm:"column:id"`
	MeetingID      string `gorm:"column:meeting_id"`
	AgentSessionID string `gorm:"column:agent_session_id"`
	AgentTurnID    string `gorm:"column:agent_turn_id"`
	ThroughSeq     int64  `gorm:"column:through_seq"`
	ContentJSON    string `gorm:"column:content_json"`
	ContentSHA256  string `gorm:"column:content_sha256"`
	CreatedAt      int64  `gorm:"column:created_at"`
	UpdatedAt      int64  `gorm:"column:updated_at"`
}

// TableName 返回 ContextSnapshot 的显式数据库表名。
func (ContextSnapshot) TableName() string { return "context_snapshots" }

// MinuteVersion 映射会议纪要的版本链。
type MinuteVersion struct {
	ID              string  `gorm:"column:id"`
	MeetingID       string  `gorm:"column:meeting_id"`
	AgentTurnID     *string `gorm:"column:agent_turn_id"`
	ParentVersionID *string `gorm:"column:parent_version_id"`
	VersionNo       int     `gorm:"column:version_no"`
	Source          string  `gorm:"column:source"`
	ContentMarkdown string  `gorm:"column:content_markdown"`
	State           string  `gorm:"column:state"`
	IsCurrent       bool    `gorm:"column:is_current"`
	ConfirmedAt     *int64  `gorm:"column:confirmed_at"`
	CreatedAt       int64   `gorm:"column:created_at"`
	UpdatedAt       int64   `gorm:"column:updated_at"`
}

// TableName 返回 MinuteVersion 的显式数据库表名。
func (MinuteVersion) TableName() string { return "minute_versions" }

// DeletionJob 映射可恢复的会议或录音删除任务。
type DeletionJob struct {
	ID                 string  `gorm:"column:id"`
	MeetingID          string  `gorm:"column:meeting_id"`
	Kind               string  `gorm:"column:kind"`
	State              string  `gorm:"column:state"`
	TargetManifestJSON string  `gorm:"column:target_manifest_json"`
	FailedItemsJSON    *string `gorm:"column:failed_items_json"`
	AttemptCount       int     `gorm:"column:attempt_count"`
	LastErrorCode      *string `gorm:"column:last_error_code"`
	CreatedAt          int64   `gorm:"column:created_at"`
	UpdatedAt          int64   `gorm:"column:updated_at"`
}

// TableName 返回 DeletionJob 的显式数据库表名。
func (DeletionJob) TableName() string { return "deletion_jobs" }
