package transcript

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// Repository 负责 transcript 持久化，不决定事件幂等和事务边界。
type Repository struct {
	reader *gorm.DB
}

// RawRecordRow 是原始记录投影所需的最小 SQLite 读取行，不包含内部 ID 或敏感数据。
type RawRecordRow struct {
	Seq                    int64
	Kind                   string
	OccurredAt             int64
	StartSample            int64
	EndSample              int64
	CurrentText            string
	ASRSessionID           string
	ParticipantDisplayName string
	ClusterDisplayNo       int
	TrackDisplayNo         int
	GuestDisplayName       string
	SourceURL              string
	OriginalName           string
	MediaType              string
	SizeBytes              int64
	SHA256                 string
	Description            string
	AgentText              string
}

// TimelineRow 是 Wails Timeline 服务所需的 final/gap 联合读取行。
type TimelineRow struct {
	Seq          int64
	Kind         string
	OccurredAt   int64
	StartSample  int64
	EndSample    int64
	Text         string
	SpeakerLabel string
	ASRSessionID string
	GapReason    string
}

// NewRepository 创建 transcript Repository，reader 仅供事务外读取。
func NewRepository(reader *gorm.DB) *Repository {
	return &Repository{reader: reader}
}

// GetMeetingForEvent 返回仍允许接收 final/gap 的会议事实。
func (repository *Repository) GetMeetingForEvent(ctx context.Context, tx *gorm.DB, meetingID string) (models.Meeting, error) {
	if tx == nil || meetingID == "" {
		return models.Meeting{}, fmt.Errorf("读取事件会议：参数无效")
	}
	var meeting models.Meeting
	err := tx.WithContext(ctx).Select(meetingColumns()).Where("id = ?", meetingID).Take(&meeting).Error
	if err != nil {
		return models.Meeting{}, fmt.Errorf("读取事件会议失败：%w", err)
	}
	return meeting, nil
}

// FindUtteranceByProviderResult 在同一事务内按 provider 幂等键查询 final。
func (repository *Repository) FindUtteranceByProviderResult(ctx context.Context, tx *gorm.DB, sessionID string, resultID string) (*models.Utterance, error) {
	if tx == nil || sessionID == "" || resultID == "" {
		return nil, fmt.Errorf("查询转写幂等记录：参数无效")
	}
	var utterance models.Utterance
	err := tx.WithContext(ctx).Select(utteranceColumns()).Where("asr_session_id = ? AND provider_result_id = ?", sessionID, resultID).Take(&utterance).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询转写幂等记录失败：%w", err)
	}
	return &utterance, nil
}

// FindGapByOriginKey 在同一事务内按 gap 幂等键查询已写事实。
func (repository *Repository) FindGapByOriginKey(ctx context.Context, tx *gorm.DB, originKey string) (*models.ASRGap, error) {
	if tx == nil || originKey == "" {
		return nil, fmt.Errorf("查询 gap 幂等记录：参数无效")
	}
	var gap models.ASRGap
	err := tx.WithContext(ctx).Select(gapColumns()).Where("origin_key = ?", originKey).Take(&gap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 gap 幂等记录失败：%w", err)
	}
	return &gap, nil
}

// FindEventByID 返回已持久化事件，用于幂等调用复用原有 seq。
func (repository *Repository) FindEventByID(ctx context.Context, tx *gorm.DB, eventID string) (*models.MeetingEvent, error) {
	if tx == nil || eventID == "" {
		return nil, fmt.Errorf("查询事件：参数无效")
	}
	var event models.MeetingEvent
	err := tx.WithContext(ctx).Select(eventColumns()).Where("id = ?", eventID).Take(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询事件失败：%w", err)
	}
	return &event, nil
}

// GetSessionForEvent 返回属于会议的 ASR session，避免跨会议写入 final 进度。
func (repository *Repository) GetSessionForEvent(ctx context.Context, tx *gorm.DB, meetingID string, sessionID string) (models.ASRSession, error) {
	if tx == nil || meetingID == "" || sessionID == "" {
		return models.ASRSession{}, fmt.Errorf("读取 ASR session：参数无效")
	}
	var session models.ASRSession
	err := tx.WithContext(ctx).Where("id = ? AND meeting_id = ?", sessionID, meetingID).Take(&session).Error
	if err != nil {
		return models.ASRSession{}, fmt.Errorf("读取 ASR session 失败：%w", err)
	}
	return session, nil
}

