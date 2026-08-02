package transcript

import (
	"context"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// FailActiveSessions 把崩溃遗留物理连接收敛为 failed，并返回已持久 final 的最大边界。
func (repository *Repository) FailActiveSessions(ctx context.Context, tx *gorm.DB, meetingID string, endedAt int64) (int64, error) {
	if tx == nil || meetingID == "" {
		return 0, fmt.Errorf("恢复 ASR sessions：参数无效")
	}
	var lastFinal int64
	if err := tx.WithContext(ctx).Model(&models.ASRSession{}).Where("meeting_id = ?", meetingID).Select("COALESCE(MAX(last_final_sample), 0)").Scan(&lastFinal).Error; err != nil {
		return 0, fmt.Errorf("读取恢复 final 边界失败：%w", err)
	}
	errorCode := "ASR_STREAM_INTERRUPTED"
	if err := tx.WithContext(ctx).Model(&models.ASRSession{}).Where("meeting_id = ? AND state IN ?", meetingID, []string{"connecting", "streaming", "disconnected"}).Updates(map[string]any{"state": "failed", "ended_at": endedAt, "last_error_code": errorCode, "updated_at": endedAt}).Error; err != nil {
		return 0, fmt.Errorf("收敛遗留 ASR session 失败：%w", err)
	}
	if err := repository.UpdateMeetingASRState(ctx, tx, meetingID, "unavailable", endedAt); err != nil {
		return 0, err
	}
	return lastFinal, nil
}
