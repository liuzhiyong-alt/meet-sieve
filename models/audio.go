package models

// AudioAsset 映射会议音频文件的完整性信息。
type AudioAsset struct {
	ID           string `gorm:"column:id"`
	MeetingID    string `gorm:"column:meeting_id"`
	Kind         string `gorm:"column:kind"`
	SequenceNo   int    `gorm:"column:sequence_no"`
	RelativePath string `gorm:"column:relative_path"`
	StartSample  int64  `gorm:"column:start_sample"`
	EndSample    int64  `gorm:"column:end_sample"`
	SampleRate   int64  `gorm:"column:sample_rate"`
	BitDepth     int64  `gorm:"column:bit_depth"`
	Channels     int64  `gorm:"column:channels"`
	SizeBytes    int64  `gorm:"column:size_bytes"`
	SHA256       string `gorm:"column:sha256"`
	State        string `gorm:"column:state"`
	CreatedAt    int64  `gorm:"column:created_at"`
	UpdatedAt    int64  `gorm:"column:updated_at"`
}

// TableName 返回 AudioAsset 的显式数据库表名。
func (AudioAsset) TableName() string { return "audio_assets" }

// ASRSession 映射火山实时转写连接会话。
type ASRSession struct {
	ID                string  `gorm:"column:id"`
	MeetingID         string  `gorm:"column:meeting_id"`
	Provider          string  `gorm:"column:provider"`
	ProviderSessionID *string `gorm:"column:provider_session_id"`
	State             string  `gorm:"column:state"`
	StartedAt         int64   `gorm:"column:started_at"`
	EndedAt           *int64  `gorm:"column:ended_at"`
	ReconnectCount    int     `gorm:"column:reconnect_count"`
	TransportMode     string  `gorm:"column:transport_mode"`
	InputStartSample  int64   `gorm:"column:input_start_sample"`
	LastSentSample    int64   `gorm:"column:last_sent_sample"`
	LastFinalSample   int64   `gorm:"column:last_final_sample"`
	LastErrorCode     *string `gorm:"column:last_error_code"`
	CreatedAt         int64   `gorm:"column:created_at"`
	UpdatedAt         int64   `gorm:"column:updated_at"`
}

// TableName 返回 ASRSession 的显式数据库表名。
func (ASRSession) TableName() string { return "asr_sessions" }

// MeetingMediaPause 映射语音 AI turn 独占的媒体暂停边界与丢弃样本。
type MeetingMediaPause struct {
	ID                  string  `gorm:"column:id"`
	MeetingID           string  `gorm:"column:meeting_id"`
	AgentTurnID         string  `gorm:"column:agent_turn_id"`
	Reason              string  `gorm:"column:reason"`
	State               string  `gorm:"column:state"`
	LogicalSample       *int64  `gorm:"column:logical_sample"`
	PhysicalStartSample *int64  `gorm:"column:physical_start_sample"`
	PhysicalEndSample   *int64  `gorm:"column:physical_end_sample"`
	DiscardedSamples    int64   `gorm:"column:discarded_samples"`
	StartedAt           int64   `gorm:"column:started_at"`
	EndedAt             *int64  `gorm:"column:ended_at"`
	LastErrorCode       *string `gorm:"column:last_error_code"`
	CreatedAt           int64   `gorm:"column:created_at"`
	UpdatedAt           int64   `gorm:"column:updated_at"`
}

// TableName 返回 MeetingMediaPause 的显式数据库表名。
func (MeetingMediaPause) TableName() string { return "meeting_media_pauses" }

// ASRGap 映射实时转写过程中的音频缺口。
type ASRGap struct {
	ID            string  `gorm:"column:id"`
	MeetingID     string  `gorm:"column:meeting_id"`
	EventID       string  `gorm:"column:event_id"`
	ASRSessionID  *string `gorm:"column:asr_session_id"`
	AudioAssetID  *string `gorm:"column:audio_asset_id"`
	StartSample   int64   `gorm:"column:start_sample"`
	EndSample     int64   `gorm:"column:end_sample"`
	Reason        string  `gorm:"column:reason"`
	OriginKey     string  `gorm:"column:origin_key"`
	State         string  `gorm:"column:state"`
	AttemptCount  int     `gorm:"column:attempt_count"`
	ResultFromSeq *int64  `gorm:"column:result_from_seq"`
	ResultToSeq   *int64  `gorm:"column:result_to_seq"`
	ConflictJSON  *string `gorm:"column:conflict_json"`
	LastErrorCode *string `gorm:"column:last_error_code"`
	CreatedAt     int64   `gorm:"column:created_at"`
	UpdatedAt     int64   `gorm:"column:updated_at"`
}

// TableName 返回 ASRGap 的显式数据库表名。
func (ASRGap) TableName() string { return "asr_gaps" }

