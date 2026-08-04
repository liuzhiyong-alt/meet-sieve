// Package audio 提供录音设备设置与真实设备测试。
package audio

import (
	"context"
	"fmt"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/port"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// Settings 是录音分类独立加载和保存的投影。
type Settings struct {
	DefaultMicrophoneID string
	Revision            int64
	Devices             []port.InputDevice
}

// SettingsService 持久化默认麦克风，并使用同一真实 capture 做设备测试。
type SettingsService struct {
	reader       *gorm.DB
	transactions *database.TransactionManager
	capture      port.AudioCapture
	now          func() time.Time
}

// NewSettingsService 创建录音设置服务。
func NewSettingsService(reader *gorm.DB, transactions *database.TransactionManager, capture port.AudioCapture) *SettingsService {
	return &SettingsService{reader: reader, transactions: transactions, capture: capture, now: time.Now}
}

// Get 返回当前默认设备和系统实时设备列表。
func (service *SettingsService) Get(ctx context.Context) (Settings, error) {
	if service == nil || service.reader == nil || service.capture == nil {
		return Settings{}, fmt.Errorf("录音设置服务不可用")
	}
	var row models.Settings
	if err := service.reader.WithContext(ctx).Select("default_microphone_id", "updated_at").Where("singleton_key = 1").Take(&row).Error; err != nil {
		return Settings{}, err
	}
	devices, err := service.capture.ListInputDevices(ctx)
	if err != nil {
		return Settings{}, err
	}
	result := Settings{Revision: row.UpdatedAt, Devices: devices}
	if row.DefaultMicrophoneID != nil {
		result.DefaultMicrophoneID = *row.DefaultMicrophoneID
	}
	return result, nil
}

// Save 验证设备仍存在后独立保存默认麦克风。
func (service *SettingsService) Save(ctx context.Context, deviceID string) (Settings, error) {
	current, err := service.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	valid := false
	for _, device := range current.Devices {
		if device.ID == deviceID {
			valid = true
			break
		}
	}
	if !valid {
		return Settings{}, apperr.Biz(apperr.CodeVoiceDeviceUnavailable, apperr.WithOp("audio.settings.device"))
	}
	if service.transactions == nil {
		return Settings{}, fmt.Errorf("录音设置事务不可用")
	}
	now := service.now().UnixMilli()
	err = service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var active int64
		if err := tx.WithContext(ctx).Model(&models.Meeting{}).Where("lifecycle_state IN ?", []string{"preparing", "recording", "finalizing"}).Limit(1).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return apperr.Biz(apperr.CodeWorkspaceChangeBlocked, apperr.WithOp("audio.settings.active_meeting"))
		}
		var deleting int64
		if err := tx.WithContext(ctx).Table("deletion_jobs").Where("state IN ?", []string{"pending", "running"}).Limit(1).Count(&deleting).Error; err != nil {
			return err
		}
		if deleting > 0 {
			return apperr.Biz(apperr.CodeWorkspaceChangeBlocked, apperr.WithOp("audio.settings.deleting"))
		}
		return tx.WithContext(ctx).Model(&models.Settings{}).Where("singleton_key = 1").Updates(map[string]any{"default_microphone_id": deviceID, "updated_at": now}).Error
	})
	if err != nil {
		return Settings{}, err
	}
	return service.Get(ctx)
}

// TestDevice 对指定设备执行真实短探测，不修改保存设置。
func (service *SettingsService) TestDevice(ctx context.Context, deviceID string) error {
	if service == nil || service.capture == nil || deviceID == "" {
		return apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("audio.settings.test"))
	}
	return service.capture.TestInputDevice(ctx, deviceID)
}
