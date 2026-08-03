// Package guest 负责访客 session 持久化，不保存原始 token。
package guest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"meet-sieve/internal/infra/database"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// Repository 使用 reader 查询、TransactionManager 提交单 writer 短事务。
type Repository struct {
	reader       *gorm.DB
	transactions *database.TransactionManager
}

// NewRepository 创建访客 session Repository。
func NewRepository(reader *gorm.DB, transactions *database.TransactionManager) *Repository {
	return &Repository{reader: reader, transactions: transactions}
}

// CreateSession 在短事务中写入只含 token hash 的访客会话。
func (repository *Repository) CreateSession(ctx context.Context, session models.GuestSession) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("创建访客会话：Repository 不可用")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Select(sessionColumns()).Create(&session).Error; err != nil {
			return fmt.Errorf("写入访客会话失败：%w", err)
		}
		return nil
	})
}

// GetMeeting 返回构建 Guest 安全投影所需的最小会议事实。
func (repository *Repository) GetMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.Meeting{}, fmt.Errorf("读取访客会议：参数无效")
	}
	var meeting models.Meeting
	err := repository.reader.WithContext(ctx).
		Select("id", "subject", "started_at", "lifecycle_state", "lan_state").
		Where("id = ?", meetingID).Take(&meeting).Error
	if err != nil {
		return models.Meeting{}, fmt.Errorf("读取访客会议失败：%w", err)
	}
	return meeting, nil
}

// FindActiveByTokenHash 按 SHA-256 查询活动 session，不接受原始 token。
func (repository *Repository) FindActiveByTokenHash(ctx context.Context, tokenHash string) (*models.GuestSession, error) {
	if repository == nil || repository.reader == nil || tokenHash == "" {
		return nil, fmt.Errorf("查询访客会话：参数无效")
	}
	var session models.GuestSession
	err := repository.reader.WithContext(ctx).Select(sessionColumns()).
		Where("session_token_hash = ? AND state = 'active'", tokenHash).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询访客会话失败：%w", err)
	}
	return &session, nil
}

// MarkExpired 幂等把超时的活动 session 标记为 expired。
func (repository *Repository) MarkExpired(ctx context.Context, sessionID string, updatedAt int64) error {
	return repository.updateActiveState(ctx, sessionID, "expired", updatedAt)
}

// RevokeMeeting 幂等撤销指定会议的全部活动 session。
func (repository *Repository) RevokeMeeting(ctx context.Context, meetingID string) error {
	if repository == nil || repository.transactions == nil || meetingID == "" {
		return fmt.Errorf("撤销访客会话：参数无效")
	}
	now := time.Now().UnixMilli()
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.GuestSession{}).
			Where("meeting_id = ? AND state = 'active'", meetingID).
			Updates(map[string]any{"state": "revoked", "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("撤销访客会话失败：%w", result.Error)
		}
		return nil
	})
}

// RevokeAllActive 在应用恢复时撤销上一进程遗留的所有活动 session。
func (repository *Repository) RevokeAllActive(ctx context.Context) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("恢复撤销访客会话：Repository 不可用")
	}
	now := time.Now().UnixMilli()
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.GuestSession{}).Where("state = 'active'").
			Updates(map[string]any{"state": "revoked", "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("恢复撤销访客会话失败：%w", result.Error)
		}
		return nil
	})
}

// TouchLastSeen 仅在上次写入早于节流阈值时更新在线时间。
func (repository *Repository) TouchLastSeen(ctx context.Context, sessionID string, now int64, threshold int64) error {
	if repository == nil || repository.transactions == nil || sessionID == "" {
		return fmt.Errorf("更新访客在线时间：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.GuestSession{}).
			Where("id = ? AND state = 'active'", sessionID).
			Where("last_seen_at IS NULL OR last_seen_at <= ?", threshold).
			Updates(map[string]any{"last_seen_at": now, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("更新访客在线时间失败：%w", result.Error)
		}
		return nil
	})
}

// updateActiveState 只转换当前 active session，保持重复操作幂等。
func (repository *Repository) updateActiveState(ctx context.Context, sessionID string, state string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || sessionID == "" {
		return fmt.Errorf("更新访客会话状态：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.GuestSession{}).Where("id = ? AND state = 'active'", sessionID).
			Updates(map[string]any{"state": state, "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("更新访客会话状态失败：%w", result.Error)
		}
		return nil
	})
}

// sessionColumns 返回 guest_sessions 的显式字段列表。
func sessionColumns() []string {
	return []string{
		"id", "meeting_id", "display_name", "session_token_hash", "state",
		"expires_at", "last_seen_at", "created_at", "updated_at",
	}
}
