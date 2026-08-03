package agent

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// BeginFinalSyncResult 返回新建、重试或已完成的 ingest turn。
type BeginFinalSyncResult struct {
	Turn      models.AgentTurn
	Completed bool
}

// HasSuccessfulAgentSession 判断会议是否曾完成 initialize，避免创建伪结束同步。
func (repository *Repository) HasSuccessfulAgentSession(ctx context.Context, meetingID string) (bool, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return false, fmt.Errorf("读取 Codex 启用事实：参数无效")
	}
	var count int64
	err := repository.reader.WithContext(ctx).Model(&models.AgentTurn{}).Where("meeting_id = ? AND kind = 'initialize' AND state = 'completed'", meetingID).Count(&count).Error
	return count > 0, err
}

// BeginFinalSync 原子取得结束同步执行权；相同 key 的失败 turn 从原游标重试。
func (repository *Repository) BeginFinalSync(ctx context.Context, turn models.AgentTurn) (BeginFinalSyncResult, error) {
	if repository == nil || repository.transactions == nil || turn.ID == "" || turn.MeetingID == "" || turn.AgentSessionID == "" || turn.Kind != "ingest" || turn.IdempotencyKey == "" {
		return BeginFinalSyncResult{}, fmt.Errorf("开始 Codex 结束同步：参数无效")
	}
	var result BeginFinalSyncResult
	err := repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var existing models.AgentTurn
		err := tx.WithContext(ctx).Select(turnColumns()).Where("agent_session_id = ? AND idempotency_key = ?", turn.AgentSessionID, turn.IdempotencyKey).Take(&existing).Error
		if err == nil {
			if existing.State == "completed" {
				result = BeginFinalSyncResult{Turn: existing, Completed: true}
				return nil
			}
			if existing.State != "failed" && existing.State != "timed_out" && existing.State != "cancelled" {
				return ErrConflict
			}
			reset := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state IN ?", existing.ID, []string{"failed", "timed_out", "cancelled"}).Updates(map[string]any{"state": "pending", "provider_turn_id": nil, "started_at": nil, "ended_at": nil, "last_error_code": nil, "updated_at": turn.UpdatedAt})
			if reset.Error != nil || reset.RowsAffected != 1 {
				return ErrConflict
			}
			existing.State, existing.ProviderTurnID, existing.StartedAt, existing.EndedAt, existing.LastErrorCode = "pending", nil, nil, nil, nil
			result.Turn = existing
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("读取结束同步 turn 失败：%w", err)
		} else {
			if err := tx.WithContext(ctx).Select(turnColumns()).Create(&turn).Error; err != nil {
				return mapWriteError("创建结束同步 turn", err)
			}
			result.Turn = turn
		}
		meeting := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ? AND lifecycle_state IN ? AND local_save_state = 'saved' AND agent_state IN ?", turn.MeetingID, []string{"ended", "interrupted"}, []string{"available", "unsynced"}).Updates(map[string]any{"agent_state": "busy", "updated_at": turn.UpdatedAt})
		if meeting.Error != nil || meeting.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
	return result, err
}

// CompleteFinalSyncNoChanges 完成没有新增事件的结束同步。
func (repository *Repository) CompleteFinalSyncNoChanges(ctx context.Context, turnID string, updatedAt int64) error {
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var turn models.AgentTurn
		if err := tx.WithContext(ctx).Select(turnColumns()).Where("id = ? AND state = 'pending'", turnID).Take(&turn).Error; err != nil {
			return ErrConflict
		}
		if result := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state = 'pending'", turnID).Updates(map[string]any{"state": "completed", "started_at": updatedAt, "ended_at": updatedAt, "updated_at": updatedAt}); result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
		return setFinalAgentState(ctx, tx, turn.MeetingID, "unavailable", updatedAt)
	})
}

// CompleteFinalSync 原子提交最后 snapshot/batch/turn，且不创建 ai.answer。
func (repository *Repository) CompleteFinalSync(ctx context.Context, turnID string, providerTurnID string, batchID string, snapshot models.ContextSnapshot, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || turnID == "" || providerTurnID == "" || batchID == "" {
		return fmt.Errorf("完成 Codex 结束同步：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		turn, err := getRunningTurn(ctx, tx, turnID, providerTurnID)
		if err != nil {
			return err
		}
		if err := upsertSnapshotTx(ctx, tx, snapshot); err != nil {
			return err
		}
		batch := tx.WithContext(ctx).Model(&models.SyncBatch{}).Where("id = ? AND agent_session_id = ? AND state = 'running'", batchID, turn.AgentSessionID).Updates(map[string]any{"state": "completed", "updated_at": updatedAt})
		if batch.Error != nil || batch.RowsAffected != 1 {
			return ErrConflict
		}
		finished := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state = 'running' AND provider_turn_id = ?", turnID, providerTurnID).Updates(map[string]any{"state": "completed", "ended_at": updatedAt, "updated_at": updatedAt})
		if finished.Error != nil || finished.RowsAffected != 1 {
			return ErrConflict
		}
		return setFinalAgentState(ctx, tx, turn.MeetingID, "unavailable", updatedAt)
	})
}

