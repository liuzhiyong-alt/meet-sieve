package minutes

import (
	"context"
	"fmt"

	domainminutes "meet-sieve/internal/domain/minutes"

	"gorm.io/gorm"
)

// ReadFactSnapshot 在单个 SQLite 读快照中冻结 cutoff、白名单事实与未解决 gap。
func (repository *Repository) ReadFactSnapshot(ctx context.Context, meetingID string) (domainminutes.Context, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return domainminutes.Context{}, fmt.Errorf("读取纪要事实：参数无效")
	}
	var result domainminutes.Context
	err := repository.reader.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := readMeetingSnapshot(ctx, tx, meetingID, &result.Meeting); err != nil {
			return err
		}
		facts, err := readWhitelistedFacts(ctx, tx, meetingID, result.Meeting.CutoffSeq)
		if err != nil {
			return err
		}
		gaps, err := readUnresolvedGaps(ctx, tx, meetingID, result.Meeting.CutoffSeq)
		if err != nil {
			return err
		}
		result.Facts, result.Gaps = facts, gaps
		return nil
	})
	return result, err
}

// readMeetingSnapshot 读取可生成会议并固定最大事件序号。
func readMeetingSnapshot(ctx context.Context, tx *gorm.DB, meetingID string, target *domainminutes.MeetingSnapshot) error {
	var row struct {
		MeetingID string `gorm:"column:meeting_id"`
		MeetingNo string `gorm:"column:meeting_no"`
		Subject   string `gorm:"column:subject"`
		StartedAt int64  `gorm:"column:started_at"`
		EndedAt   int64  `gorm:"column:ended_at"`
		CutoffSeq int64  `gorm:"column:cutoff_seq"`
		Timezone  string `gorm:"column:timezone"`
	}
	const statement = `SELECT meeting.id AS meeting_id, meeting.meeting_no, meeting.subject,
       COALESCE(meeting.started_at, 0) AS started_at, COALESCE(meeting.ended_at, 0) AS ended_at,
       meeting.local_timezone AS timezone,
       COALESCE((SELECT MAX(seq) FROM meeting_events WHERE meeting_id=meeting.id), 0) AS cutoff_seq
FROM meetings AS meeting
WHERE meeting.id=? AND meeting.lifecycle_state IN ('ended','interrupted') AND meeting.local_save_state='saved'`
	if err := tx.WithContext(ctx).Raw(statement, meetingID).Take(&row).Error; err != nil {
		return fmt.Errorf("读取纪要会议快照失败：%w", err)
	}
	*target = domainminutes.MeetingSnapshot{MeetingID: row.MeetingID, MeetingNo: row.MeetingNo, Subject: row.Subject, StartedAt: row.StartedAt, EndedAt: row.EndedAt, CutoffSeq: row.CutoffSeq, Timezone: row.Timezone}
	if err := tx.WithContext(ctx).Raw(`SELECT display_name_snapshot FROM meeting_participants WHERE meeting_id=? ORDER BY sort_order ASC, id ASC`, meetingID).Scan(&target.Participants).Error; err != nil {
		return fmt.Errorf("读取纪要参会者快照失败：%w", err)
	}
	return nil
}

// readWhitelistedFacts 只 join 三类本地事实，不触及任何 AI、snapshot、tool 或审批表。
func readWhitelistedFacts(ctx context.Context, tx *gorm.DB, meetingID string, cutoff int64) ([]domainminutes.Fact, error) {
	const statement = `SELECT event.seq, event.occurred_at,
       CASE WHEN utterance.id IS NOT NULL THEN 'utterance'
            WHEN message.id IS NOT NULL THEN 'message' ELSE 'resource' END AS kind,
       COALESCE(utterance.start_sample, 0) AS start_sample,
       COALESCE(utterance.end_sample, 0) AS end_sample,
       COALESCE(participant.display_name_snapshot, message.display_name_snapshot, '') AS speaker,
       COALESCE(utterance.current_text, message.content, resource.current_description,
                resource.original_description, resource.original_name, resource.safe_name, resource.source_url, '') AS text,
       COALESCE(resource.kind, '') AS resource_kind,
       COALESCE(resource.safe_name, resource.original_name, '') AS resource_name,
       COALESCE(resource.media_type, '') AS media_type,
       COALESCE(resource.size_bytes, 0) AS size_bytes,
       COALESCE(utterance.text_revision, resource.description_revision, 0) AS revision
FROM meeting_events AS event
LEFT JOIN utterances AS utterance
  ON event.kind IN ('utterance.final','asr.compensated') AND utterance.event_id=event.id AND utterance.meeting_id=event.meeting_id
LEFT JOIN meeting_participants AS participant ON participant.id=utterance.current_participant_id
LEFT JOIN messages AS message
  ON event.kind='message.created' AND message.event_id=event.id AND message.meeting_id=event.meeting_id
LEFT JOIN resources AS resource
  ON event.kind='resource.created' AND resource.event_id=event.id AND resource.meeting_id=event.meeting_id AND resource.state='completed'
WHERE event.meeting_id=? AND event.seq<=?
  AND (utterance.id IS NOT NULL OR message.id IS NOT NULL OR resource.id IS NOT NULL)
ORDER BY event.seq ASC`
	var facts []domainminutes.Fact
	if err := tx.WithContext(ctx).Raw(statement, meetingID, cutoff).Scan(&facts).Error; err != nil {
		return nil, fmt.Errorf("读取纪要白名单事实失败：%w", err)
	}
	return facts, nil
}

// readUnresolvedGaps 固定 cutoff 时仍非 completed 的全部 gap 快照。
func readUnresolvedGaps(ctx context.Context, tx *gorm.DB, meetingID string, cutoff int64) ([]domainminutes.GapNotice, error) {
	const statement = `SELECT gap.start_sample, gap.end_sample, gap.state
FROM asr_gaps AS gap JOIN meeting_events AS event ON event.id=gap.event_id
WHERE gap.meeting_id=? AND event.seq<=? AND gap.state<>'completed'
ORDER BY gap.start_sample ASC, gap.end_sample ASC, gap.id ASC`
	var gaps []domainminutes.GapNotice
	if err := tx.WithContext(ctx).Raw(statement, meetingID, cutoff).Scan(&gaps).Error; err != nil {
		return nil, fmt.Errorf("读取纪要 gap 快照失败：%w", err)
	}
	return gaps, nil
}
