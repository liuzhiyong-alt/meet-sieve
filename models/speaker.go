package models

// SpeakerTrack 映射 provider 标签或本地 utterance 证据的说话人处理状态和当前自动决策。
type SpeakerTrack struct {
	ID           string `gorm:"column:id"`
	MeetingID    string `gorm:"column:meeting_id"`
	ASRSessionID string `gorm:"column:asr_session_id"`
	// Source 区分 provider 返回标签与无标签 final 的本地音频证据，不能混用两种幂等键。
	Source                 string   `gorm:"column:source"`
	ASRSpeakerLabel        *string  `gorm:"column:asr_speaker_label"`
	ProviderSegmentNo      *int     `gorm:"column:provider_segment_no"`
	SourceUtteranceID      *string  `gorm:"column:source_utterance_id"`
	DisplayNo              int      `gorm:"column:display_no"`
	State                  string   `gorm:"column:state"`
	AutomaticParticipantID *string  `gorm:"column:automatic_participant_id"`
	SpeakerClusterID       *string  `gorm:"column:speaker_cluster_id"`
	TopScore               *float64 `gorm:"column:top_score"`
	RunnerUpScore          *float64 `gorm:"column:runner_up_score"`
	EvidenceDurationMS     int64    `gorm:"column:evidence_duration_ms"`
	ModelID                *string  `gorm:"column:model_id"`
	ModelVersion           *string  `gorm:"column:model_version"`
	ModelSHA256            *string  `gorm:"column:model_sha256"`
	Dimension              *int     `gorm:"column:dimension"`
	Embedding              []byte   `gorm:"column:embedding"`
	ProfileID              *string  `gorm:"column:profile_id"`
	LastErrorCode          *string  `gorm:"column:last_error_code"`
	RoutingRevision        int      `gorm:"column:routing_revision"`
	Revision               int      `gorm:"column:revision"`
	CreatedAt              int64    `gorm:"column:created_at"`
	UpdatedAt              int64    `gorm:"column:updated_at"`
}

// TableName 返回 SpeakerTrack 的显式数据库表名。
func (SpeakerTrack) TableName() string { return "speaker_tracks" }

// SpeakerTrackEvidence 映射匿名 track 使用或排除的逐条转写证据。
type SpeakerTrackEvidence struct {
	ID                  string   `gorm:"column:id"`
	SpeakerTrackID      string   `gorm:"column:speaker_track_id"`
	UtteranceID         string   `gorm:"column:utterance_id"`
	EvidenceOrder       int      `gorm:"column:evidence_order"`
	OverlapRisk         bool     `gorm:"column:overlap_risk"`
	Included            bool     `gorm:"column:included"`
	ExcludedReason      *string  `gorm:"column:excluded_reason"`
	RoutingState        string   `gorm:"column:routing_state"`
	RoutingErrorCode    *string  `gorm:"column:routing_error_code"`
	RoutingDurationMS   int64    `gorm:"column:routing_duration_ms"`
	RoutingScore        *float64 `gorm:"column:routing_score"`
	RoutingMargin       *float64 `gorm:"column:routing_margin"`
	RoutingModelID      *string  `gorm:"column:routing_model_id"`
	RoutingModelVersion *string  `gorm:"column:routing_model_version"`
	RoutingModelSHA256  *string  `gorm:"column:routing_model_sha256"`
	RoutingDimension    *int     `gorm:"column:routing_dimension"`
	RoutingProfileID    *string  `gorm:"column:routing_profile_id"`
	RoutingEmbedding    []byte   `gorm:"column:routing_embedding"`
	CreatedAt           int64    `gorm:"column:created_at"`
	UpdatedAt           int64    `gorm:"column:updated_at"`
}

// TableName 返回 SpeakerTrackEvidence 的显式数据库表名。
func (SpeakerTrackEvidence) TableName() string { return "speaker_track_evidence" }
