package models

// MeetingEvent 映射统一有序的持久会议事件。
type MeetingEvent struct {
	ID          string  `gorm:"column:id"`
	MeetingID   string  `gorm:"column:meeting_id"`
	Seq         int64   `gorm:"column:seq"`
	Kind        string  `gorm:"column:kind"`
	OccurredAt  int64   `gorm:"column:occurred_at"`
	Source      string  `gorm:"column:source"`
	EntityType  *string `gorm:"column:entity_type"`
	EntityID    *string `gorm:"column:entity_id"`
	PayloadJSON *string `gorm:"column:payload_json"`
	CreatedAt   int64   `gorm:"column:created_at"`
	UpdatedAt   int64   `gorm:"column:updated_at"`
}

// TableName 返回 MeetingEvent 的显式数据库表名。
func (MeetingEvent) TableName() string { return "meeting_events" }

// Utterance 映射最终转写及其当前校正投影。
type Utterance struct {
	ID                      string   `gorm:"column:id"`
	MeetingID               string   `gorm:"column:meeting_id"`
	EventID                 string   `gorm:"column:event_id"`
	ASRSessionID            string   `gorm:"column:asr_session_id"`
	ProviderResultID        string   `gorm:"column:provider_result_id"`
	OriginalText            string   `gorm:"column:original_text"`
	CurrentText             string   `gorm:"column:current_text"`
	StartSample             int64    `gorm:"column:start_sample"`
	EndSample               int64    `gorm:"column:end_sample"`
	ASRSpeakerLabel         *string  `gorm:"column:asr_speaker_label"`
	CurrentParticipantID    *string  `gorm:"column:current_participant_id"`
	SpeakerTrackID          *string  `gorm:"column:speaker_track_id"`
	SpeakerClusterID        *string  `gorm:"column:speaker_cluster_id"`
	SpeakerAssignmentSource string   `gorm:"column:speaker_assignment_source"`
	SpeakerConfidence       *float64 `gorm:"column:speaker_confidence"`
	TextRevision            int      `gorm:"column:text_revision"`
	SpeakerRevision         int      `gorm:"column:speaker_revision"`
	CreatedAt               int64    `gorm:"column:created_at"`
	UpdatedAt               int64    `gorm:"column:updated_at"`
}

// TableName 返回 Utterance 的显式数据库表名。
func (Utterance) TableName() string { return "utterances" }

// GuestSession 映射 LAN 访客会话。
type GuestSession struct {
	ID               string `gorm:"column:id"`
	MeetingID        string `gorm:"column:meeting_id"`
	DisplayName      string `gorm:"column:display_name"`
	SessionTokenHash string `gorm:"column:session_token_hash"`
	State            string `gorm:"column:state"`
	ExpiresAt        int64  `gorm:"column:expires_at"`
	LastSeenAt       *int64 `gorm:"column:last_seen_at"`
	CreatedAt        int64  `gorm:"column:created_at"`
	UpdatedAt        int64  `gorm:"column:updated_at"`
}

// TableName 返回 GuestSession 的显式数据库表名。
func (GuestSession) TableName() string { return "guest_sessions" }

// Message 映射主机或访客创建的消息。
type Message struct {
	ID                  string  `gorm:"column:id"`
	MeetingID           string  `gorm:"column:meeting_id"`
	EventID             string  `gorm:"column:event_id"`
	AuthorKind          string  `gorm:"column:author_kind"`
	MemberID            *string `gorm:"column:member_id"`
	GuestSessionID      *string `gorm:"column:guest_session_id"`
	RequestID           *string `gorm:"column:request_id"`
	DisplayNameSnapshot string  `gorm:"column:display_name_snapshot"`
	Content             string  `gorm:"column:content"`
	CreatedAt           int64   `gorm:"column:created_at"`
	UpdatedAt           int64   `gorm:"column:updated_at"`
}

// TableName 返回 Message 的显式数据库表名。
func (Message) TableName() string { return "messages" }

// Resource 映射链接或工作目录内附件。
type Resource struct {
	ID                  string  `gorm:"column:id"`
	MeetingID           string  `gorm:"column:meeting_id"`
	EventID             string  `gorm:"column:event_id"`
	GuestSessionID      *string `gorm:"column:guest_session_id"`
	RequestID           *string `gorm:"column:request_id"`
	Kind                string  `gorm:"column:kind"`
	OriginalName        *string `gorm:"column:original_name"`
	SafeName            *string `gorm:"column:safe_name"`
	RelativePath        *string `gorm:"column:relative_path"`
	SourceURL           *string `gorm:"column:source_url"`
	MediaType           *string `gorm:"column:media_type"`
	SizeBytes           *int64  `gorm:"column:size_bytes"`
	SHA256              *string `gorm:"column:sha256"`
	OriginalDescription *string `gorm:"column:original_description"`
	CurrentDescription  *string `gorm:"column:current_description"`
	DescriptionRevision int     `gorm:"column:description_revision"`
	State               string  `gorm:"column:state"`
	CreatedAt           int64   `gorm:"column:created_at"`
	UpdatedAt           int64   `gorm:"column:updated_at"`
}

// TableName 返回 Resource 的显式数据库表名。
func (Resource) TableName() string { return "resources" }

// Correction 映射对事件实体的不可变校正记录。
type Correction struct {
	ID             string  `gorm:"column:id"`
	MeetingID      string  `gorm:"column:meeting_id"`
	EventID        string  `gorm:"column:event_id"`
	RequestID      string  `gorm:"column:request_id"`
	TargetKind     string  `gorm:"column:target_kind"`
	TargetID       string  `gorm:"column:target_id"`
	CorrectionKind string  `gorm:"column:correction_kind"`
	BeforeJSON     string  `gorm:"column:before_json"`
	AfterJSON      string  `gorm:"column:after_json"`
	OperatorKind   string  `gorm:"column:operator_kind"`
	OperatorID     *string `gorm:"column:operator_id"`
	Reason         *string `gorm:"column:reason"`
	TargetRevision int     `gorm:"column:target_revision"`
	ResultRevision int     `gorm:"column:result_revision"`
	BatchScope     string  `gorm:"column:batch_scope"`
	CreatedAt      int64   `gorm:"column:created_at"`
	UpdatedAt      int64   `gorm:"column:updated_at"`
}

// TableName 返回 Correction 的显式数据库表名。
func (Correction) TableName() string { return "corrections" }

// CorrectionItem 映射一次批量校对中实际受影响的单条实体审计。
type CorrectionItem struct {
	ID           string `gorm:"column:id"`
	CorrectionID string `gorm:"column:correction_id"`
	TargetKind   string `gorm:"column:target_kind"`
	TargetID     string `gorm:"column:target_id"`
	BeforeJSON   string `gorm:"column:before_json"`
	AfterJSON    string `gorm:"column:after_json"`
	ItemOrder    int    `gorm:"column:item_order"`
	CreatedAt    int64  `gorm:"column:created_at"`
	UpdatedAt    int64  `gorm:"column:updated_at"`
}

// TableName 返回 CorrectionItem 的显式数据库表名。
func (CorrectionItem) TableName() string { return "correction_items" }