// NextEventSeq 在单 writer 事务中分配下一条持久事件序号。
func (repository *Repository) NextEventSeq(ctx context.Context, tx *gorm.DB, meetingID string) (int64, error) {
	if tx == nil || meetingID == "" {
		return 0, fmt.Errorf("分配事件序号：参数无效")
	}
	var next int64
	if err := tx.WithContext(ctx).Raw("SELECT COALESCE(MAX(seq), 0) + 1 FROM meeting_events WHERE meeting_id = ?", meetingID).Scan(&next).Error; err != nil {
		return 0, fmt.Errorf("分配事件序号失败：%w", err)
	}
	return next, nil
}

// SumDiscardedSamplesBefore 汇总逻辑样本边界之前已经完成的媒体暂停样本。
func (repository *Repository) SumDiscardedSamplesBefore(ctx context.Context, tx *gorm.DB, meetingID string, logicalSample int64) (int64, error) {
	if tx == nil || meetingID == "" || logicalSample < 0 {
		return 0, fmt.Errorf("读取媒体暂停样本：参数无效")
	}
	var discarded int64
	if err := tx.WithContext(ctx).Raw(`SELECT COALESCE(SUM(discarded_samples), 0)
FROM meeting_media_pauses
WHERE meeting_id = ? AND state = 'completed' AND logical_sample <= ?`, meetingID, logicalSample).Scan(&discarded).Error; err != nil {
		return 0, fmt.Errorf("读取媒体暂停样本失败：%w", err)
	}
	return discarded, nil
}

// CreateFinal 在调用方事务中同时插入 event header 和 final utterance。
func (repository *Repository) CreateFinal(ctx context.Context, tx *gorm.DB, event models.MeetingEvent, utterance models.Utterance) error {
	if tx == nil {
		return fmt.Errorf("创建 final：事务不能为空")
	}
	if err := tx.WithContext(ctx).Create(&event).Error; err != nil {
		return fmt.Errorf("写入 final 事件失败：%w", err)
	}
	if err := tx.WithContext(ctx).Create(&utterance).Error; err != nil {
		return fmt.Errorf("写入 final 发言失败：%w", err)
	}
	return nil
}

// CreateVoiceCommandCandidate 在 final 事务中写入语音指令候选用途关系。
func (repository *Repository) CreateVoiceCommandCandidate(ctx context.Context, tx *gorm.DB, relation models.AgentVoiceCommandUtterance) error {
	if tx == nil || relation.ID == "" || relation.CommandID == "" || relation.UtteranceID == "" {
		return fmt.Errorf("创建语音指令候选：参数无效")
	}
	if err := tx.WithContext(ctx).Create(&relation).Error; err != nil {
		return fmt.Errorf("创建语音指令候选失败：%w", err)
	}
	return nil
}

// CreateGap 在调用方事务中同时插入 event header 和 gap。
func (repository *Repository) CreateGap(ctx context.Context, tx *gorm.DB, event models.MeetingEvent, gap models.ASRGap) error {
	if tx == nil {
		return fmt.Errorf("创建 gap：事务不能为空")
	}
	if err := tx.WithContext(ctx).Create(&event).Error; err != nil {
		return fmt.Errorf("写入 gap 事件失败：%w", err)
	}
	if err := tx.WithContext(ctx).Create(&gap).Error; err != nil {
		return fmt.Errorf("写入 gap 记录失败：%w", err)
	}
	if err := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ?", gap.MeetingID).
		Updates(map[string]any{"gap_state": "pending", "updated_at": gap.UpdatedAt}).Error; err != nil {
		return fmt.Errorf("更新会议 gap 状态失败：%w", err)
	}
	return nil
}

