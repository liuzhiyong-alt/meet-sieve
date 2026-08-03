package agent

import (
	"context"
	"fmt"
)

// TimelineEntry 是主持人会中页可见的持久 AI 事件投影。
type TimelineEntry struct {
	Seq        int64
	Kind       string
	OccurredAt int64
	TurnID     string
	Text       string
	Reason     string
}

// ListTimeline 按 seq 返回问题、最终回答和终止状态，不返回 prompt 或 partial。
func (repository *Repository) ListTimeline(ctx context.Context, meetingID string, afterSeq int64, limit int) ([]TimelineEntry, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || afterSeq < 0 {
		return nil, fmt.Errorf("读取 AI 时间线：参数无效")
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	var entries []TimelineEntry
	err := repository.reader.WithContext(ctx).Raw(`
SELECT seq, kind, occurred_at, COALESCE(entity_id, '') AS turn_id,
       CASE WHEN kind IN ('ai.question', 'ai.answer') THEN COALESCE(json_extract(payload_json, '$.text'), '') ELSE '' END AS text,
       CASE WHEN kind IN ('ai.cancelled', 'ai.failed') THEN COALESCE(json_extract(payload_json, '$.reason'), '') ELSE '' END AS reason
FROM meeting_events
WHERE meeting_id = ? AND seq > ? AND kind IN ('ai.question', 'ai.answer', 'ai.cancelled', 'ai.failed')
ORDER BY seq ASC
LIMIT ?`, meetingID, afterSeq, limit).Scan(&entries).Error
	if err != nil {
		return nil, fmt.Errorf("读取 AI 时间线失败：%w", err)
	}
	return entries, nil
}
