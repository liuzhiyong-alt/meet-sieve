package transcript

import (
	"context"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// CreateSession 创建一个物理 ASR 连接事实，并同步会议实时转写状态。
func (repository *Repository) CreateSession(ctx context.Context, tx *gorm.DB, session models.ASRSession, meetingState string) error {
	if tx == nil || session.ID == "" || session.MeetingID == "" {
		return fmt.Errorf("创建 ASR session：参数无效")
	}
	if err := tx.WithContext(ctx).Create(&session).Error; err != nil {
		return fmt.Errorf("创建 ASR session 失败：%w", err)
	}
	return repository.UpdateMeetingASRState(ctx, tx, session.MeetingID, meetingState, session.UpdatedAt)
}

// MarkSessionStreaming 保存握手取得的 provider session，并同步会议 streaming 状态。
func (repository *Repository) MarkSessionStreaming(ctx context.Context, tx *gorm.DB, meetingID string, sessionID string, providerSessionID string, updatedAt int64) error {
	if tx == nil || meetingID == "" || sessionID == "" {
		return fmt.Errorf("更新 ASR session：参数无效")
	}
	updates := map[string]any{"state": "streaming", "updated_at": updatedAt}
	if providerSessionID != "" {
		updates["provider_session_id"] = providerSessionID
	}
	// 迟到的 provider started 事件不得把已失败或已停止的物理连接复活为 streaming。
	result := tx.WithContext(ctx).Model(&models.ASRSession{}).
		Where("id = ? AND meeting_id = ? AND state IN ?", sessionID, meetingID, []string{"connecting", "streaming"}).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("更新 ASR session streaming 失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("更新 ASR session streaming 失败：session 不存在或已终结")
	}
	return repository.UpdateMeetingASRState(ctx, tx, meetingID, "streaming", updatedAt)
}

// AdvanceSessionSentSample 只向前推进已实际写入 WebSocket 的样本边界。
func (repository *Repository) AdvanceSessionSentSample(ctx context.Context, tx *gorm.DB, meetingID string, sessionID string, sample int64, updatedAt int64) error {
	if tx == nil || meetingID == "" || sessionID == "" || sample < 0 {
		return fmt.Errorf("推进 ASR 样本进度：参数无效")
	}
	result := tx.WithContext(ctx).Model(&models.ASRSession{}).
		Where("id = ? AND meeting_id = ? AND last_sent_sample <= ?", sessionID, meetingID, sample).
		Updates(map[string]any{"last_sent_sample": sample, "updated_at": updatedAt})
	if result.Error != nil {
		return fmt.Errorf("推进 ASR 样本进度失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("推进 ASR 样本进度失败：session 不存在或样本倒退")
	}
	return nil
}

// FinishSession 终结一个物理连接，并同步会议的实时转写状态。
func (repository *Repository) FinishSession(ctx context.Context, tx *gorm.DB, meetingID string, sessionID string, sessionState string, meetingState string, errorCode *string, endedAt int64) error {
	if tx == nil || meetingID == "" || sessionID == "" {
		return fmt.Errorf("终结 ASR session：参数无效")
	}
	result := tx.WithContext(ctx).Model(&models.ASRSession{}).Where("id = ? AND meeting_id = ?", sessionID, meetingID).
		Updates(map[string]any{"state": sessionState, "ended_at": endedAt, "last_error_code": errorCode, "updated_at": endedAt})
	if result.Error != nil {
		return fmt.Errorf("终结 ASR session 失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("终结 ASR session 失败：session 不存在")
	}
	return repository.UpdateMeetingASRState(ctx, tx, meetingID, meetingState, endedAt)
}

// UpdateMeetingASRState 只更新独立实时转写状态轴。
func (repository *Repository) UpdateMeetingASRState(ctx context.Context, tx *gorm.DB, meetingID string, state string, updatedAt int64) error {
	if tx == nil || meetingID == "" {
		return fmt.Errorf("更新会议 ASR 状态：参数无效")
	}
	result := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ?", meetingID).
		Updates(map[string]any{"realtime_asr_state": state, "updated_at": updatedAt})
	if result.Error != nil {
		return fmt.Errorf("更新会议 ASR 状态失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("更新会议 ASR 状态失败：会议不存在")
	}
	return nil
}