// GapTranscriptionAttempt 映射一次可审计的会后缺口补转写请求。
type GapTranscriptionAttempt struct {
	ID                  string  `gorm:"column:id"`
	MeetingID           string  `gorm:"column:meeting_id"`
	AudioAssetID        string  `gorm:"column:audio_asset_id"`
	Provider            string  `gorm:"column:provider"`
	ProviderRequestID   string  `gorm:"column:provider_request_id"`
	CoreStartSample     int64   `gorm:"column:core_start_sample"`
	CoreEndSample       int64   `gorm:"column:core_end_sample"`
	AudioStartSample    int64   `gorm:"column:audio_start_sample"`
	AudioEndSample      int64   `gorm:"column:audio_end_sample"`
	State               string  `gorm:"column:state"`
	AttemptNo           int     `gorm:"column:attempt_no"`
	RequestSHA256       string  `gorm:"column:request_sha256"`
	ResponseJSON        *string `gorm:"column:response_json"`
	ProviderLogIDSuffix *string `gorm:"column:provider_log_id_suffix"`
	LastErrorCode       *string `gorm:"column:last_error_code"`
	StartedAt           *int64  `gorm:"column:started_at"`
	EndedAt             *int64  `gorm:"column:ended_at"`
	CreatedAt           int64   `gorm:"column:created_at"`
	UpdatedAt           int64   `gorm:"column:updated_at"`
}

// TableName 返回 GapTranscriptionAttempt 的显式数据库表名。
func (GapTranscriptionAttempt) TableName() string { return "gap_transcription_attempts" }

// GapTranscriptionAttemptItem 映射一次补转写尝试包含的有序 gap。
type GapTranscriptionAttemptItem struct {
	AttemptID string `gorm:"column:attempt_id"`
	GapID     string `gorm:"column:gap_id"`
	ItemOrder int    `gorm:"column:item_order"`
	CreatedAt int64  `gorm:"column:created_at"`
}

// TableName 返回 GapTranscriptionAttemptItem 的显式数据库表名。
func (GapTranscriptionAttemptItem) TableName() string { return "gap_transcription_attempt_items" }

// VoiceSample 映射成员的原始声纹样本。
type VoiceSample struct {
	ID                 string  `gorm:"column:id"`
	MemberID           string  `gorm:"column:member_id"`
	RelativePath       string  `gorm:"column:relative_path"`
	DurationMS         int64   `gorm:"column:duration_ms"`
	SampleRate         int64   `gorm:"column:sample_rate"`
	Channels           int64   `gorm:"column:channels"`
	BitDepth           int64   `gorm:"column:bit_depth"`
	SizeBytes          int64   `gorm:"column:size_bytes"`
	SHA256             string  `gorm:"column:sha256"`
	SourceKind         string  `gorm:"column:source_kind"`
	SourceName         *string `gorm:"column:source_name"`
	RequestID          *string `gorm:"column:request_id"`
	SourceMeetingID    *string `gorm:"column:source_meeting_id"`
	SourceUtteranceID  *string `gorm:"column:source_utterance_id"`
	EnvironmentKind    string  `gorm:"column:environment_kind"`
	ProcessingState    string  `gorm:"column:processing_state"`
	QualityState       string  `gorm:"column:quality_state"`
	QualityCode        *string `gorm:"column:quality_code"`
	QualityMetricsJSON *string `gorm:"column:quality_metrics_json"`
	LastErrorCode      *string `gorm:"column:last_error_code"`
	CreatedAt          int64   `gorm:"column:created_at"`
	UpdatedAt          int64   `gorm:"column:updated_at"`
}

// TableName 返回 VoiceSample 的显式数据库表名。
func (VoiceSample) TableName() string { return "voice_samples" }

// VoiceEmbedding 映射声纹样本的模型输出。
type VoiceEmbedding struct {
	ID            string `gorm:"column:id"`
	VoiceSampleID string `gorm:"column:voice_sample_id"`
	ModelID       string `gorm:"column:model_id"`
	ModelVersion  string `gorm:"column:model_version"`
	ModelSHA256   string `gorm:"column:model_sha256"`
	Dimension     int    `gorm:"column:dimension"`
	Embedding     []byte `gorm:"column:embedding"`
	CreatedAt     int64  `gorm:"column:created_at"`
	UpdatedAt     int64  `gorm:"column:updated_at"`
}

// TableName 返回 VoiceEmbedding 的显式数据库表名。
func (VoiceEmbedding) TableName() string { return "voice_embeddings" }

// SpeakerCluster 映射一场会议内稳定编号的未知说话人聚类。
type SpeakerCluster struct {
	ID                    string   `gorm:"column:id"`
	MeetingID             string   `gorm:"column:meeting_id"`
	DisplayNo             int      `gorm:"column:display_no"`
	AssignedParticipantID *string  `gorm:"column:assigned_participant_id"`
	AssignmentSource      string   `gorm:"column:assignment_source"`
	Centroid              []byte   `gorm:"column:centroid"`
	ModelID               *string  `gorm:"column:model_id"`
	ModelVersion          *string  `gorm:"column:model_version"`
	ModelSHA256           *string  `gorm:"column:model_sha256"`
	Dimension             *int     `gorm:"column:dimension"`
	ProfileID             *string  `gorm:"column:profile_id"`
	TrackCount            int      `gorm:"column:track_count"`
	Confidence            *float64 `gorm:"column:confidence"`
	Revision              int      `gorm:"column:revision"`
	CreatedAt             int64    `gorm:"column:created_at"`
	UpdatedAt             int64    `gorm:"column:updated_at"`
}

// TableName 返回 SpeakerCluster 的显式数据库表名。
func (SpeakerCluster) TableName() string { return "speaker_clusters" }
