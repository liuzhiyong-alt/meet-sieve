package meeting

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// GetMediaPause 返回指定 turn 的媒体暂停事实。
func (repository *Repository) GetMediaPause(ctx context.Context, turnID string) (models.MeetingMediaPause, error) {
	if repository == nil || repository.reader == nil || turnID == "" {
		return models.MeetingMediaPause{}, fmt.Errorf("读取媒体暂停事实：参数无效")
	}
	var pause models.MeetingMediaPause
	err := repository.reader.WithContext(ctx).Where("agent_turn_id = ?", turnID).Take(&pause).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.MeetingMediaPause{}, fmt.Errorf("媒体暂停事实不存在")
	}
	if err != nil {
		return models.MeetingMediaPause{}, fmt.Errorf("读取媒体暂停事实失败：%w", err)
	}
	return pause, nil
}

// HasActiveMediaPause 判断会议是否仍有正在建立、已生效或正在恢复的 AI 转写暂停。
func (repository *Repository) HasActiveMediaPause(ctx context.Context, meetingID string) (bool, error) {
	if repository == nil || repository.reader == nil || ctx == nil || meetingID == "" {
		return false, fmt.Errorf("读取活动媒体暂停：参数无效")
	}
	var count int64
	err := repository.reader.WithContext(ctx).Model(&models.MeetingMediaPause{}).
		Where("meeting_id = ? AND state IN ?", meetingID, []string{"pausing", "paused", "resuming"}).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("读取活动媒体暂停失败：%w", err)
	}
	return count > 0, nil
}

// CreateMediaPause 创建语音 AI turn 唯一的 pausing 事实。
func (repository *Repository) CreateMediaPause(ctx context.Context, tx *gorm.DB, pause models.MeetingMediaPause) error {
	if tx == nil || pause.ID == "" || pause.MeetingID == "" || pause.AgentTurnID == "" {
		return fmt.Errorf("创建媒体暂停事实：参数无效")
	}
	if err := tx.WithContext(ctx).Create(&pause).Error; err != nil {
		return fmt.Errorf("创建媒体暂停事实失败：%w", err)
	}
	return nil
}

// MarkMediaPaused 写入 runner 已确认的录音暂停边界。
func (repository *Repository) MarkMediaPaused(ctx context.Context, tx *gorm.DB, turnID string, logicalSample int64, physicalSample int64, updatedAt int64) error {
	if tx == nil || turnID == "" || logicalSample < 0 || physicalSample < 0 {
		return fmt.Errorf("确认媒体暂停：参数无效")
	}
	result := tx.WithContext(ctx).Model(&models.MeetingMediaPause{}).
		Where("agent_turn_id = ? AND state = 'pausing'", turnID).
		Updates(map[string]any{
			"state": "paused", "logical_sample": logicalSample,
			"physical_start_sample": physicalSample, "updated_at": updatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("确认媒体暂停失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("确认媒体暂停状态冲突")
	}
	return nil
}

// CompleteMediaPause 写入恢复边界和暂停期累计丢弃样本。
func (repository *Repository) CompleteMediaPause(ctx context.Context, tx *gorm.DB, turnID string, physicalEnd int64, discarded int64, endedAt int64) error {
	if tx == nil || turnID == "" || physicalEnd < 0 || discarded < 0 {
		return fmt.Errorf("完成媒体暂停：参数无效")
	}
	result := tx.WithContext(ctx).Model(&models.MeetingMediaPause{}).
		Where("agent_turn_id = ? AND state IN ?", turnID, []string{"paused", "resuming"}).
		Updates(map[string]any{
			"state": "completed", "physical_end_sample": physicalEnd,
			"discarded_samples": discarded, "ended_at": endedAt, "updated_at": endedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("完成媒体暂停失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		var pause models.MeetingMediaPause
		if err := tx.WithContext(ctx).Select("state").Where("agent_turn_id = ?", turnID).Take(&pause).Error; err == nil && pause.State == "completed" {
			return nil
		}
		return fmt.Errorf("完成媒体暂停状态冲突")
	}
	return nil
}

// FailMediaPause 把尚未完成的暂停事实收口为失败并保留稳定错误码。
func (repository *Repository) FailMediaPause(ctx context.Context, tx *gorm.DB, turnID string, errorCode string, endedAt int64) error {
	if tx == nil || turnID == "" || errorCode == "" {
		return fmt.Errorf("标记媒体暂停失败：参数无效")
	}
	return tx.WithContext(ctx).Model(&models.MeetingMediaPause{}).
		Where("agent_turn_id = ? AND state <> 'completed'", turnID).
		Updates(map[string]any{"state": "failed", "last_error_code": errorCode, "ended_at": endedAt, "updated_at": endedAt}).Error
}
