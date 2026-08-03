// Package minutes 以短事务维护纪要 turn、不可变版本与 current 投影。
package minutes

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/internal/infra/database"
	"meet-sieve/models"

	"gorm.io/gorm"
)

var (
	// ErrNotFound 表示目标会议、turn 或版本不存在。
	ErrNotFound = errors.New("纪要事实不存在")
	// ErrConflict 表示来源状态或版本基线已变化。
	ErrConflict = errors.New("纪要状态冲突")
)

// Repository 使用单 writer 事务维护纪要事实。
type Repository struct {
	reader       *gorm.DB
	transactions *database.TransactionManager
}

// NewRepository 创建纪要 Repository。
func NewRepository(reader *gorm.DB, transactions *database.TransactionManager) *Repository {
	return &Repository{reader: reader, transactions: transactions}
}

// BeginMinutesTurnResult 返回新建或幂等命中的 turn。
type BeginMinutesTurnResult struct {
	Turn     models.AgentTurn
	Existing bool
}

// StateRecord 是纪要页面重载时需要的 SQLite 状态。
type StateRecord struct {
	Aggregate       string
	Current         *models.MinuteVersion
	LatestCandidate *models.MinuteVersion
	RecentFailure   *models.AgentTurn
}

// ReadState 返回当前版本、最近 AI 候选与最近失败，不依赖运行时事件。
func (repository *Repository) ReadState(ctx context.Context, meetingID string) (StateRecord, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return StateRecord{}, fmt.Errorf("读取纪要状态：参数无效")
	}
	var meeting models.Meeting
	if err := repository.reader.WithContext(ctx).Select("id", "minute_state").Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		return StateRecord{}, fmt.Errorf("读取纪要聚合状态失败：%w", err)
	}
	result := StateRecord{Aggregate: meeting.MinuteState}
	var current models.MinuteVersion
	if err := repository.reader.WithContext(ctx).Select(minuteColumns()).Where("meeting_id = ? AND is_current = 1", meetingID).Take(&current).Error; err == nil {
		result.Current = &current
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return StateRecord{}, fmt.Errorf("读取当前纪要状态失败：%w", err)
	}
	var candidate models.MinuteVersion
	if err := repository.reader.WithContext(ctx).Select(minuteColumns()).Where("meeting_id = ? AND source = 'ai' AND state = 'draft' AND is_current = 0", meetingID).Order("created_at DESC").Order("version_no DESC").Take(&candidate).Error; err == nil {
		result.LatestCandidate = &candidate
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return StateRecord{}, fmt.Errorf("读取 AI 纪要候选失败：%w", err)
	}
	var failed models.AgentTurn
	if err := repository.reader.WithContext(ctx).Select(turnColumns()).Where("meeting_id = ? AND kind = 'minutes' AND state IN ?", meetingID, []string{"failed", "cancelled", "timed_out"}).Order("updated_at DESC").Take(&failed).Error; err == nil {
		result.RecentFailure = &failed
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return StateRecord{}, fmt.Errorf("读取纪要最近失败失败：%w", err)
	}
	return result, nil
}

// BeginMinutesTurn 原子校验会议、创建 turn，并把纪要状态切为 generating。
func (repository *Repository) BeginMinutesTurn(ctx context.Context, turn models.AgentTurn) (BeginMinutesTurnResult, error) {
	if repository == nil || repository.transactions == nil || turn.ID == "" || turn.MeetingID == "" || turn.AgentSessionID == "" || turn.IdempotencyKey == "" || turn.Kind != "minutes" {
		return BeginMinutesTurnResult{}, fmt.Errorf("开始纪要生成：参数无效")
	}
	var result BeginMinutesTurnResult
	err := repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var existing models.AgentTurn
		err := tx.WithContext(ctx).Where("agent_session_id = ? AND idempotency_key = ?", turn.AgentSessionID, turn.IdempotencyKey).Take(&existing).Error
		if err == nil {
			result = BeginMinutesTurnResult{Turn: existing, Existing: true}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("读取幂等纪要 turn：%w", err)
		}
		meeting := tx.WithContext(ctx).Model(&models.Meeting{}).
			Where("id = ? AND lifecycle_state IN ? AND local_save_state = 'saved' AND minute_state <> 'generating'", turn.MeetingID, []string{"ended", "interrupted"}).
			Updates(map[string]any{"minute_state": "generating", "updated_at": turn.UpdatedAt})
		if meeting.Error != nil {
			return mapWriteError("切换纪要生成状态", meeting.Error)
		}
		if meeting.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.WithContext(ctx).Select(turnColumns()).Create(&turn).Error; err != nil {
			return mapWriteError("创建纪要 turn", err)
		}
		result = BeginMinutesTurnResult{Turn: turn}
		return nil
	})
	return result, err
}