// FailFinalSync 收敛 turn/batch 并独立标记 meeting.agent_state=unsynced。
func (repository *Repository) FailFinalSync(ctx context.Context, turnID string, errorCode string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || turnID == "" || errorCode == "" {
		return fmt.Errorf("收敛 Codex 结束同步：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var turn models.AgentTurn
		if err := tx.WithContext(ctx).Select(turnColumns()).Where("id = ?", turnID).Take(&turn).Error; err != nil {
			return ErrNotFound
		}
		if turn.State == "completed" {
			return nil
		}
		result := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state IN ?", turnID, []string{"pending", "running"}).Updates(map[string]any{"state": "failed", "ended_at": updatedAt, "last_error_code": errorCode, "updated_at": updatedAt})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.WithContext(ctx).Model(&models.SyncBatch{}).Where("agent_session_id = ? AND state = 'running'", turn.AgentSessionID).Updates(map[string]any{"state": "failed", "last_error_code": errorCode, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("收敛结束同步批次失败：%w", err)
		}
		return setFinalAgentState(ctx, tx, turn.MeetingID, "unsynced", updatedAt)
	})
}

// MarkFinalSyncUnsynced 在 turn 尚未创建的前置失败中标记独立同步状态。
func (repository *Repository) MarkFinalSyncUnsynced(ctx context.Context, meetingID string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || meetingID == "" {
		return fmt.Errorf("标记 Codex 结束同步失败：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.Meeting{}).
			Where("id = ? AND lifecycle_state IN ? AND local_save_state = 'saved' AND agent_state <> 'unchecked'", meetingID, []string{"ended", "interrupted"}).
			Updates(map[string]any{"agent_state": "unsynced", "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("标记 Codex 结束同步失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// RecoverFinalSyncState 收敛遗留 ingest turn/batch 并标记 unsynced，不恢复 provider。
func (repository *Repository) RecoverFinalSyncState(ctx context.Context, errorCode string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || errorCode == "" {
		return fmt.Errorf("恢复 Codex 结束同步：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var sessionIDs []string
		if err := tx.WithContext(ctx).Model(&models.AgentTurn{}).Distinct("agent_session_id").Where("kind = 'ingest' AND state IN ?", []string{"pending", "running"}).Pluck("agent_session_id", &sessionIDs).Error; err != nil {
			return fmt.Errorf("读取遗留结束同步：%w", err)
		}
		if len(sessionIDs) == 0 {
			return nil
		}
		var meetingIDs []string
		if err := tx.WithContext(ctx).Model(&models.AgentSession{}).Distinct("meeting_id").Where("id IN ?", sessionIDs).Pluck("meeting_id", &meetingIDs).Error; err != nil {
			return fmt.Errorf("读取遗留结束同步会议：%w", err)
		}
		if err := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("kind = 'ingest' AND state IN ?", []string{"pending", "running"}).Updates(map[string]any{"state": "failed", "ended_at": updatedAt, "last_error_code": errorCode, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("收敛遗留结束同步 turn：%w", err)
		}
		if err := tx.WithContext(ctx).Model(&models.SyncBatch{}).Where("agent_session_id IN ? AND state IN ?", sessionIDs, []string{"pending", "running"}).Updates(map[string]any{"state": "failed", "last_error_code": errorCode, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("收敛遗留结束同步批次：%w", err)
		}
		return tx.WithContext(ctx).Model(&models.Meeting{}).Where("id IN ?", meetingIDs).Updates(map[string]any{"agent_state": "unsynced", "updated_at": updatedAt}).Error
	})
}

// ResetBatchForRetry 把相同幂等批次从 failed 恢复为 pending。
func (repository *Repository) ResetBatchForRetry(ctx context.Context, batchID string, updatedAt int64) error {
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.SyncBatch{}).Where("id = ? AND state = 'failed'", batchID).Updates(map[string]any{"state": "pending", "last_error_code": nil, "updated_at": updatedAt})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// setFinalAgentState 更新结束同步独立状态轴。
func setFinalAgentState(ctx context.Context, tx *gorm.DB, meetingID string, state string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ? AND agent_state = 'busy'", meetingID).Updates(map[string]any{"agent_state": state, "updated_at": updatedAt})
	if result.Error != nil || result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
