package gap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// ConflictUtteranceRow 是冲突页展示与 revision 校验所需的当前事实。
type ConflictUtteranceRow struct {
	ID           string `gorm:"column:id"`
	Seq          int64  `gorm:"column:seq"`
	OriginalText string `gorm:"column:original_text"`
	CurrentText  string `gorm:"column:current_text"`
	StartSample  int64  `gorm:"column:start_sample"`
	EndSample    int64  `gorm:"column:end_sample"`
	TextRevision int    `gorm:"column:text_revision"`
}

// ConflictRecord 汇总内部 attempt、gap、候选与相邻当前事实。
type ConflictRecord struct {
	Gap        models.ASRGap
	Attempt    models.GapTranscriptionAttempt
	AudioAsset models.AudioAsset
	Existing   []ConflictUtteranceRow
	Context    []ConflictUtteranceRow
}

// ReadConflict 读取指定 gap 的实时双份证据，不返回文件绝对路径。
func (repository *Repository) ReadConflict(ctx context.Context, meetingID string, gapID string) (ConflictRecord, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || gapID == "" {
		return ConflictRecord{}, fmt.Errorf("读取 gap 冲突：参数无效")
	}
	var record ConflictRecord
	if err := repository.reader.WithContext(ctx).Where("id = ? AND meeting_id = ? AND state = 'conflict'", gapID, meetingID).Take(&record.Gap).Error; err != nil {
		return ConflictRecord{}, ErrConflict
	}
	const attemptStatement = `SELECT attempt.* FROM gap_transcription_attempts AS attempt
JOIN gap_transcription_attempt_items AS item ON item.attempt_id=attempt.id
WHERE item.gap_id=? AND attempt.meeting_id=? AND attempt.state='conflict'
ORDER BY attempt.created_at DESC LIMIT 1`
	if err := repository.reader.WithContext(ctx).Raw(attemptStatement, gapID, meetingID).Take(&record.Attempt).Error; err != nil {
		return ConflictRecord{}, fmt.Errorf("读取冲突 attempt 失败：%w", err)
	}
	if err := repository.reader.WithContext(ctx).Where("id = ?", record.Attempt.AudioAssetID).Take(&record.AudioAsset).Error; err != nil {
		return ConflictRecord{}, fmt.Errorf("读取冲突音频资产失败：%w", err)
	}
	const utteranceStatement = `SELECT utterance.id, event.seq, utterance.original_text, utterance.current_text,
       utterance.start_sample, utterance.end_sample, utterance.text_revision
FROM utterances AS utterance JOIN meeting_events AS event ON event.id=utterance.event_id
WHERE utterance.meeting_id=? AND utterance.start_sample<? AND utterance.end_sample>?
ORDER BY utterance.start_sample ASC, utterance.id ASC`
	if err := repository.reader.WithContext(ctx).Raw(utteranceStatement, meetingID, record.Attempt.CoreEndSample, record.Attempt.CoreStartSample).Scan(&record.Existing).Error; err != nil {
		return ConflictRecord{}, fmt.Errorf("读取冲突当前转写失败：%w", err)
	}
	const contextStatement = `SELECT utterance.id, event.seq, utterance.original_text, utterance.current_text,
       utterance.start_sample, utterance.end_sample, utterance.text_revision
FROM utterances AS utterance JOIN meeting_events AS event ON event.id=utterance.event_id
WHERE utterance.meeting_id=? AND utterance.end_sample>=? AND utterance.start_sample<=?
ORDER BY utterance.start_sample ASC, utterance.id ASC LIMIT 20`
	if err := repository.reader.WithContext(ctx).Raw(contextStatement, meetingID, max64(0, record.Attempt.CoreStartSample-160000), record.Attempt.CoreEndSample+160000).Scan(&record.Context).Error; err != nil {
		return ConflictRecord{}, fmt.Errorf("读取冲突相邻上下文失败：%w", err)
	}
	return record, nil
}

// ResolutionEdit 是事务内单条人工文字更新。
type ResolutionEdit struct {
	TargetID         string
	ExpectedRevision int
	Text             string
}

// ResolveConflictInput 描述冲突解决的全部固定事实。
type ResolveConflictInput struct {
	MeetingID         string
	GapID             string
	AttemptID         string
	ExpectedUpdatedAt int64
	RequestID         string
	Resolution        string
	ResolutionEvent   models.MeetingEvent
	Session           *models.ASRSession
	Events            []models.MeetingEvent
	Utterances        []models.Utterance
	CorrectionEvents  []models.MeetingEvent
	Corrections       []models.Correction
	Edits             []ResolutionEdit
	UpdatedAt         int64
}

// HasMatchingResolution 判断 request ID 是否已提交完全相同的 gap 解决动作。
func (repository *Repository) HasMatchingResolution(ctx context.Context, meetingID string, gapID string, resolution string, requestID string) (bool, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || gapID == "" || resolution == "" || requestID == "" {
		return false, fmt.Errorf("读取冲突解决幂等事实：参数无效")
	}
	return resolutionExists(ctx, repository.reader, meetingID, gapID, resolution, requestID)
}

