package agent

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// GetMeeting 返回智能体编排所需的完整会议状态。
func (repository *Repository) GetMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.Meeting{}, fmt.Errorf("读取智能体会议：参数无效")
	}
	var meeting models.Meeting
	err := repository.reader.WithContext(ctx).Where("id = ?", meetingID).Take(&meeting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Meeting{}, ErrNotFound
	}
	if err != nil {
		return models.Meeting{}, fmt.Errorf("读取智能体会议失败：%w", err)
	}
	return meeting, nil
}

// LatestEventSeq 返回当前会议已提交的统一事件最大序号。
func (repository *Repository) LatestEventSeq(ctx context.Context, meetingID string) (int64, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return 0, fmt.Errorf("读取会议事件边界：参数无效")
	}
	var sequence int64
	if err := repository.reader.WithContext(ctx).Raw("SELECT COALESCE(MAX(seq), 0) FROM meeting_events WHERE meeting_id = ?", meetingID).Scan(&sequence).Error; err != nil {
		return 0, fmt.Errorf("读取会议事件边界失败：%w", err)
	}
	return sequence, nil
}

// BeginInitialization 原子把会议切为 initializing 并创建 starting session。
func (repository *Repository) BeginInitialization(ctx context.Context, session models.AgentSession) error {
	if repository == nil || repository.transactions == nil || session.ID == "" || session.MeetingID == "" {
		return fmt.Errorf("开始智能体初始化：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		meeting := tx.WithContext(ctx).Model(&models.Meeting{}).
			Where("id = ? AND lifecycle_state = 'recording' AND local_save_state = 'saving' AND agent_state IN ?", session.MeetingID, []string{"unchecked", "unavailable"}).
			Updates(map[string]any{"agent_state": "initializing", "updated_at": session.UpdatedAt})
		if meeting.Error != nil {
			return fmt.Errorf("更新会议初始化状态失败：%w", meeting.Error)
		}
		if meeting.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.WithContext(ctx).Select(sessionColumns()).Create(&session).Error; err != nil {
			return mapWriteError("创建 starting session", err)
		}
		return nil
	})
}

// SetSessionThread 保存 provider thread ID，不提前把 session 标为 available。
func (repository *Repository) SetSessionThread(ctx context.Context, sessionID string, threadID string, updatedAt int64) error {
	return repository.UpdateSessionState(ctx, sessionID, []string{"starting"}, map[string]any{"thread_id": threadID, "updated_at": updatedAt})
}