// MarkMinutesTurnRunning 记录 provider turn 身份并使用 CAS 进入 running。
func (repository *Repository) MarkMinutesTurnRunning(ctx context.Context, turnID string, providerTurnID string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || turnID == "" || providerTurnID == "" {
		return fmt.Errorf("启动纪要 turn：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND kind = 'minutes' AND state = 'pending'", turnID).
			Updates(map[string]any{"state": "running", "provider_turn_id": providerTurnID, "started_at": updatedAt, "updated_at": updatedAt})
		if result.Error != nil {
			return mapWriteError("启动纪要 turn", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// GetCurrent 返回会议当前纪要版本。
func (repository *Repository) GetCurrent(ctx context.Context, meetingID string) (models.MinuteVersion, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.MinuteVersion{}, fmt.Errorf("读取当前纪要：参数无效")
	}
	var version models.MinuteVersion
	err := repository.reader.WithContext(ctx).Select(minuteColumns()).Where("meeting_id = ? AND is_current = 1", meetingID).Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.MinuteVersion{}, ErrNotFound
	}
	if err != nil {
		return models.MinuteVersion{}, fmt.Errorf("读取当前纪要失败：%w", err)
	}
	return version, nil
}

// GetVersion 返回指定不可变版本。
func (repository *Repository) GetVersion(ctx context.Context, meetingID string, versionID string) (models.MinuteVersion, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || versionID == "" {
		return models.MinuteVersion{}, fmt.Errorf("读取纪要版本：参数无效")
	}
	var version models.MinuteVersion
	err := repository.reader.WithContext(ctx).Select(minuteColumns()).Where("meeting_id = ? AND id = ?", meetingID, versionID).Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.MinuteVersion{}, ErrNotFound
	}
	if err != nil {
		return models.MinuteVersion{}, fmt.Errorf("读取纪要版本失败：%w", err)
	}
	return version, nil
}

// ListVersions 按创建时间和版本号倒序读取历史。
func (repository *Repository) ListVersions(ctx context.Context, meetingID string, beforeVersionNo int, limit int) ([]models.MinuteVersion, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("读取纪要历史：参数无效")
	}
	query := repository.reader.WithContext(ctx).Select(minuteColumns()).Where("meeting_id = ?", meetingID)
	if beforeVersionNo > 0 {
		query = query.Where("version_no < ?", beforeVersionNo)
	}
	var versions []models.MinuteVersion
	if err := query.Order("created_at DESC").Order("version_no DESC").Limit(limit).Find(&versions).Error; err != nil {
		return nil, fmt.Errorf("读取纪要历史失败：%w", err)
	}
	return versions, nil
}

// mapWriteError 把唯一约束稳定映射为冲突。
func mapWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") || strings.Contains(strings.ToLower(err.Error()), "database is locked") {
		return fmt.Errorf("%s：%w", operation, ErrConflict)
	}
	return fmt.Errorf("%s失败：%w", operation, err)
}

// turnColumns 返回 agent_turns 的完整显式列。
func turnColumns() []string {
	return []string{"id", "meeting_id", "agent_session_id", "provider_turn_id", "kind", "state", "idempotency_key", "question_event_id", "answer_event_id", "started_at", "ended_at", "last_error_code", "created_at", "updated_at"}
}

// minuteColumns 返回 minute_versions 的完整显式列。
func minuteColumns() []string {
	return []string{"id", "meeting_id", "agent_turn_id", "parent_version_id", "version_no", "source", "content_markdown", "state", "is_current", "confirmed_at", "created_at", "updated_at"}
}
