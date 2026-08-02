package transcript

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// GetSettings 返回 settings singleton 的显式字段投影。
func (repository *Repository) GetSettings(ctx context.Context) (models.Settings, error) {
	if repository == nil || repository.reader == nil {
		return models.Settings{}, fmt.Errorf("读取 ASR 设置：Repository 未初始化")
	}
	var settings models.Settings
	err := repository.reader.WithContext(ctx).Select(settingsColumns()).Where("singleton_key = 1").Take(&settings).Error
	if err != nil {
		return models.Settings{}, fmt.Errorf("读取 ASR 设置失败：%w", err)
	}
	return settings, nil
}

// GetSettingsForUpdate 在调用方事务中读取 settings singleton。
func (repository *Repository) GetSettingsForUpdate(ctx context.Context, tx *gorm.DB) (models.Settings, error) {
	if tx == nil {
		return models.Settings{}, fmt.Errorf("事务内读取 ASR 设置：事务不能为空")
	}
	var settings models.Settings
	err := tx.WithContext(ctx).Select(settingsColumns()).Where("singleton_key = 1").Take(&settings).Error
	if err != nil {
		return models.Settings{}, fmt.Errorf("事务内读取 ASR 设置失败：%w", err)
	}
	return settings, nil
}

// HasActiveMeeting 返回是否存在禁止修改凭据的活动会议。
func (repository *Repository) HasActiveMeeting(ctx context.Context, tx *gorm.DB) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("检查活动会议：事务不能为空")
	}
	var meeting models.Meeting
	err := tx.WithContext(ctx).Select("id").Where("lifecycle_state IN ?", []string{"preparing", "recording", "finalizing"}).Limit(1).Take(&meeting).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("检查活动会议失败：%w", err)
}

// UpdateASRSettings 只更新当前 Step 的四个凭据字段和审计时间。
func (repository *Repository) UpdateASRSettings(ctx context.Context, tx *gorm.DB, settings models.Settings) error {
	if tx == nil || settings.ID == "" {
		return fmt.Errorf("更新 ASR 设置：参数无效")
	}
	result := tx.WithContext(ctx).Model(&models.Settings{}).Where("id = ? AND singleton_key = 1", settings.ID).Updates(map[string]any{
		"volc_auth_mode": settings.VolcAuthMode, "volc_api_app_key": settings.VolcAPIAppKey,
		"volc_api_access_key": settings.VolcAPIAccessKey, "volc_api_key": settings.VolcAPIKey,
		"updated_at": settings.UpdatedAt,
	})
	if result.Error != nil {
		return fmt.Errorf("更新 ASR 设置失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("更新 ASR 设置失败：settings singleton 不存在")
	}
	return nil
}

func settingsColumns() []string {
	return []string{"id", "singleton_key", "volc_auth_mode", "volc_api_app_key", "volc_api_access_key", "volc_api_key", "default_microphone_id", "wake_word", "codex_executable_path", "created_at", "updated_at"}
}