// UpdateLastFinalSample 只向前推进已安全持久化的 final 样本边界。
func (repository *Repository) UpdateLastFinalSample(ctx context.Context, tx *gorm.DB, meetingID string, sessionID string, endSample int64, updatedAt int64) error {
	if tx == nil || meetingID == "" || sessionID == "" {
		return fmt.Errorf("更新 final 样本进度：参数无效")
	}
	result := tx.WithContext(ctx).Model(&models.ASRSession{}).Where("id = ? AND meeting_id = ?", sessionID, meetingID).
		Where("last_final_sample <= ? AND ? <= last_sent_sample", endSample, endSample).
		Updates(map[string]any{"last_final_sample": endSample, "updated_at": updatedAt})
	if result.Error != nil {
		return fmt.Errorf("更新 final 样本进度失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("final 样本进度不在 session 已发送范围内")
	}
	return nil
}

// ListTimeline 返回 seq 游标后的当前 Step 4 持久事件。
func (repository *Repository) ListTimeline(ctx context.Context, meetingID string, afterSeq int64, limit int) ([]models.MeetingEvent, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || afterSeq < 0 {
		return nil, fmt.Errorf("读取事件时间线：参数无效")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	var events []models.MeetingEvent
	err := repository.reader.WithContext(ctx).Select(eventColumns()).
		Where("meeting_id = ? AND seq > ? AND kind IN ?", meetingID, afterSeq, []string{"utterance.final", "asr.gap"}).
		Order("seq ASC").Limit(limit).Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("读取事件时间线失败：%w", err)
	}
	return events, nil
}

// ListTimelineRows 按 seq 游标返回 final/gap 判别联合所需的当前投影。
func (repository *Repository) ListTimelineRows(ctx context.Context, meetingID string, afterSeq int64, limit int) ([]TimelineRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || afterSeq < 0 {
		return nil, fmt.Errorf("读取转写 Timeline：参数无效")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	statement := `SELECT event.seq, event.kind, event.occurred_at,
COALESCE(utterance.start_sample, gap.start_sample, 0) AS start_sample,
COALESCE(utterance.end_sample, gap.end_sample, 0) AS end_sample,
COALESCE(utterance.current_text, '') AS text,
COALESCE(utterance.asr_speaker_label, '') AS speaker_label,
COALESCE(utterance.asr_session_id, gap.asr_session_id, '') AS asr_session_id,
COALESCE(gap.reason, '') AS gap_reason
FROM meeting_events AS event
LEFT JOIN utterances AS utterance ON utterance.event_id = event.id
LEFT JOIN agent_voice_command_utterances AS voice_command ON voice_command.utterance_id = utterance.id
LEFT JOIN asr_gaps AS gap ON gap.event_id = event.id
WHERE event.meeting_id = ? AND event.seq > ? AND event.kind IN ('utterance.final', 'asr.gap')
  AND (voice_command.id IS NULL OR voice_command.state = 'released')
ORDER BY event.seq ASC LIMIT ?`
	var rows []TimelineRow
	if err := repository.reader.WithContext(ctx).Raw(statement, meetingID, afterSeq, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取转写 Timeline 失败：%w", err)
	}
	return rows, nil
}

// LoadRawRecordRows 按持久事件 seq 读取原始记录所需的转写、消息和已完成资源事实。
func (repository *Repository) LoadRawRecordRows(ctx context.Context, meetingID string) ([]RawRecordRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取原始记录事件：参数无效")
	}
	const statement = `
SELECT event.seq, event.kind, event.occurred_at,
       COALESCE(utterance.start_sample, gap.start_sample, 0) AS start_sample,
       COALESCE(utterance.end_sample, gap.end_sample, 0) AS end_sample,
       COALESCE(utterance.current_text, message.content, '') AS current_text,
       COALESCE(utterance.asr_session_id, '') AS asr_session_id,
       COALESCE(participant.display_name_snapshot, '') AS participant_display_name,
       COALESCE(cluster.display_no, 0) AS cluster_display_no,
	   COALESCE(track.display_no, 0) AS track_display_no,
       COALESCE(message.display_name_snapshot, guest.display_name, '') AS guest_display_name,
       COALESCE(resource.source_url, '') AS source_url,
       COALESCE(resource.original_name, '') AS original_name,
       COALESCE(resource.media_type, '') AS media_type,
       COALESCE(resource.size_bytes, 0) AS size_bytes,
       COALESCE(resource.sha256, '') AS sha256,
       COALESCE(resource.current_description, '') AS description,
       CASE WHEN event.kind IN ('ai.question', 'ai.answer')
            THEN COALESCE(json_extract(event.payload_json, '$.text'), '') ELSE '' END AS agent_text
FROM meeting_events AS event
LEFT JOIN utterances AS utterance ON utterance.event_id = event.id AND utterance.meeting_id = event.meeting_id
LEFT JOIN agent_voice_command_utterances AS voice_command ON voice_command.utterance_id = utterance.id
LEFT JOIN asr_gaps AS gap ON gap.event_id = event.id AND gap.meeting_id = event.meeting_id
LEFT JOIN meeting_participants AS participant ON participant.id = utterance.current_participant_id
LEFT JOIN speaker_clusters AS cluster ON cluster.id = utterance.speaker_cluster_id
LEFT JOIN speaker_tracks AS track ON track.id = utterance.speaker_track_id
LEFT JOIN messages AS message ON message.event_id = event.id AND message.meeting_id = event.meeting_id
LEFT JOIN resources AS resource ON resource.event_id = event.id AND resource.meeting_id = event.meeting_id AND resource.state = 'completed'
LEFT JOIN guest_sessions AS guest ON guest.id = resource.guest_session_id AND guest.meeting_id = event.meeting_id
WHERE event.meeting_id = ?
  AND event.kind IN ('utterance.final', 'asr.gap', 'message.created', 'resource.created', 'ai.question', 'ai.answer', 'ai.cancelled', 'ai.failed')
  AND (voice_command.id IS NULL OR voice_command.state = 'released')
  AND (event.kind <> 'resource.created' OR resource.id IS NOT NULL)
ORDER BY event.seq ASC`
	var rows []RawRecordRow
	if err := repository.reader.WithContext(ctx).Raw(statement, meetingID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取原始记录事件失败：%w", err)
	}
	return rows, nil
}

// HasCorrections 返回会议是否存在至少一条已提交人工校对，用于 Markdown 统一说明。
func (repository *Repository) HasCorrections(ctx context.Context, meetingID string) (bool, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return false, fmt.Errorf("读取原始记录 correction 摘要：参数无效")
	}
	var count int64
	if err := repository.reader.WithContext(ctx).Model(&models.Correction{}).
		Where("meeting_id = ?", meetingID).Limit(1).Count(&count).Error; err != nil {
		return false, fmt.Errorf("读取原始记录 correction 摘要失败：%w", err)
	}
	return count > 0, nil
}

