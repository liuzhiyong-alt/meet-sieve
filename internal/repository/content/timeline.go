package content

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// TimelineRow 是桌面统一时间线查询使用的内部投影，不直接暴露给前端。
type TimelineRow struct {
	Seq                      int64
	EventKind                string
	OccurredAt               int64
	Source                   string
	EntityID                 string
	PayloadJSON              string
	UtteranceID              string
	UtteranceText            string
	StartSample              int64
	EndSample                int64
	ParticipantID            string
	ParticipantName          string
	SpeakerClusterID         string
	ASRSessionID             string
	ASRSpeakerLabel          string
	MessageID                string
	MessageAuthorKind        string
	MessageDisplayName       string
	MessageContent           string
	MessageContentFormat     string
	ResourceID               string
	ResourceKind             string
	ResourceState            string
	ResourceName             string
	ResourceMediaType        string
	ResourceSize             int64
	ResourceSHA256           string
	ResourceURL              string
	ResourceDescription      string
	ResourceGuestDisplayName string
	GapID                    string
	GapReason                string
}

// ListTimelineRows 按方向和 seq 游标读取统一事实；返回数量最多为 limit+1 供调用方判断续页。
func (repository *Repository) ListTimelineRows(ctx context.Context, meetingID string, direction string, cursorSeq int64, limit int) ([]TimelineRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || limit <= 0 {
		return nil, fmt.Errorf("读取统一时间线：参数无效")
	}
	operator, order, err := timelineQueryShape(direction, cursorSeq)
	if err != nil {
		return nil, err
	}
	statement := fmt.Sprintf(`SELECT
event.seq, event.kind AS event_kind, event.occurred_at, event.source,
COALESCE(event.entity_id, '') AS entity_id, COALESCE(event.payload_json, '') AS payload_json,
COALESCE(utterance.id, '') AS utterance_id,
COALESCE(utterance.current_text, '') AS utterance_text,
COALESCE(utterance.start_sample, 0) AS start_sample,
COALESCE(utterance.end_sample, 0) AS end_sample,
COALESCE(utterance.current_participant_id, '') AS participant_id,
COALESCE(participant.display_name_snapshot, '') AS participant_name,
COALESCE(utterance.speaker_cluster_id, '') AS speaker_cluster_id,
COALESCE(utterance.asr_session_id, '') AS asr_session_id,
COALESCE(utterance.asr_speaker_label, '') AS asr_speaker_label,
COALESCE(message.id, '') AS message_id,
COALESCE(message.author_kind, '') AS message_author_kind,
COALESCE(message.display_name_snapshot, '') AS message_display_name,
COALESCE(message.content, '') AS message_content,
COALESCE(message.content_format, 'plain') AS message_content_format,
COALESCE(resource.id, '') AS resource_id,
COALESCE(resource.kind, '') AS resource_kind,
COALESCE(resource.state, '') AS resource_state,
COALESCE(resource.original_name, '') AS resource_name,
COALESCE(resource.media_type, '') AS resource_media_type,
COALESCE(resource.size_bytes, 0) AS resource_size,
COALESCE(resource.sha256, '') AS resource_sha256,
COALESCE(resource.source_url, '') AS resource_url,
COALESCE(resource.current_description, '') AS resource_description,
COALESCE(guest.display_name, '') AS resource_guest_display_name,
COALESCE(gap.id, '') AS gap_id,
COALESCE(gap.reason, '') AS gap_reason
FROM meeting_events AS event
LEFT JOIN utterances AS utterance
  ON event.kind = 'utterance.final' AND utterance.event_id = event.id AND utterance.meeting_id = event.meeting_id
LEFT JOIN meeting_participants AS participant
  ON participant.id = utterance.current_participant_id AND participant.meeting_id = event.meeting_id
LEFT JOIN messages AS message
  ON event.kind = 'message.created' AND message.event_id = event.id AND message.meeting_id = event.meeting_id
LEFT JOIN resources AS resource
  ON event.kind = 'resource.created' AND resource.event_id = event.id AND resource.meeting_id = event.meeting_id
LEFT JOIN guest_sessions AS guest
  ON guest.id = resource.guest_session_id AND guest.meeting_id = event.meeting_id
LEFT JOIN asr_gaps AS gap
  ON event.kind = 'asr.gap' AND gap.event_id = event.id AND gap.meeting_id = event.meeting_id
WHERE event.meeting_id = ? %s
ORDER BY event.seq %s
LIMIT ?`, operator, order)
	arguments := []any{meetingID}
	if direction != "latest" {
		arguments = append(arguments, cursorSeq)
	}
	arguments = append(arguments, limit+1)
	var rows []TimelineRow
	if err := repository.reader.WithContext(ctx).Raw(statement, arguments...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取统一时间线失败：%w", err)
	}
	return rows, nil
}

