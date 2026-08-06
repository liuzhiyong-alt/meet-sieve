package query

import (
	"context"
	"errors"
	"fmt"
	"slices"

	querydomain "meet-sieve/internal/domain/query"

	"gorm.io/gorm"
)

// TranscriptRow 是原始记录页面使用的单条 SQLite 事实投影。
type TranscriptRow struct {
	Seq              int64
	Kind             string
	OccurredAt       int64
	Text             string
	SpeakerName      string
	SpeakerDisplay   string
	ClusterDisplayNo int
	TrackDisplayNo   int
	StartSample      int64
	EndSample        int64
}

// SeqPageState 明确表达当前事件页两侧是否存在相邻页。
type SeqPageState struct {
	HasPrevious bool
	HasNext     bool
}

// ContentRow 是消息、附件、链接和公开 AI 回答的安全内部投影。
type ContentRow struct {
	Seq           int64
	Kind          string
	OccurredAt    int64
	EntityID      string
	DisplayName   string
	Text          string
	ResourceKind  string
	ResourceName  string
	ResourceState string
	SourceURL     string
}

// GetMeeting 按 ID 读取完整正交摘要，不把文件路径带入 read model。
func (repository *Repository) GetMeeting(ctx context.Context, meetingID string) (*MeetingSummaryRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取会议详情：参数无效")
	}
	query := repository.reader.WithContext(ctx).Table("meetings AS meeting").Select(
		"meeting.id", "meeting.meeting_no", "meeting.subject", "meeting.started_at", "meeting.ended_at",
		"meeting.lifecycle_state", "meeting.local_save_state", "meeting.realtime_asr_state", "meeting.gap_state",
		"meeting.agent_state", "meeting.minute_state", "meeting.lan_state",
		"EXISTS (SELECT 1 FROM audio_assets aa WHERE aa.meeting_id = meeting.id AND aa.state = 'ready') AS has_ready_audio",
		"EXISTS (SELECT 1 FROM deletion_jobs dj WHERE dj.meeting_id = meeting.id AND dj.kind = 'recording' AND dj.state = 'completed') AS recording_deleted",
	).Where("meeting.id = ?", meetingID)
	var row MeetingSummaryRow
	if err := query.Take(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("读取会议详情失败：%w", err)
	}
	rows := []MeetingSummaryRow{row}
	if err := repository.loadPageFacts(ctx, rows); err != nil {
		return nil, err
	}
	return &rows[0], nil
}

// CountStatus 只统计首页最高优先级继续处理分类，不用于会议记录总页数。
func (repository *Repository) CountStatus(ctx context.Context, status querydomain.MeetingStatus) (int, error) {
	if repository == nil || repository.reader == nil {
		return 0, fmt.Errorf("统计继续处理会议：Repository 不可用")
	}
	query := buildMeetingListQuery(repository.reader.WithContext(ctx), querydomain.MeetingFilter{}, nil)
	query, err := applyStatusFilter(query, string(status))
	if err != nil {
		return 0, err
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("统计继续处理会议失败：%w", err)
	}
	return int(count), nil
}

// ListTranscript 按事件 seq 读取原始记录，after 与 before 只能使用一个。
func (repository *Repository) ListTranscript(ctx context.Context, meetingID string, afterSeq int64, beforeSeq int64, limit int) ([]TranscriptRow, SeqPageState, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || afterSeq < 0 || beforeSeq < 0 || (afterSeq > 0 && beforeSeq > 0) {
		return nil, SeqPageState{}, fmt.Errorf("读取原始记录：参数无效")
	}
	limit = normalizeSeqLimit(limit, 200)
	query := repository.reader.WithContext(ctx).Table("meeting_events AS event").Select(
		"event.seq", "event.kind", "event.occurred_at",
		`CASE
			WHEN event.kind = 'utterance.final' THEN COALESCE(utterance.current_text, '')
			WHEN event.kind = 'message.created' THEN COALESCE(message.content, '')
			WHEN event.kind = 'resource.created' THEN COALESCE(NULLIF(resource.current_description, ''), NULLIF(resource.original_name, ''), NULLIF(resource.source_url, ''), '')
			WHEN event.kind IN ('ai.question', 'ai.answer') THEN COALESCE(json_extract(event.payload_json, '$.text'), '')
			WHEN event.kind = 'ai.cancelled' THEN 'AI 回答已取消'
			WHEN event.kind = 'ai.failed' THEN 'AI 回答失败'
			WHEN event.kind = 'asr.gap' THEN '实时转写中断'
			ELSE ''
		END AS text`,
		"COALESCE(participant.display_name_snapshot, '') AS speaker_name",
		"COALESCE(cluster.display_no, 0) AS cluster_display_no",
		"COALESCE(track.display_no, 0) AS track_display_no",
		"COALESCE(utterance.start_sample, 0) AS start_sample", "COALESCE(utterance.end_sample, 0) AS end_sample",
	).Joins("LEFT JOIN utterances AS utterance ON utterance.event_id = event.id AND utterance.meeting_id = event.meeting_id").
		Joins("LEFT JOIN agent_voice_command_utterances AS voice_command ON voice_command.utterance_id = utterance.id").
		Joins("LEFT JOIN meeting_participants AS participant ON participant.id = utterance.current_participant_id AND participant.meeting_id = event.meeting_id").
		Joins("LEFT JOIN speaker_clusters AS cluster ON cluster.id = utterance.speaker_cluster_id AND cluster.meeting_id = event.meeting_id").
		Joins("LEFT JOIN speaker_tracks AS track ON track.id = utterance.speaker_track_id AND track.meeting_id = event.meeting_id").
		Joins("LEFT JOIN messages AS message ON message.event_id = event.id AND message.meeting_id = event.meeting_id").
		Joins("LEFT JOIN resources AS resource ON resource.event_id = event.id AND resource.meeting_id = event.meeting_id").
		Where("event.meeting_id = ? AND event.kind IN ?", meetingID, []string{
			"utterance.final", "asr.gap", "message.created", "resource.created",
			"ai.question", "ai.answer", "ai.cancelled", "ai.failed",
		}).Where("voice_command.id IS NULL OR voice_command.state = 'released'")
	reverse := beforeSeq > 0
	if afterSeq > 0 {
		query = query.Where("event.seq > ?", afterSeq)
	}
	if reverse {
		query = query.Where("event.seq < ?", beforeSeq).Order("event.seq DESC")
	} else {
		query = query.Order("event.seq ASC")
	}
	var rows []TranscriptRow
	if err := query.Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, SeqPageState{}, fmt.Errorf("读取原始记录失败：%w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	if reverse {
		slices.Reverse(rows)
	}
	return rows, buildSeqPageState(afterSeq, beforeSeq, hasMore), nil
}

