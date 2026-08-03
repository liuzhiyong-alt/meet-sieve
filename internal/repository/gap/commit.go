package gap

import (
	"context"
	"encoding/json"
	"fmt"

	domaingap "meet-sieve/internal/domain/gap"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// CompensationInput 描述补偿事实原子提交所需的完整、已验证输入。
type CompensationInput struct {
	AttemptID           string
	Session             models.ASRSession
	Events              []models.MeetingEvent
	Utterances          []models.Utterance
	ResponseJSON        string
	ProviderLogIDSuffix string
	UpdatedAt           int64
}

// NoSpeechInput 描述无语音补偿事件的原子提交输入。
type NoSpeechInput struct {
	AttemptID           string
	Session             models.ASRSession
	Event               models.MeetingEvent
	ResponseJSON        string
	ProviderLogIDSuffix string
	UpdatedAt           int64
}

// ConflictInput 描述补转写冲突证据的原子提交输入。
type ConflictInput struct {
	AttemptID           string
	ResponseJSON        string
	ConflictJSON        string
	ProviderLogIDSuffix string
	UpdatedAt           int64
}

// CommitCompensation 原子创建 synthetic session、事件和 utterance，并完成关联 gap。
func (repository *Repository) CommitCompensation(ctx context.Context, input CompensationInput) error {
	if repository == nil || repository.transactions == nil || !validCompensation(input) {
		return fmt.Errorf("提交补转写结果：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		meetingID, gapIDs, err := loadRunningAttempt(ctx, tx, input.AttemptID)
		if err != nil {
			return err
		}
		if input.Session.MeetingID != meetingID {
			return ErrConflict
		}
		conflict, err := hasCurrentOverlap(ctx, tx, meetingID, input.Utterances)
		if err != nil {
			return err
		}
		if conflict {
			return ErrConflict
		}
		if err := createSyntheticSession(ctx, tx, input.Session); err != nil {
			return err
		}
		fromSeq, toSeq, err := createCompensatedUtterances(ctx, tx, meetingID, input.Events, input.Utterances)
		if err != nil {
			return err
		}
		return completeAttempt(ctx, tx, input.AttemptID, gapIDs, fromSeq, toSeq, input.ResponseJSON, input.ProviderLogIDSuffix, input.UpdatedAt)
	})
}

// CommitNoSpeechCompensation 原子创建无语音事件并完成关联 gap，不创建空 utterance。
func (repository *Repository) CommitNoSpeechCompensation(ctx context.Context, input NoSpeechInput) error {
	if repository == nil || repository.transactions == nil || input.AttemptID == "" || input.Event.ID == "" || !json.Valid([]byte(input.ResponseJSON)) {
		return fmt.Errorf("提交无语音补偿：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		meetingID, gapIDs, err := loadRunningAttempt(ctx, tx, input.AttemptID)
		if err != nil {
			return err
		}
		if input.Session.MeetingID != meetingID {
			return ErrConflict
		}
		if err := createSyntheticSession(ctx, tx, input.Session); err != nil {
			return err
		}
		sequence, err := nextSeq(ctx, tx, meetingID)
		if err != nil {
			return err
		}
		input.Event.Seq = sequence
		if err := tx.WithContext(ctx).Create(&input.Event).Error; err != nil {
			return fmt.Errorf("创建无语音补偿事件失败：%w", err)
		}
		return completeAttempt(ctx, tx, input.AttemptID, gapIDs, sequence, sequence, input.ResponseJSON, input.ProviderLogIDSuffix, input.UpdatedAt)
	})
}

// CommitGapConflict 原子保存有限候选和重叠摘要，不创建文件 utterance。
func (repository *Repository) CommitGapConflict(ctx context.Context, input ConflictInput) error {
	if repository == nil || repository.transactions == nil || input.AttemptID == "" || !validLimitedJSON(input.ResponseJSON) || !validLimitedJSON(input.ConflictJSON) {
		return fmt.Errorf("提交补转写冲突：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		meetingID, gapIDs, err := loadRunningAttempt(ctx, tx, input.AttemptID)
		if err != nil {
			return err
		}
		gapResult := tx.WithContext(ctx).Model(&models.ASRGap{}).
			Where("id IN ? AND meeting_id = ? AND state = 'processing'", gapIDs, meetingID).
			Updates(map[string]any{"state": "conflict", "conflict_json": input.ConflictJSON, "last_error_code": "GAP_TRANSCRIPTION_CONFLICT", "updated_at": input.UpdatedAt})
		if gapResult.Error != nil || gapResult.RowsAffected != int64(len(gapIDs)) {
			return ErrConflict
		}
		attemptResult := tx.WithContext(ctx).Model(&models.GapTranscriptionAttempt{}).
			Where("id = ? AND state = 'running'", input.AttemptID).
			Updates(map[string]any{"state": "conflict", "response_json": input.ResponseJSON, "provider_log_id_suffix": input.ProviderLogIDSuffix, "last_error_code": "GAP_TRANSCRIPTION_CONFLICT", "ended_at": input.UpdatedAt, "updated_at": input.UpdatedAt})
		if attemptResult.Error != nil || attemptResult.RowsAffected != 1 {
			return ErrConflict
		}
		return updateAggregate(ctx, tx, meetingID, input.UpdatedAt)
	})
}

// FailGapAttempt 原子结束活动尝试并把关联 gap 恢复为 failed。
func (repository *Repository) FailGapAttempt(ctx context.Context, attemptID string, errorCode string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || attemptID == "" || errorCode == "" {
		return fmt.Errorf("结束补转写尝试：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		meetingID, gapIDs, err := loadRunningAttempt(ctx, tx, attemptID)
		if err != nil {
			return err
		}
		gapResult := tx.WithContext(ctx).Model(&models.ASRGap{}).
			Where("id IN ? AND state = 'processing'", gapIDs).
			Updates(map[string]any{"state": "failed", "last_error_code": errorCode, "updated_at": updatedAt})
		if gapResult.Error != nil || gapResult.RowsAffected != int64(len(gapIDs)) {
			return ErrConflict
		}
		attemptResult := tx.WithContext(ctx).Model(&models.GapTranscriptionAttempt{}).
			Where("id = ? AND state = 'running'", attemptID).
			Updates(map[string]any{"state": "failed", "last_error_code": errorCode, "ended_at": updatedAt, "updated_at": updatedAt})
		if attemptResult.Error != nil || attemptResult.RowsAffected != 1 {
			return ErrConflict
		}
		return updateAggregate(ctx, tx, meetingID, updatedAt)
	})
}

// RecoverInterrupted 把异常退出遗留的 running attempt 收敛为 failed，不自动重新请求 provider。
func (repository *Repository) RecoverInterrupted(ctx context.Context) error {
	return repository.RecoverInterruptedAt(ctx, 0)
}

// RecoverInterruptedAt 使用调用方提供的恢复时间收敛遗留 attempt；零值兼容旧调用方。
func (repository *Repository) RecoverInterruptedAt(ctx context.Context, updatedAt int64) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("恢复补转写尝试：Repository 不可用")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var attempts []models.GapTranscriptionAttempt
		if err := tx.WithContext(ctx).Where("state = 'running'").Order("created_at ASC").Find(&attempts).Error; err != nil {
			return fmt.Errorf("读取遗留补转写尝试失败：%w", err)
		}
		for _, attempt := range attempts {
			settledAt := updatedAt
			if settledAt <= 0 {
				settledAt = attempt.UpdatedAt
			}
			if err := recoverAttempt(ctx, tx, attempt, settledAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// recoverAttempt 在同一恢复事务中收敛单个 attempt、关联 gap 和会议聚合。
func recoverAttempt(ctx context.Context, tx *gorm.DB, attempt models.GapTranscriptionAttempt, updatedAt int64) error {
	var items []models.GapTranscriptionAttemptItem
	if err := tx.WithContext(ctx).Where("attempt_id = ?", attempt.ID).Find(&items).Error; err != nil {
		return fmt.Errorf("读取遗留 attempt items 失败：%w", err)
	}
	gapIDs := make([]string, 0, len(items))
	for _, item := range items {
		gapIDs = append(gapIDs, item.GapID)
	}
	gapResult := tx.WithContext(ctx).Model(&models.ASRGap{}).
		Where("id IN ? AND state = 'processing'", gapIDs).
		Updates(map[string]any{"state": "failed", "last_error_code": "GAP_ATTEMPT_INTERRUPTED", "updated_at": updatedAt})
	if gapResult.Error != nil || gapResult.RowsAffected != int64(len(gapIDs)) {
		return ErrConflict
	}
	attemptResult := tx.WithContext(ctx).Model(&models.GapTranscriptionAttempt{}).
		Where("id = ? AND state = 'running'", attempt.ID).
		Updates(map[string]any{"state": "failed", "last_error_code": "GAP_ATTEMPT_INTERRUPTED", "ended_at": updatedAt, "updated_at": updatedAt})
	if attemptResult.Error != nil || attemptResult.RowsAffected != 1 {
		return ErrConflict
	}
	return updateAggregate(ctx, tx, attempt.MeetingID, updatedAt)
}

// validCompensation 校验事件和 utterance 必须一一对应且响应是有限规范化 JSON。
func validCompensation(input CompensationInput) bool {
	return input.AttemptID != "" && input.Session.ID != "" && len(input.Events) > 0 &&
		len(input.Events) == len(input.Utterances) && validLimitedJSON(input.ResponseJSON)
}

// validLimitedJSON 限制冲突证据体积并拒绝非法 JSON。
func validLimitedJSON(value string) bool {
	return len(value) > 0 && len(value) <= 512*1024 && json.Valid([]byte(value))
}

// loadRunningAttempt 锁定来源状态并按 item_order 返回关联 gap。
func loadRunningAttempt(ctx context.Context, tx *gorm.DB, attemptID string) (string, []string, error) {
	var attempt models.GapTranscriptionAttempt
	if err := tx.WithContext(ctx).Where("id = ? AND state = 'running'", attemptID).Take(&attempt).Error; err != nil {
		return "", nil, ErrConflict
	}
	var items []models.GapTranscriptionAttemptItem
	if err := tx.WithContext(ctx).Where("attempt_id = ?", attemptID).Order("item_order ASC").Find(&items).Error; err != nil {
		return "", nil, fmt.Errorf("读取补转写关联 gap 失败：%w", err)
	}
	if len(items) == 0 {
		return "", nil, ErrConflict
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.GapID)
	}
	return attempt.MeetingID, ids, nil
}

// hasCurrentOverlap 在提交时二次查询当前 utterance，避免识别期间事实变化被覆盖。
func hasCurrentOverlap(ctx context.Context, tx *gorm.DB, meetingID string, values []models.Utterance) (bool, error) {
	for _, value := range values {
		var count int64
		err := tx.WithContext(ctx).Model(&models.Utterance{}).
			Where("meeting_id = ? AND start_sample < ? AND end_sample > ?", meetingID, value.EndSample, value.StartSample).
			Count(&count).Error
		if err != nil {
			return false, fmt.Errorf("检查补转写重叠失败：%w", err)
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

// createSyntheticSession 创建一次文件请求对应的 stopped ASR session。
func createSyntheticSession(ctx context.Context, tx *gorm.DB, session models.ASRSession) error {
	if session.TransportMode != "auc_flash_v3" || session.State != "stopped" {
		return fmt.Errorf("文件 ASR session 无效")
	}
	if err := tx.WithContext(ctx).Create(&session).Error; err != nil {
		return fmt.Errorf("创建文件 ASR session 失败：%w", err)
	}
	return nil
}

// createCompensatedUtterances 按输入顺序分配连续 seq 并创建补偿事实。
func createCompensatedUtterances(ctx context.Context, tx *gorm.DB, meetingID string, events []models.MeetingEvent, utterances []models.Utterance) (int64, int64, error) {
	sequence, err := nextSeq(ctx, tx, meetingID)
	if err != nil {
		return 0, 0, err
	}
	from := sequence
	for index := range events {
		events[index].Seq = sequence
		utterances[index].EventID = events[index].ID
		if err := tx.WithContext(ctx).Create(&events[index]).Error; err != nil {
			return 0, 0, fmt.Errorf("创建补偿事件失败：%w", err)
		}
		if err := tx.WithContext(ctx).Create(&utterances[index]).Error; err != nil {
			return 0, 0, fmt.Errorf("创建补偿 utterance 失败：%w", err)
		}
		sequence++
	}
	return from, sequence - 1, nil
}

// completeAttempt 同事务完成 gap、attempt 和会议聚合状态。
func completeAttempt(ctx context.Context, tx *gorm.DB, attemptID string, gapIDs []string, fromSeq int64, toSeq int64, responseJSON string, logSuffix string, updatedAt int64) error {
	gapResult := tx.WithContext(ctx).Model(&models.ASRGap{}).
		Where("id IN ? AND state = 'processing'", gapIDs).
		Updates(map[string]any{"state": "completed", "result_from_seq": fromSeq, "result_to_seq": toSeq, "last_error_code": nil, "updated_at": updatedAt})
	if gapResult.Error != nil || gapResult.RowsAffected != int64(len(gapIDs)) {
		return ErrConflict
	}
	attemptResult := tx.WithContext(ctx).Model(&models.GapTranscriptionAttempt{}).
		Where("id = ? AND state = 'running'", attemptID).
		Updates(map[string]any{"state": "completed", "response_json": responseJSON, "provider_log_id_suffix": logSuffix, "last_error_code": nil, "ended_at": updatedAt, "updated_at": updatedAt})
	if attemptResult.Error != nil || attemptResult.RowsAffected != 1 {
		return ErrConflict
	}
	var attempt models.GapTranscriptionAttempt
	if err := tx.WithContext(ctx).Select("meeting_id").Where("id = ?", attemptID).Take(&attempt).Error; err != nil {
		return fmt.Errorf("读取已完成 attempt 会议失败：%w", err)
	}
	return updateAggregate(ctx, tx, attempt.MeetingID, updatedAt)
}

// updateAggregate 从 gap 明细确定性更新 meeting.gap_state。
func updateAggregate(ctx context.Context, tx *gorm.DB, meetingID string, updatedAt int64) error {
	var states []string
	if err := tx.WithContext(ctx).Model(&models.ASRGap{}).Where("meeting_id = ?", meetingID).Pluck("state", &states).Error; err != nil {
		return fmt.Errorf("读取 gap 聚合状态失败：%w", err)
	}
	domainStates := make([]domaingap.State, 0, len(states))
	for _, state := range states {
		domainStates = append(domainStates, domaingap.State(state))
	}
	result := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ?", meetingID).
		Updates(map[string]any{"gap_state": domaingap.AggregateState(domainStates), "updated_at": updatedAt})
	if result.Error != nil || result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

// nextSeq 在单 writer 事务内分配下一条会议事件序号。
func nextSeq(ctx context.Context, tx *gorm.DB, meetingID string) (int64, error) {
	var sequence int64
	if err := tx.WithContext(ctx).Raw("SELECT COALESCE(MAX(seq), 0) + 1 FROM meeting_events WHERE meeting_id = ?", meetingID).Scan(&sequence).Error; err != nil {
		return 0, fmt.Errorf("分配补偿事件序号失败：%w", err)
	}
	return sequence, nil
}
