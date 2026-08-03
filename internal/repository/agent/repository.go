// Package agent 按明确查询语义持久化智能体 session、turn、batch 和滚动快照。
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/internal/infra/database"
	"meet-sieve/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrNotFound 表示指定的智能体事实不存在。
	ErrNotFound = errors.New("智能体事实不存在")
	// ErrConflict 表示活动唯一约束或状态比较更新冲突。
	ErrConflict = errors.New("智能体状态冲突")
)

// Repository 使用 reader 查询，并通过 TransactionManager 执行短写事务。
type Repository struct {
	reader       *gorm.DB
	transactions *database.TransactionManager
}

// NewRepository 创建智能体事实 Repository。
func NewRepository(reader *gorm.DB, transactions *database.TransactionManager) *Repository {
	return &Repository{reader: reader, transactions: transactions}
}

// CreateSession 创建一个本地 session；活动唯一冲突返回 ErrConflict。
func (repository *Repository) CreateSession(ctx context.Context, session models.AgentSession) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("创建智能体会话：Repository 不可用")
	}
	return repository.write(ctx, "创建智能体会话", func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Select(sessionColumns()).Create(&session).Error
	})
}

// GetSession 按本地 ID 返回一个 session，不存在时返回 ErrNotFound。
func (repository *Repository) GetSession(ctx context.Context, sessionID string) (models.AgentSession, error) {
	if repository == nil || repository.reader == nil || sessionID == "" {
		return models.AgentSession{}, fmt.Errorf("读取智能体会话：参数无效")
	}
	var session models.AgentSession
	err := repository.reader.WithContext(ctx).Select(sessionColumns()).Where("id = ?", sessionID).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AgentSession{}, ErrNotFound
	}
	if err != nil {
		return models.AgentSession{}, fmt.Errorf("读取智能体会话失败：%w", err)
	}
	return session, nil
}

// GetActiveSessionByMeeting 返回会议唯一 starting/available session。
func (repository *Repository) GetActiveSessionByMeeting(ctx context.Context, meetingID string) (models.AgentSession, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.AgentSession{}, fmt.Errorf("读取活动智能体会话：参数无效")
	}
	var session models.AgentSession
	err := repository.reader.WithContext(ctx).Select(sessionColumns()).
		Where("meeting_id = ? AND state IN ?", meetingID, []string{"starting", "available"}).
		Order("created_at DESC").Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AgentSession{}, ErrNotFound
	}
	if err != nil {
		return models.AgentSession{}, fmt.Errorf("读取活动智能体会话失败：%w", err)
	}
	return session, nil
}

// UpdateSessionState 比较来源状态后更新 thread、终态和错误信息。
func (repository *Repository) UpdateSessionState(ctx context.Context, sessionID string, fromStates []string, updates map[string]any) error {
	if repository == nil || repository.transactions == nil || sessionID == "" || len(fromStates) == 0 || len(updates) == 0 {
		return fmt.Errorf("更新智能体会话：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.AgentSession{}).
			Where("id = ? AND state IN ?", sessionID, fromStates).Updates(updates)
		if result.Error != nil {
			return mapWriteError("更新智能体会话", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// CreateTurn 创建一个明确用途的工作单元。
func (repository *Repository) CreateTurn(ctx context.Context, turn models.AgentTurn) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("创建智能体 turn：Repository 不可用")
	}
	return repository.write(ctx, "创建智能体 turn", func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Select(turnColumns()).Create(&turn).Error
	})
}

// GetTurn 按本地 ID 返回一个 turn。
func (repository *Repository) GetTurn(ctx context.Context, turnID string) (models.AgentTurn, error) {
	if repository == nil || repository.reader == nil || turnID == "" {
		return models.AgentTurn{}, fmt.Errorf("读取智能体 turn：参数无效")
	}
	var turn models.AgentTurn
	err := repository.reader.WithContext(ctx).Select(turnColumns()).Where("id = ?", turnID).Take(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AgentTurn{}, ErrNotFound
	}
	if err != nil {
		return models.AgentTurn{}, fmt.Errorf("读取智能体 turn 失败：%w", err)
	}
	return turn, nil
}

// GetActiveTurnByMeeting 返回页面重载时唯一 pending/running turn。
func (repository *Repository) GetActiveTurnByMeeting(ctx context.Context, meetingID string) (models.AgentTurn, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.AgentTurn{}, fmt.Errorf("读取活动智能体 turn：参数无效")
	}
	var turn models.AgentTurn
	err := repository.reader.WithContext(ctx).Select(turnColumns()).
		Where("meeting_id = ? AND state IN ?", meetingID, []string{"pending", "running"}).
		Order("created_at DESC").Take(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AgentTurn{}, ErrNotFound
	}
	if err != nil {
		return models.AgentTurn{}, fmt.Errorf("读取活动智能体 turn 失败：%w", err)
	}
	return turn, nil
}

// FindTurnByIdempotency 查找同一 session 已创建的工作单元。
func (repository *Repository) FindTurnByIdempotency(ctx context.Context, sessionID string, key string) (models.AgentTurn, error) {
	if repository == nil || repository.reader == nil || sessionID == "" || key == "" {
		return models.AgentTurn{}, fmt.Errorf("读取幂等 turn：参数无效")
	}
	var turn models.AgentTurn
	err := repository.reader.WithContext(ctx).Select(turnColumns()).
		Where("agent_session_id = ? AND idempotency_key = ?", sessionID, key).Take(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AgentTurn{}, ErrNotFound
	}
	if err != nil {
		return models.AgentTurn{}, fmt.Errorf("读取幂等 turn 失败：%w", err)
	}
	return turn, nil
}

// CreateBatch 创建连续上下文同步批次。
func (repository *Repository) CreateBatch(ctx context.Context, batch models.SyncBatch) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("创建同步批次：Repository 不可用")
	}
	return repository.write(ctx, "创建同步批次", func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Select(batchColumns()).Create(&batch).Error
	})
}

