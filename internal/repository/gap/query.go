package gap

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// ListEligibleGaps 返回自动处理可领取的 pending/failed gap。
func (repository *Repository) ListEligibleGaps(ctx context.Context, meetingID string) ([]models.ASRGap, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取待补转写 gap：参数无效")
	}
	var gaps []models.ASRGap
	if err := repository.reader.WithContext(ctx).Where("meeting_id = ? AND state IN ?", meetingID, []string{"pending", "failed"}).Order("start_sample ASC").Order("end_sample ASC").Order("id ASC").Find(&gaps).Error; err != nil {
		return nil, fmt.Errorf("读取待补转写 gap 失败：%w", err)
	}
	return gaps, nil
}

// GetAttempt 返回一次补转写尝试。
func (repository *Repository) GetAttempt(ctx context.Context, attemptID string) (models.GapTranscriptionAttempt, error) {
	if repository == nil || repository.reader == nil || attemptID == "" {
		return models.GapTranscriptionAttempt{}, fmt.Errorf("读取补转写 attempt：参数无效")
	}
	var attempt models.GapTranscriptionAttempt
	if err := repository.reader.WithContext(ctx).Select(attemptColumns()).Where("id = ?", attemptID).Take(&attempt).Error; err != nil {
		return models.GapTranscriptionAttempt{}, fmt.Errorf("读取补转写 attempt 失败：%w", err)
	}
	return attempt, nil
}

// GetAttemptByProviderRequest 返回显式请求 ID 已创建的 attempt。
func (repository *Repository) GetAttemptByProviderRequest(ctx context.Context, meetingID string, requestID string) (models.GapTranscriptionAttempt, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || requestID == "" {
		return models.GapTranscriptionAttempt{}, fmt.Errorf("读取补转写幂等请求：参数无效")
	}
	var attempt models.GapTranscriptionAttempt
	err := repository.reader.WithContext(ctx).Select(attemptColumns()).Where("meeting_id = ? AND provider_request_id = ?", meetingID, requestID).Take(&attempt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.GapTranscriptionAttempt{}, ErrConflict
	}
	if err != nil {
		return models.GapTranscriptionAttempt{}, fmt.Errorf("读取补转写幂等请求失败：%w", err)
	}
	return attempt, nil
}

// GetReadyGapAsset 返回指定会议仍可用于冲突回放的派生音频事实。
func (repository *Repository) GetReadyGapAsset(ctx context.Context, meetingID string, assetID string) (models.AudioAsset, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || assetID == "" {
		return models.AudioAsset{}, fmt.Errorf("读取 gap 回放音频：参数无效")
	}
	var asset models.AudioAsset
	err := repository.reader.WithContext(ctx).
		Where("id = ? AND meeting_id = ? AND kind = 'gap' AND state = 'ready'", assetID, meetingID).
		Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AudioAsset{}, ErrConflict
	}
	if err != nil {
		return models.AudioAsset{}, fmt.Errorf("读取 gap 回放音频失败：%w", err)
	}
	return asset, nil
}

// StateRecord 是页面重载可从 SQLite 重建的 gap 最小事实。
type StateRecord struct {
	Aggregate string
	Gaps      []models.ASRGap
	Attempt   *models.GapTranscriptionAttempt
}

// ReadState 返回 gap 聚合、明细和当前活动 attempt。
func (repository *Repository) ReadState(ctx context.Context, meetingID string) (StateRecord, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return StateRecord{}, fmt.Errorf("读取 gap 状态：参数无效")
	}
	var meeting models.Meeting
	if err := repository.reader.WithContext(ctx).Select("id", "gap_state").Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		return StateRecord{}, fmt.Errorf("读取 gap 聚合状态失败：%w", err)
	}
	var gaps []models.ASRGap
	if err := repository.reader.WithContext(ctx).Where("meeting_id = ?", meetingID).Order("start_sample ASC").Order("end_sample ASC").Order("id ASC").Find(&gaps).Error; err != nil {
		return StateRecord{}, fmt.Errorf("读取 gap 明细失败：%w", err)
	}
	var attempt models.GapTranscriptionAttempt
	err := repository.reader.WithContext(ctx).Select(attemptColumns()).Where("meeting_id = ? AND state IN ?", meetingID, []string{"pending", "running"}).Take(&attempt).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return StateRecord{}, fmt.Errorf("读取活动补转写失败：%w", err)
	}
	result := StateRecord{Aggregate: meeting.GapState, Gaps: gaps}
	if err == nil {
		result.Attempt = &attempt
	}
	return result, nil
}