// GetWritableMeeting 在写事务内确认会议仍处于可接收会中消息的录音状态。
func (repository *Repository) GetWritableMeeting(ctx context.Context, tx *gorm.DB, meetingID string) (models.Meeting, error) {
	if tx == nil || meetingID == "" {
		return models.Meeting{}, ErrMeetingNotWritable
	}
	var meeting models.Meeting
	err := tx.WithContext(ctx).Where("id = ?", meetingID).Take(&meeting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || meeting.LifecycleState != "recording" {
		return models.Meeting{}, ErrMeetingNotWritable
	}
	if err != nil {
		return models.Meeting{}, fmt.Errorf("读取可写会议失败：%w", err)
	}
	return meeting, nil
}

// FindExistingHost 在会议范围内读取主持人消息或附件的首次幂等结果。
func (repository *Repository) FindExistingHost(ctx context.Context, tx *gorm.DB, meetingID string, requestID string) (*ExistingContent, error) {
	if tx == nil || meetingID == "" || requestID == "" {
		return nil, fmt.Errorf("查询主持人内容幂等记录：参数无效")
	}
	message, err := findExistingHostMessage(ctx, tx, meetingID, requestID)
	if err != nil || message != nil {
		return message, err
	}
	return findExistingHostResource(ctx, tx, meetingID, requestID)
}

// LatestFinalOccurredAt 返回指定会议最近一条 final 的事件时间；没有 final 时返回 0。
func (repository *Repository) LatestFinalOccurredAt(ctx context.Context, meetingID string) (int64, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return 0, fmt.Errorf("读取最近 final：参数无效")
	}
	var occurredAt int64
	if err := repository.reader.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(occurred_at), 0) FROM meeting_events WHERE meeting_id = ? AND kind = 'utterance.final'",
		meetingID,
	).Scan(&occurredAt).Error; err != nil {
		return 0, fmt.Errorf("读取最近 final 失败：%w", err)
	}
	return occurredAt, nil
}

// LatestEventSeq 返回统一时间线最新持久序号，供不携带 seq 的旧事件源发布失效通知。
func (repository *Repository) LatestEventSeq(ctx context.Context, meetingID string) (int64, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return 0, fmt.Errorf("读取最新事件序号：参数无效")
	}
	var sequence int64
	if err := repository.reader.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(seq), 0) FROM meeting_events WHERE meeting_id = ?", meetingID,
	).Scan(&sequence).Error; err != nil {
		return 0, fmt.Errorf("读取最新事件序号失败：%w", err)
	}
	return sequence, nil
}

// timelineQueryShape 返回固定白名单中的游标条件和排序方向。
func timelineQueryShape(direction string, cursorSeq int64) (string, string, error) {
	switch direction {
	case "latest":
		if cursorSeq != 0 {
			return "", "", fmt.Errorf("latest 查询不接受游标")
		}
		return "", "DESC", nil
	case "before":
		if cursorSeq <= 0 {
			return "", "", fmt.Errorf("before 查询需要正 seq")
		}
		return "AND event.seq < ?", "DESC", nil
	case "after":
		if cursorSeq < 0 {
			return "", "", fmt.Errorf("after 查询需要非负 seq")
		}
		return "AND event.seq > ?", "ASC", nil
	default:
		return "", "", fmt.Errorf("时间线方向无效")
	}
}

// findExistingHostMessage 查询主持人消息的首次提交结果。
func findExistingHostMessage(ctx context.Context, tx *gorm.DB, meetingID string, requestID string) (*ExistingContent, error) {
	var row struct {
		EntityID   string
		Content    string
		Seq        int64
		OccurredAt int64
	}
	result := tx.WithContext(ctx).Table("messages AS message").
		Select("message.id AS entity_id", "message.content", "event.seq", "event.occurred_at").
		Joins("JOIN meeting_events AS event ON event.id = message.event_id").
		Where("message.meeting_id = ? AND message.author_kind = 'host' AND message.request_id = ?", meetingID, requestID).
		Limit(1).Find(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("查询主持人消息幂等记录失败：%w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &ExistingContent{Kind: "text", Content: row.Content, EntityID: row.EntityID, Seq: row.Seq, OccurredAt: row.OccurredAt}, nil
}

// findExistingHostResource 查询主持人附件的首次提交结果。
func findExistingHostResource(ctx context.Context, tx *gorm.DB, meetingID string, requestID string) (*ExistingContent, error) {
	var row struct {
		EntityID     string
		Kind         string
		Seq          int64
		OccurredAt   int64
		OriginalName string
		SizeBytes    int64
		SHA256       string
		MediaType    string
	}
	result := tx.WithContext(ctx).Table("resources AS resource").
		Select("resource.id AS entity_id", "resource.kind", "event.seq", "event.occurred_at",
			"COALESCE(resource.original_name, '') AS original_name", "COALESCE(resource.size_bytes, 0) AS size_bytes",
			"COALESCE(resource.sha256, '') AS sha256", "COALESCE(resource.media_type, '') AS media_type").
		Joins("JOIN meeting_events AS event ON event.id = resource.event_id").
		Where("resource.meeting_id = ? AND resource.guest_session_id IS NULL AND resource.request_id = ?", meetingID, requestID).
		Limit(1).Find(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("查询主持人附件幂等记录失败：%w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &ExistingContent{
		Kind: row.Kind, EntityID: row.EntityID, Seq: row.Seq, OccurredAt: row.OccurredAt,
		OriginalName: row.OriginalName, SizeBytes: row.SizeBytes, SHA256: row.SHA256, MediaType: row.MediaType,
	}, nil
}