// ListBatches 按序列范围返回 session 的批次，不读取未声明字段。
func (repository *Repository) ListBatches(ctx context.Context, sessionID string) ([]models.SyncBatch, error) {
	if repository == nil || repository.reader == nil || sessionID == "" {
		return nil, fmt.Errorf("读取同步批次：参数无效")
	}
	var batches []models.SyncBatch
	if err := repository.reader.WithContext(ctx).Select(batchColumns()).Where("agent_session_id = ?", sessionID).
		Order("from_seq ASC").Find(&batches).Error; err != nil {
		return nil, fmt.Errorf("读取同步批次失败：%w", err)
	}
	return batches, nil
}

// UpsertSnapshot 按 session 原子插入或覆盖唯一滚动快照。
func (repository *Repository) UpsertSnapshot(ctx context.Context, snapshot models.ContextSnapshot) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("保存滚动快照：Repository 不可用")
	}
	return repository.write(ctx, "保存滚动快照", func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Select(snapshotColumns()).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "agent_session_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"agent_turn_id", "through_seq", "content_json", "content_sha256", "updated_at",
			}),
		}).Create(&snapshot).Error
	})
}

// GetSnapshot 返回 session 当前唯一滚动快照。
func (repository *Repository) GetSnapshot(ctx context.Context, sessionID string) (models.ContextSnapshot, error) {
	if repository == nil || repository.reader == nil || sessionID == "" {
		return models.ContextSnapshot{}, fmt.Errorf("读取滚动快照：参数无效")
	}
	var snapshot models.ContextSnapshot
	err := repository.reader.WithContext(ctx).Select(snapshotColumns()).
		Where("agent_session_id = ?", sessionID).Take(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ContextSnapshot{}, ErrNotFound
	}
	if err != nil {
		return models.ContextSnapshot{}, fmt.Errorf("读取滚动快照失败：%w", err)
	}
	return snapshot, nil
}

// write 在单 writer 短事务中执行单项事实写入并统一映射约束错误。
func (repository *Repository) write(ctx context.Context, operation string, fn func(*gorm.DB) error) error {
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return mapWriteError(operation, fn(tx))
	})
}

// mapWriteError 将 SQLite 唯一约束转换为稳定冲突语义。
func mapWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
		return fmt.Errorf("%s：%w", operation, ErrConflict)
	}
	return fmt.Errorf("%s失败：%w", operation, err)
}

// sessionColumns 返回 agent_sessions 的完整显式列。
func sessionColumns() []string {
	return []string{"id", "meeting_id", "provider", "thread_id", "cwd_relative_path", "state", "resumed_from_session_id", "started_at", "ended_at", "last_error_code", "created_at", "updated_at"}
}

// turnColumns 返回 agent_turns 的完整显式列。
func turnColumns() []string {
	return []string{"id", "meeting_id", "agent_session_id", "provider_turn_id", "kind", "state", "idempotency_key", "question_event_id", "answer_event_id", "started_at", "ended_at", "last_error_code", "created_at", "updated_at"}
}

// batchColumns 返回 sync_batches 的完整显式列。
func batchColumns() []string {
	return []string{"id", "meeting_id", "agent_session_id", "from_seq", "to_seq", "idempotency_key", "state", "attempt_count", "last_error_code", "created_at", "updated_at"}
}

// snapshotColumns 返回 context_snapshots 的完整显式列。
func snapshotColumns() []string {
	return []string{"id", "meeting_id", "agent_session_id", "agent_turn_id", "through_seq", "content_json", "content_sha256", "created_at", "updated_at"}
}