// CompleteInitialization 原子提交 initialize turn、快照并把 session/meeting 切为 available。
func (repository *Repository) CompleteInitialization(ctx context.Context, turnID string, providerTurnID string, batchID string, snapshot models.ContextSnapshot, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || turnID == "" || providerTurnID == "" {
		return fmt.Errorf("完成智能体初始化：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		turn, err := getRunningTurn(ctx, tx, turnID, providerTurnID)
		if err != nil {
			return err
		}
		if err := upsertSnapshotTx(ctx, tx, snapshot); err != nil {
			return err
		}
		if batchID != "" {
			batchResult := tx.WithContext(ctx).Model(&models.SyncBatch{}).
				Where("id = ? AND agent_session_id = ? AND state = 'running'", batchID, turn.AgentSessionID).
				Updates(map[string]any{"state": "completed", "updated_at": updatedAt})
			if batchResult.Error != nil || batchResult.RowsAffected != 1 {
				return ErrConflict
			}
		}
		turnResult := tx.WithContext(ctx).Model(&models.AgentTurn{}).
			Where("id = ? AND state = 'running' AND provider_turn_id = ?", turnID, providerTurnID).
			Updates(map[string]any{"state": "completed", "ended_at": updatedAt, "updated_at": updatedAt})
		if turnResult.Error != nil || turnResult.RowsAffected != 1 {
			return ErrConflict
		}
		sessionResult := tx.WithContext(ctx).Model(&models.AgentSession{}).
			Where("id = ? AND state = 'starting'", turn.AgentSessionID).
			Updates(map[string]any{"state": "available", "updated_at": updatedAt})
		if sessionResult.Error != nil || sessionResult.RowsAffected != 1 {
			return ErrConflict
		}
		meetingResult := tx.WithContext(ctx).Model(&models.Meeting{}).
			Where("id = ? AND agent_state = 'initializing'", turn.MeetingID).
			Updates(map[string]any{"agent_state": "available", "updated_at": updatedAt})
		if meetingResult.Error != nil || meetingResult.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// FailInitialization 原子收敛 starting session 和会议，不影响录音状态轴。
func (repository *Repository) FailInitialization(ctx context.Context, sessionID string, errorCode string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || sessionID == "" {
		return fmt.Errorf("收敛智能体初始化：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var session models.AgentSession
		if err := tx.WithContext(ctx).Select(sessionColumns()).Where("id = ?", sessionID).Take(&session).Error; err != nil {
			return ErrConflict
		}
		if session.State == "failed" && session.LastErrorCode != nil && *session.LastErrorCode == errorCode {
			return nil
		}
		if session.State != "starting" {
			return ErrConflict
		}
		sessionResult := tx.WithContext(ctx).Model(&models.AgentSession{}).Where("id = ? AND state = 'starting'", sessionID).
			Updates(map[string]any{"state": "failed", "ended_at": updatedAt, "last_error_code": errorCode, "updated_at": updatedAt})
		if sessionResult.Error != nil || sessionResult.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.WithContext(ctx).Model(&models.AgentTurn{}).
			Where("agent_session_id = ? AND state IN ?", sessionID, []string{"pending", "running"}).
			Updates(map[string]any{"state": "failed", "ended_at": updatedAt, "last_error_code": errorCode, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("收敛初始化 turn 失败：%w", err)
		}
		meetingResult := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ? AND agent_state = 'initializing'", session.MeetingID).
			Updates(map[string]any{"agent_state": "unavailable", "updated_at": updatedAt})
		if meetingResult.Error != nil || meetingResult.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// GetLatestSession 返回会议最近的本地 session，用于恢复 thread 或从本地事实重建。
func (repository *Repository) GetLatestSession(ctx context.Context, meetingID string) (models.AgentSession, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.AgentSession{}, fmt.Errorf("读取最近智能体会话：参数无效")
	}
	var session models.AgentSession
	err := repository.reader.WithContext(ctx).Select(sessionColumns()).Where("meeting_id = ?", meetingID).
		Order("created_at DESC").Order("id DESC").Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AgentSession{}, ErrNotFound
	}
	if err != nil {
		return models.AgentSession{}, fmt.Errorf("读取最近智能体会话失败：%w", err)
	}
	return session, nil
}

// EndSession 收敛活动 session，并把非终止会议的 agent 状态设为 unavailable。
func (repository *Repository) EndSession(ctx context.Context, sessionID string, errorCode *string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || sessionID == "" {
		return fmt.Errorf("结束智能体会话：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var session models.AgentSession
		if err := tx.WithContext(ctx).Select(sessionColumns()).Where("id = ? AND state IN ?", sessionID, []string{"starting", "available"}).Take(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		state := "ended"
		if errorCode != nil {
			state = "failed"
		}
		if err := tx.WithContext(ctx).Model(&models.AgentSession{}).Where("id = ?", session.ID).
			Updates(map[string]any{"state": state, "ended_at": updatedAt, "last_error_code": errorCode, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("结束智能体 session 失败：%w", err)
		}
		if err := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ? AND agent_state IN ?", session.MeetingID, []string{"initializing", "available", "busy"}).
			Updates(map[string]any{"agent_state": "unavailable", "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("结束智能体会议状态失败：%w", err)
		}
		return nil
	})
}

// GetSettings 返回唤醒词和 Codex 启动配置；凭据字段不在此服务读取范围。
func (repository *Repository) GetSettings(ctx context.Context) (models.Settings, error) {
	if repository == nil || repository.reader == nil {
		return models.Settings{}, fmt.Errorf("读取 Codex 设置：Repository 不可用")
	}
	var settings models.Settings
	err := repository.reader.WithContext(ctx).Select(
		"id", "singleton_key", "wake_word", "codex_executable_path", "codex_proxy_port",
		"codex_availability_state", "codex_version", "codex_account_state",
		"codex_protocol_state", "codex_probe_message", "codex_probed_at",
		"created_at", "updated_at",
	).
		Where("singleton_key = 1").Take(&settings).Error
	if err != nil {
		return models.Settings{}, fmt.Errorf("读取 Codex 设置失败：%w", err)
	}
	return settings, nil
}

// UpdateSettings 保存设置；Codex 启动配置真正变化时才让既有检测快照失效。
func (repository *Repository) UpdateSettings(ctx context.Context, wakeWord string, executablePath *string, proxyPort *int, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || wakeWord == "" {
		return fmt.Errorf("保存 Codex 设置：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var current models.Settings
		if err := tx.WithContext(ctx).Select("codex_executable_path", "codex_proxy_port").Where("singleton_key = 1").Take(&current).Error; err != nil {
			return fmt.Errorf("读取 Codex 原设置失败：%w", err)
		}
		updates := map[string]any{"wake_word": wakeWord, "codex_executable_path": executablePath, "codex_proxy_port": proxyPort, "updated_at": updatedAt}
		if optionalStringChanged(current.CodexExecutablePath, executablePath) || optionalIntChanged(current.CodexProxyPort, proxyPort) {
			updates["codex_availability_state"] = "unchecked"
			updates["codex_version"] = ""
			updates["codex_account_state"] = "unknown"
			updates["codex_protocol_state"] = "unchecked"
			updates["codex_probe_message"] = "设置已更新，尚未检测"
			updates["codex_probed_at"] = nil
		}
		result := tx.WithContext(ctx).Model(&models.Settings{}).Where("singleton_key = 1").
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("保存 Codex 设置失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

// UpdateProbeSnapshot 原子保存不含账号、路径和凭据的 Codex 检测结果。
func (repository *Repository) UpdateProbeSnapshot(ctx context.Context, state string, version string, accountState string, protocolState string, message string, probedAt int64) error {
	if repository == nil || repository.transactions == nil || state == "" || accountState == "" || protocolState == "" || probedAt < 0 {
		return fmt.Errorf("保存 Codex 检测结果：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.Settings{}).Where("singleton_key = 1").Updates(map[string]any{
			"codex_availability_state": state, "codex_version": version,
			"codex_account_state": accountState, "codex_protocol_state": protocolState,
			"codex_probe_message": message, "codex_probed_at": probedAt, "updated_at": probedAt,
		})
		if result.Error != nil {
			return fmt.Errorf("保存 Codex 检测结果失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

// optionalStringChanged 判断两个数据库可选字符串是否发生语义变化。
func optionalStringChanged(left *string, right *string) bool {
	if left == nil || right == nil {
		return left != nil || right != nil
	}
	return *left != *right
}

// optionalIntChanged 判断两个可选端口是否发生语义变化。
func optionalIntChanged(left *int, right *int) bool {
	if left == nil || right == nil {
		return left != nil || right != nil
	}
	return *left != *right
}