// ResolveGapConflict 原子提交非重叠补偿、人工校正与 gap/attempt 聚合。
func (repository *Repository) ResolveGapConflict(ctx context.Context, input ResolveConflictInput) error {
	if repository == nil || repository.transactions == nil || input.MeetingID == "" || input.GapID == "" || input.AttemptID == "" || input.RequestID == "" || input.ResolutionEvent.ID == "" {
		return fmt.Errorf("解决 gap 冲突：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		duplicate, err := resolutionExists(ctx, tx, input.MeetingID, input.GapID, input.Resolution, input.RequestID)
		if err != nil || duplicate {
			return err
		}
		var gap models.ASRGap
		if err := tx.WithContext(ctx).Where("id = ? AND meeting_id = ? AND state = 'conflict' AND updated_at = ?", input.GapID, input.MeetingID, input.ExpectedUpdatedAt).Take(&gap).Error; err != nil {
			return ErrConflict
		}
		for _, edit := range input.Edits {
			result := tx.WithContext(ctx).Model(&models.Utterance{}).Where("id = ? AND meeting_id = ? AND text_revision = ?", edit.TargetID, input.MeetingID, edit.ExpectedRevision).
				Updates(map[string]any{"current_text": edit.Text, "text_revision": edit.ExpectedRevision + 1, "updated_at": input.UpdatedAt})
			if result.Error != nil || result.RowsAffected != 1 {
				return ErrConflict
			}
		}
		if input.Session != nil {
			if err := tx.WithContext(ctx).Create(input.Session).Error; err != nil {
				return fmt.Errorf("创建冲突解决 ASR session 失败：%w", err)
			}
		}
		createdEvents := append([]models.MeetingEvent(nil), input.Events...)
		createdEvents = append(createdEvents, input.CorrectionEvents...)
		createdEvents = append(createdEvents, input.ResolutionEvent)
		for index := range createdEvents {
			sequence, err := nextSeq(ctx, tx, input.MeetingID)
			if err != nil {
				return err
			}
			createdEvents[index].Seq = sequence
			if err := tx.WithContext(ctx).Create(&createdEvents[index]).Error; err != nil {
				return fmt.Errorf("创建冲突解决事件失败：%w", err)
			}
		}
		for index := range input.Utterances {
			input.Utterances[index].EventID = createdEvents[index].ID
			if err := tx.WithContext(ctx).Create(&input.Utterances[index]).Error; err != nil {
				return fmt.Errorf("创建冲突解决补偿转写失败：%w", err)
			}
		}
		for index := range input.Corrections {
			if index >= len(input.CorrectionEvents) || input.Corrections[index].EventID != input.CorrectionEvents[index].ID {
				return fmt.Errorf("冲突解决校正事件不匹配")
			}
			if err := tx.WithContext(ctx).Create(&input.Corrections[index]).Error; err != nil {
				return fmt.Errorf("创建冲突解决校正失败：%w", err)
			}
		}
		fromSeq, toSeq := input.ResolutionEvent.Seq, input.ResolutionEvent.Seq
		if len(createdEvents) > 0 {
			fromSeq, toSeq = createdEvents[0].Seq, createdEvents[len(createdEvents)-1].Seq
		}
		gapResult := tx.WithContext(ctx).Model(&models.ASRGap{}).Where("id = ? AND state = 'conflict' AND updated_at = ?", gap.ID, input.ExpectedUpdatedAt).
			Updates(map[string]any{"state": "completed", "result_from_seq": fromSeq, "result_to_seq": toSeq, "conflict_json": nil, "last_error_code": nil, "updated_at": input.UpdatedAt})
		if gapResult.Error != nil || gapResult.RowsAffected != 1 {
			return ErrConflict
		}
		if err := settleResolvedAttempt(ctx, tx, input.AttemptID, input.UpdatedAt); err != nil {
			return err
		}
		return updateAggregate(ctx, tx, input.MeetingID, input.UpdatedAt)
	})
}

// resolutionExists 以 resolution event payload 中的 request_id 实现三种动作的统一幂等。
func resolutionExists(ctx context.Context, tx *gorm.DB, meetingID string, gapID string, resolution string, requestID string) (bool, error) {
	var event models.MeetingEvent
	err := tx.WithContext(ctx).Where("meeting_id = ? AND kind = 'asr.compensated' AND json_extract(payload_json,'$.request_id') = ?", meetingID, requestID).Take(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取冲突解决幂等事实失败：%w", err)
	}
	var payload struct {
		Resolution string `json:"resolution"`
	}
	if event.EntityID == nil || *event.EntityID != gapID || event.PayloadJSON == nil || json.Unmarshal([]byte(*event.PayloadJSON), &payload) != nil || payload.Resolution != resolution {
		return false, ErrConflict
	}
	return true, nil
}

// settleResolvedAttempt 在全部 item gap 完成后把保留证据的 attempt 切为 completed。
func settleResolvedAttempt(ctx context.Context, tx *gorm.DB, attemptID string, updatedAt int64) error {
	var unresolved int64
	err := tx.WithContext(ctx).Raw(`SELECT COUNT(*) FROM gap_transcription_attempt_items item JOIN asr_gaps gap ON gap.id=item.gap_id WHERE item.attempt_id=? AND gap.state<>'completed'`, attemptID).Scan(&unresolved).Error
	if err != nil {
		return err
	}
	if unresolved == 0 {
		result := tx.WithContext(ctx).Model(&models.GapTranscriptionAttempt{}).Where("id = ? AND state = 'conflict'", attemptID).Updates(map[string]any{"state": "completed", "last_error_code": nil, "updated_at": updatedAt})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
	}
	return nil
}

// max64 返回两个 int64 中较大值。
func max64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