// GetMeeting 返回补转写所需的可信会议目录与状态。
func (repository *Repository) GetMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.Meeting{}, fmt.Errorf("读取补转写会议：参数无效")
	}
	var meeting models.Meeting
	if err := repository.reader.WithContext(ctx).Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		return models.Meeting{}, fmt.Errorf("读取补转写会议失败：%w", err)
	}
	return meeting, nil
}

// ListSourceAudio 返回优先 mixed、其次 microphone 的 ready 音频事实。
func (repository *Repository) ListSourceAudio(ctx context.Context, meetingID string) ([]models.AudioAsset, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取补转写源音频：参数无效")
	}
	var mixed []models.AudioAsset
	if err := repository.reader.WithContext(ctx).Where("meeting_id = ? AND kind = 'mixed' AND state = 'ready'", meetingID).Order("sequence_no ASC").Find(&mixed).Error; err != nil {
		return nil, err
	}
	if len(mixed) > 0 {
		return mixed, nil
	}
	var microphones []models.AudioAsset
	if err := repository.reader.WithContext(ctx).Where("meeting_id = ? AND kind = 'microphone' AND state = 'ready'", meetingID).Order("sequence_no ASC").Find(&microphones).Error; err != nil {
		return nil, err
	}
	return microphones, nil
}

// RegisterGapAsset 在文件完成后分配会议内 gap 序号并登记 ready 资产。
func (repository *Repository) RegisterGapAsset(ctx context.Context, asset models.AudioAsset) (models.AudioAsset, error) {
	if repository == nil || repository.transactions == nil || asset.ID == "" || asset.MeetingID == "" || asset.Kind != "gap" || asset.State != "ready" {
		return models.AudioAsset{}, fmt.Errorf("登记 gap 音频：参数无效")
	}
	err := repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var sequence int
		if err := tx.WithContext(ctx).Raw("SELECT COALESCE(MAX(sequence_no),0)+1 FROM audio_assets WHERE meeting_id=? AND kind='gap'", asset.MeetingID).Scan(&sequence).Error; err != nil {
			return fmt.Errorf("分配 gap 音频序号失败：%w", err)
		}
		asset.SequenceNo = sequence
		if err := tx.WithContext(ctx).Create(&asset).Error; err != nil {
			return fmt.Errorf("登记 gap 音频失败：%w", err)
		}
		return nil
	})
	return asset, err
}

// MarkGapAssetDeleted 在派生文件清理后保留元数据审计并标记 deleted。
func (repository *Repository) MarkGapAssetDeleted(ctx context.Context, assetID string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || assetID == "" {
		return fmt.Errorf("清理 gap 音频：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.AudioAsset{}).Where("id = ? AND kind = 'gap' AND state = 'ready'", assetID).Updates(map[string]any{"state": "deleted", "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("标记 gap 音频已删除失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// HasActiveAttempt 判断 processor 是否应等待当前 owner。
func (repository *Repository) HasActiveAttempt(ctx context.Context, meetingID string) (bool, error) {
	var count int64
	err := repository.reader.WithContext(ctx).Model(&models.GapTranscriptionAttempt{}).Where("meeting_id = ? AND state IN ?", meetingID, []string{"pending", "running"}).Count(&count).Error
	return count > 0, err
}

// GetGap 返回冲突解决所需的当前 gap。
func (repository *Repository) GetGap(ctx context.Context, meetingID string, gapID string) (models.ASRGap, error) {
	var gap models.ASRGap
	err := repository.reader.WithContext(ctx).Where("id = ? AND meeting_id = ?", gapID, meetingID).Take(&gap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ASRGap{}, ErrConflict
	}
	return gap, err
}
