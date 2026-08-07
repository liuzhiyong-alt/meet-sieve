package minutes

import (
	"context"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// GetSettings 读取会议纪要要求和设置修订时间。
func (repository *Repository) GetSettings(ctx context.Context) (models.Settings, error) {
	if repository == nil || repository.reader == nil {
		return models.Settings{}, fmt.Errorf("读取会议纪要设置：Repository 不可用")
	}
	var settings models.Settings
	err := repository.reader.WithContext(ctx).
		Select("id", "singleton_key", "minute_prompt", "updated_at").
		Where("singleton_key = 1").Take(&settings).Error
	if err != nil {
		return models.Settings{}, fmt.Errorf("读取会议纪要设置失败：%w", err)
	}
	return settings, nil
}

// UpdateMinutePrompt 原子保存会议纪要要求，不修改其他设置分类。
func (repository *Repository) UpdateMinutePrompt(ctx context.Context, prompt string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || prompt == "" {
		return fmt.Errorf("保存会议纪要设置：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.Settings{}).
			Where("singleton_key = 1").
			Updates(map[string]any{"minute_prompt": prompt, "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("保存会议纪要设置失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("保存会议纪要设置失败：settings singleton 不存在")
		}
		return nil
	})
}

// ResetMinutePrompt 清除自定义要求，使后续生成重新使用应用内置默认值。
func (repository *Repository) ResetMinutePrompt(ctx context.Context, updatedAt int64) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("恢复默认会议纪要设置：Repository 不可用")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.Settings{}).
			Where("singleton_key = 1").
			Updates(map[string]any{"minute_prompt": nil, "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("恢复默认会议纪要设置失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("恢复默认会议纪要设置失败：settings singleton 不存在")
		}
		return nil
	})
}
