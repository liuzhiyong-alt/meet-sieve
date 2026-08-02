package models

// SpeakerTrack 映射单个 ASR session 匿名标签的说话人处理状态和当前自动决策。
type SpeakerTrack struct {
	ID                     string   `gorm:"column:id"`
	MeetingID              string   `gorm:"column:meeting_id"`
	ASRSessionID           string   `gorm:"column:asr_session_id"`
	ASRSpeakerLabel        string   `gorm:"column:asr_speaker_label"`
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
	Revision               int      `gorm:"column:revision"`
	CreatedAt              int64    `gorm:"column:created_at"`
	UpdatedAt              int64    `gorm:"column:updated_at"`
}

// TableName 返回 SpeakerTrack 的显式数据库表名。
func (SpeakerTrack) TableName() string { return "speaker_tracks" }

// SpeakerTrackEvidence 映射匿名 track 使用或排除的逐条转写证据。
type SpeakerTrackEvidence struct {
	ID             string  `gorm:"column:id"`
	SpeakerTrackID string  `gorm:"column:speaker_track_id"`
	UtteranceID    string  `gorm:"column:utterance_id"`
	EvidenceOrder  int     `gorm:"column:evidence_order"`
	OverlapRisk    bool    `gorm:"column:overlap_risk"`
	Included       bool    `gorm:"column:included"`
	ExcludedReason *string `gorm:"column:excluded_reason"`
	CreatedAt      int64   `gorm:"column:created_at"`
	UpdatedAt      int64   `gorm:"column:updated_at"`
}

// TableName 返回 SpeakerTrackEvidence 的显式数据库表名。
func (SpeakerTrackEvidence) TableName() string { return "speaker_track_evidence" }