// LoadSessions 返回会议 ASR session 的开始顺序，用于稳定显示匿名 Session 编号。
func (repository *Repository) LoadSessions(ctx context.Context, meetingID string) ([]models.ASRSession, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取 ASR sessions：参数无效")
	}
	var sessions []models.ASRSession
	err := repository.reader.WithContext(ctx).
		Select("id", "meeting_id", "provider", "provider_session_id", "state", "started_at", "ended_at", "reconnect_count", "transport_mode", "input_start_sample", "last_sent_sample", "last_final_sample", "last_error_code", "created_at", "updated_at").
		Where("meeting_id = ?", meetingID).Order("started_at ASC").Order("id ASC").Find(&sessions).Error
	if err != nil {
		return nil, fmt.Errorf("读取 ASR sessions 失败：%w", err)
	}
	return sessions, nil
}

// GetMeetingSnapshot 返回原始记录投影所需的当前会议事实。
func (repository *Repository) GetMeetingSnapshot(ctx context.Context, meetingID string) (models.Meeting, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.Meeting{}, fmt.Errorf("读取转写会议快照：参数无效")
	}
	var meeting models.Meeting
	if err := repository.reader.WithContext(ctx).Select(meetingColumns()).Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		return models.Meeting{}, fmt.Errorf("读取转写会议快照失败：%w", err)
	}
	return meeting, nil
}

func meetingColumns() []string {
	return []string{"id", "meeting_no", "subject", "relative_dir", "local_timezone", "started_at", "ended_at", "lifecycle_state", "local_save_state", "realtime_asr_state", "gap_state", "agent_state", "minute_state", "lan_state", "created_at", "updated_at"}
}

func eventColumns() []string {
	return []string{"id", "meeting_id", "seq", "kind", "occurred_at", "source", "entity_type", "entity_id", "payload_json", "created_at", "updated_at"}
}

func utteranceColumns() []string {
	return []string{
		"id", "meeting_id", "event_id", "asr_session_id", "provider_result_id", "original_text", "current_text",
		"start_sample", "end_sample", "asr_speaker_label", "current_participant_id", "speaker_track_id",
		"speaker_cluster_id", "speaker_assignment_source", "speaker_confidence", "text_revision", "speaker_revision",
		"created_at", "updated_at",
	}
}

func gapColumns() []string {
	return []string{"id", "meeting_id", "event_id", "asr_session_id", "audio_asset_id", "start_sample", "end_sample", "reason", "origin_key", "state", "attempt_count", "result_from_seq", "result_to_seq", "conflict_json", "last_error_code", "created_at", "updated_at"}
}
