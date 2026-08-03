package agent

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// WakeFinal 是句首唤醒判断所需的持久 final 最小投影。
type WakeFinal struct {
	UtteranceID string
	MeetingID   string
	Text        string
}

// GetWakeFinal 只读取已经持久化的 ASR final，不接受 partial 文本。
func (repository *Repository) GetWakeFinal(ctx context.Context, utteranceID string) (WakeFinal, error) {
	if repository == nil || repository.reader == nil || utteranceID == "" {
		return WakeFinal{}, fmt.Errorf("读取唤醒 final：参数无效")
	}
	var final WakeFinal
	err := repository.reader.WithContext(ctx).Model(&models.Utterance{}).
		Select("id AS utterance_id", "meeting_id", "current_text AS text").Where("id = ?", utteranceID).Take(&final).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WakeFinal{}, ErrNotFound
	}
	if err != nil {
		return WakeFinal{}, fmt.Errorf("读取唤醒 final 失败：%w", err)
	}
	return final, nil
}

// HasActiveMeeting 返回是否有会议占用麦克风或活动业务状态。
func (repository *Repository) HasActiveMeeting(ctx context.Context) (bool, error) {
	if repository == nil || repository.reader == nil {
		return false, fmt.Errorf("检查活动会议：Repository 不可用")
	}
	var count int64
	if err := repository.reader.WithContext(ctx).Model(&models.Meeting{}).
		Where("lifecycle_state IN ?", []string{"preparing", "recording", "finalizing"}).Limit(1).Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查活动会议失败：%w", err)
	}
	return count > 0, nil
}

// GetDefaultMicrophoneID 返回设置中的设备 ID；空值由服务回退系统默认设备。
func (repository *Repository) GetDefaultMicrophoneID(ctx context.Context) (string, error) {
	if repository == nil || repository.reader == nil {
		return "", fmt.Errorf("读取默认麦克风：Repository 不可用")
	}
	var settings models.Settings
	if err := repository.reader.WithContext(ctx).Select("default_microphone_id").Where("singleton_key = 1").Take(&settings).Error; err != nil {
		return "", fmt.Errorf("读取默认麦克风失败：%w", err)
	}
	if settings.DefaultMicrophoneID == nil {
		return "", nil
	}
	return *settings.DefaultMicrophoneID, nil
}
