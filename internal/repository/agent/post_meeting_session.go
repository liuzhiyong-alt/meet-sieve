package agent

import (
	"context"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// BeginPostMeetingSession 为已保存会议创建可恢复本地 session，不发起 provider 请求。
func (repository *Repository) BeginPostMeetingSession(ctx context.Context, session models.AgentSession) error {
	if repository == nil || repository.transactions == nil || session.ID == "" || session.MeetingID == "" || session.State != "starting" {
		return fmt.Errorf("创建会后 Codex session：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var count int64
		if err := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ? AND lifecycle_state IN ? AND local_save_state = 'saved'", session.MeetingID, []string{"ended", "interrupted"}).Count(&count).Error; err != nil || count != 1 {
			return ErrConflict
		}
		if err := tx.WithContext(ctx).Select(sessionColumns()).Create(&session).Error; err != nil {
			return mapWriteError("创建会后 Codex session", err)
		}
		return nil
	})
}

// ActivatePostMeetingSession 把已取得 thread 的会后 session 切为 available。
func (repository *Repository) ActivatePostMeetingSession(ctx context.Context, sessionID string, updatedAt int64) error {
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var session models.AgentSession
		if err := tx.WithContext(ctx).Select(sessionColumns()).Where("id = ? AND state = 'starting' AND thread_id IS NOT NULL", sessionID).Take(&session).Error; err != nil {
			return ErrConflict
		}
		if result := tx.WithContext(ctx).Model(&models.AgentSession{}).Where("id = ? AND state = 'starting'", sessionID).Updates(map[string]any{"state": "available", "updated_at": updatedAt}); result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
		return tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ?", session.MeetingID).Updates(map[string]any{"agent_state": "available", "updated_at": updatedAt}).Error
	})
}

// ReopenPostMeetingSession 恢复同一逻辑 session，以复用原 batch 与幂等键。
func (repository *Repository) ReopenPostMeetingSession(ctx context.Context, sessionID string, threadID string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || sessionID == "" || threadID == "" {
		return fmt.Errorf("恢复会后 Codex session：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var session models.AgentSession
		if err := tx.WithContext(ctx).Select(sessionColumns()).Where("id = ? AND state IN ?", sessionID, []string{"ended", "failed"}).Take(&session).Error; err != nil {
			return ErrConflict
		}
		result := tx.WithContext(ctx).Model(&models.AgentSession{}).Where("id = ? AND state IN ?", sessionID, []string{"ended", "failed"}).Updates(map[string]any{"state": "available", "thread_id": threadID, "ended_at": nil, "last_error_code": nil, "updated_at": updatedAt})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
		return tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ?", session.MeetingID).Updates(map[string]any{"agent_state": "available", "updated_at": updatedAt}).Error
	})
}