// ListContent 按事件 seq 读取消息、资源和明确公开的 AI 回答。
func (repository *Repository) ListContent(ctx context.Context, meetingID string, afterSeq int64, beforeSeq int64, limit int) ([]ContentRow, SeqPageState, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || afterSeq < 0 || beforeSeq < 0 || (afterSeq > 0 && beforeSeq > 0) {
		return nil, SeqPageState{}, fmt.Errorf("读取会议内容：参数无效")
	}
	limit = normalizeSeqLimit(limit, 100)
	query := repository.reader.WithContext(ctx).Table("meeting_events AS event").Select(
		"event.seq", "event.kind", "event.occurred_at",
		"COALESCE(message.id, resource.id, event.entity_id, '') AS entity_id",
		"COALESCE(message.display_name_snapshot, guest.display_name, '') AS display_name",
		"CASE WHEN event.kind = 'message.created' THEN COALESCE(message.content, '') WHEN event.kind = 'ai.answer' AND json_extract(event.payload_json, '$.guest_visible') = 1 THEN COALESCE(json_extract(event.payload_json, '$.text'), '') ELSE COALESCE(resource.current_description, '') END AS text",
		"COALESCE(resource.kind, '') AS resource_kind", "COALESCE(resource.original_name, '') AS resource_name",
		"CASE WHEN resource.integrity_state IS NOT NULL AND resource.integrity_state <> 'unchecked' THEN resource.integrity_state ELSE COALESCE(resource.state, '') END AS resource_state", "COALESCE(resource.source_url, '') AS source_url",
	).Joins("LEFT JOIN messages AS message ON message.event_id = event.id AND message.meeting_id = event.meeting_id").
		Joins("LEFT JOIN resources AS resource ON resource.event_id = event.id AND resource.meeting_id = event.meeting_id").
		Joins("LEFT JOIN guest_sessions AS guest ON guest.id = resource.guest_session_id AND guest.meeting_id = event.meeting_id").
		Where("event.meeting_id = ? AND event.kind IN ?", meetingID, []string{"message.created", "resource.created", "ai.answer"}).
		Where("event.kind <> 'ai.answer' OR (json_extract(event.payload_json, '$.guest_visible') = 1 AND trim(COALESCE(json_extract(event.payload_json, '$.text'), '')) <> '')")
	reverse := beforeSeq > 0
	if afterSeq > 0 {
		query = query.Where("event.seq > ?", afterSeq)
	}
	if reverse {
		query = query.Where("event.seq < ?", beforeSeq).Order("event.seq DESC")
	} else {
		query = query.Order("event.seq ASC")
	}
	var rows []ContentRow
	if err := query.Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, SeqPageState{}, fmt.Errorf("读取会议内容失败：%w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	if reverse {
		slices.Reverse(rows)
	}
	return rows, buildSeqPageState(afterSeq, beforeSeq, hasMore), nil
}

// buildSeqPageState 将当前查询方向与多取一条的结果转换为明确的前后页状态。
func buildSeqPageState(afterSeq int64, beforeSeq int64, hasMore bool) SeqPageState {
	if beforeSeq > 0 {
		return SeqPageState{HasPrevious: hasMore, HasNext: true}
	}
	if afterSeq > 0 {
		return SeqPageState{HasPrevious: true, HasNext: hasMore}
	}
	return SeqPageState{HasNext: hasMore}
}

// normalizeSeqLimit 固定长列表单页上限。
func normalizeSeqLimit(limit int, maximum int) int {
	if limit <= 0 || limit > maximum {
		return maximum
	}
	return limit
}
