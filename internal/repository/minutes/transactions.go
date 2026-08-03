package minutes

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// CommitGeneratedMinuteInput 描述已校验 AI 输出的原子提交输入。
type CommitGeneratedMinuteInput struct {
	VersionID       string
	TurnID          string
	ProviderTurnID  string
	ContentMarkdown string
	UpdatedAt       int64
}

// SaveHumanMinuteInput 描述从明确基线创建人工草稿的输入。
type SaveHumanMinuteInput struct {
	VersionID       string
	MeetingID       string
	BaseVersionID   string
	ContentMarkdown string
	UpdatedAt       int64
}

// ConfirmMinuteInput 描述确认当前版本的 CAS 输入。
type ConfirmMinuteInput struct {
	MeetingID   string
	VersionID   string
	ConfirmedAt int64
}

// RestoreMinuteInput 描述复制历史版本的输入。
type RestoreMinuteInput struct {
	VersionID       string
	MeetingID       string
	SourceVersionID string
	UpdatedAt       int64
}

// FailMinutesTurnInput 描述失败、停止或超时的收敛输入。
type FailMinutesTurnInput struct {
	TurnID    string
	State     string
	ErrorCode string
	UpdatedAt int64
}

// CommitGeneratedMinute 原子完成 minutes turn 并创建不可变 AI 版本。
func (repository *Repository) CommitGeneratedMinute(ctx context.Context, input CommitGeneratedMinuteInput) (models.MinuteVersion, error) {
	if repository == nil || repository.transactions == nil || input.VersionID == "" || input.TurnID == "" || input.ProviderTurnID == "" || input.ContentMarkdown == "" {
		return models.MinuteVersion{}, fmt.Errorf("提交 AI 纪要：参数无效")
	}
	var created models.MinuteVersion
	err := repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var existing models.MinuteVersion
		if err := tx.WithContext(ctx).Where("agent_turn_id = ?", input.TurnID).Take(&existing).Error; err == nil {
			created = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("读取 AI 纪要幂等结果：%w", err)
		}
		turn, err := runningMinutesTurn(ctx, tx, input.TurnID, input.ProviderTurnID)
		if err != nil {
			return err
		}
		current, hasCurrent, err := currentVersion(ctx, tx, turn.MeetingID)
		if err != nil {
			return err
		}
		makeCurrent := !hasCurrent || (current.Source == "ai" && current.State == "draft")
		if makeCurrent && hasCurrent {
			if err := clearCurrent(ctx, tx, current.ID); err != nil {
				return err
			}
		}
		created = models.MinuteVersion{
			ID: input.VersionID, MeetingID: turn.MeetingID, AgentTurnID: &turn.ID,
			VersionNo: nextVersionNo(ctx, tx, turn.MeetingID), Source: "ai", ContentMarkdown: input.ContentMarkdown,
			State: "draft", IsCurrent: makeCurrent, CreatedAt: input.UpdatedAt, UpdatedAt: input.UpdatedAt,
		}
		if created.VersionNo == 0 {
			return fmt.Errorf("分配 AI 纪要版本号失败")
		}
		if err := tx.WithContext(ctx).Select(minuteColumns()).Create(&created).Error; err != nil {
			return mapWriteError("创建 AI 纪要版本", err)
		}
		if err := finishMinutesTurn(ctx, tx, turn, input.ProviderTurnID, input.UpdatedAt); err != nil {
			return err
		}
		return setMinuteState(ctx, tx, turn.MeetingID, "draft", input.UpdatedAt)
	})
	return created, err
}

// SaveHumanMinute 创建新的人工版本，并要求编辑基线仍是 current。
func (repository *Repository) SaveHumanMinute(ctx context.Context, input SaveHumanMinuteInput) (models.MinuteVersion, error) {
	if repository == nil || repository.transactions == nil || input.VersionID == "" || input.MeetingID == "" || input.BaseVersionID == "" || input.ContentMarkdown == "" {
		return models.MinuteVersion{}, fmt.Errorf("保存人工纪要：参数无效")
	}
	var created models.MinuteVersion
	err := repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if existing, found, err := versionByID(ctx, tx, input.VersionID); err != nil {
			return err
		} else if found {
			if existing.MeetingID != input.MeetingID || existing.Source != "human" {
				return ErrConflict
			}
			created = existing
			return nil
		}
		base, hasCurrent, err := currentVersion(ctx, tx, input.MeetingID)
		if err != nil {
			return err
		}
		if !hasCurrent || base.ID != input.BaseVersionID {
			return ErrConflict
		}
		if err := clearCurrent(ctx, tx, base.ID); err != nil {
			return err
		}
		created = newCopiedVersion(input.VersionID, input.MeetingID, base, "human", input.ContentMarkdown, input.UpdatedAt)
		created.VersionNo = nextVersionNo(ctx, tx, input.MeetingID)
		if created.VersionNo == 0 {
			return fmt.Errorf("分配人工纪要版本号失败")
		}
		created.State, created.ConfirmedAt = "draft", nil
		if err := tx.WithContext(ctx).Select(minuteColumns()).Create(&created).Error; err != nil {
			return mapWriteError("创建人工纪要版本", err)
		}
		return setMinuteState(ctx, tx, input.MeetingID, "draft", input.UpdatedAt)
	})
	return created, err
}

// ConfirmCurrentMinute 确认目标 current；重复确认同一版本幂等。
func (repository *Repository) ConfirmCurrentMinute(ctx context.Context, input ConfirmMinuteInput) error {
	if repository == nil || repository.transactions == nil || input.MeetingID == "" || input.VersionID == "" {
		return fmt.Errorf("确认纪要：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var version models.MinuteVersion
		if err := tx.WithContext(ctx).Select(minuteColumns()).Where("id = ? AND meeting_id = ? AND is_current = 1", input.VersionID, input.MeetingID).Take(&version).Error; err != nil {
			return ErrConflict
		}
		if version.State == "confirmed" {
			return nil
		}
		result := tx.WithContext(ctx).Model(&models.MinuteVersion{}).Where("id = ? AND is_current = 1 AND state = 'draft'", version.ID).
			Updates(map[string]any{"state": "confirmed", "confirmed_at": input.ConfirmedAt, "updated_at": input.ConfirmedAt})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
		state := "confirmed"
		var candidates int64
		if err := tx.WithContext(ctx).Model(&models.MinuteVersion{}).Where("meeting_id = ? AND source = 'ai' AND state = 'draft' AND is_current = 0", input.MeetingID).Count(&candidates).Error; err != nil {
			return fmt.Errorf("读取 AI 候选失败：%w", err)
		}
		if candidates > 0 {
			state = "draft"
		}
		return setMinuteState(ctx, tx, input.MeetingID, state, input.ConfirmedAt)
	})
}

// RestoreMinuteVersion 复制目标历史内容为新版本并成为 current。
func (repository *Repository) RestoreMinuteVersion(ctx context.Context, input RestoreMinuteInput) (models.MinuteVersion, error) {
	if repository == nil || repository.transactions == nil || input.VersionID == "" || input.MeetingID == "" || input.SourceVersionID == "" {
		return models.MinuteVersion{}, fmt.Errorf("恢复纪要版本：参数无效")
	}
	var created models.MinuteVersion
	err := repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if existing, found, err := versionByID(ctx, tx, input.VersionID); err != nil {
			return err
		} else if found {
			if existing.MeetingID != input.MeetingID || existing.Source != "restored" {
				return ErrConflict
			}
			created = existing
			return nil
		}
		var source models.MinuteVersion
		if err := tx.WithContext(ctx).Select(minuteColumns()).Where("id = ? AND meeting_id = ?", input.SourceVersionID, input.MeetingID).Take(&source).Error; err != nil {
			return ErrNotFound
		}
		if current, found, err := currentVersion(ctx, tx, input.MeetingID); err != nil {
			return err
		} else if found {
			if err := clearCurrent(ctx, tx, current.ID); err != nil {
				return err
			}
		}
		created = newCopiedVersion(input.VersionID, input.MeetingID, source, "restored", source.ContentMarkdown, input.UpdatedAt)
		created.VersionNo = nextVersionNo(ctx, tx, input.MeetingID)
		if created.VersionNo == 0 {
			return fmt.Errorf("分配恢复纪要版本号失败")
		}
		created.State, created.ConfirmedAt = source.State, source.ConfirmedAt
		if err := tx.WithContext(ctx).Select(minuteColumns()).Create(&created).Error; err != nil {
			return mapWriteError("创建恢复纪要版本", err)
		}
		return setMinuteState(ctx, tx, input.MeetingID, source.State, input.UpdatedAt)
	})
	return created, err
}

// FailMinutesTurn 收敛活动 turn，不创建新版本也不改变旧版本内容。
func (repository *Repository) FailMinutesTurn(ctx context.Context, input FailMinutesTurnInput) error {
	if repository == nil || repository.transactions == nil || input.TurnID == "" || input.ErrorCode == "" || (input.State != "failed" && input.State != "cancelled" && input.State != "timed_out") {
		return fmt.Errorf("收敛纪要 turn：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var turn models.AgentTurn
		if err := tx.WithContext(ctx).Select(turnColumns()).Where("id = ? AND kind = 'minutes'", input.TurnID).Take(&turn).Error; err != nil {
			return ErrNotFound
		}
		if turn.State == input.State {
			return nil
		}
		result := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state IN ?", input.TurnID, []string{"pending", "running"}).
			Updates(map[string]any{"state": input.State, "ended_at": input.UpdatedAt, "last_error_code": input.ErrorCode, "updated_at": input.UpdatedAt})
		if result.Error != nil || result.RowsAffected != 1 {
			return ErrConflict
		}
		return setMinuteState(ctx, tx, turn.MeetingID, "failed", input.UpdatedAt)
	})
}

// RecoverInterruptedTurns 把进程退出遗留的 minutes turn 收敛为 failed。
func (repository *Repository) RecoverInterruptedTurns(ctx context.Context, errorCode string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || errorCode == "" {
		return fmt.Errorf("恢复遗留纪要 turn：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var meetingIDs []string
		if err := tx.WithContext(ctx).Model(&models.AgentTurn{}).Distinct("meeting_id").Where("kind = 'minutes' AND state IN ?", []string{"pending", "running"}).Pluck("meeting_id", &meetingIDs).Error; err != nil {
			return fmt.Errorf("读取遗留纪要 turn：%w", err)
		}
		if len(meetingIDs) == 0 {
			return nil
		}
		if err := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("kind = 'minutes' AND state IN ?", []string{"pending", "running"}).
			Updates(map[string]any{"state": "failed", "ended_at": updatedAt, "last_error_code": errorCode, "updated_at": updatedAt}).Error; err != nil {
			return fmt.Errorf("收敛遗留纪要 turn：%w", err)
		}
		return tx.WithContext(ctx).Model(&models.Meeting{}).Where("id IN ?", meetingIDs).Updates(map[string]any{"minute_state": "failed", "updated_at": updatedAt}).Error
	})
}

// runningMinutesTurn 读取匹配 provider 身份的 running turn。
func runningMinutesTurn(ctx context.Context, tx *gorm.DB, turnID string, providerTurnID string) (models.AgentTurn, error) {
	var turn models.AgentTurn
	err := tx.WithContext(ctx).Select(turnColumns()).Where("id = ? AND kind = 'minutes' AND state = 'running' AND provider_turn_id = ?", turnID, providerTurnID).Take(&turn).Error
	if err != nil {
		return models.AgentTurn{}, ErrConflict
	}
	return turn, nil
}

// currentVersion 返回当前版本及是否存在。
func currentVersion(ctx context.Context, tx *gorm.DB, meetingID string) (models.MinuteVersion, bool, error) {
	var version models.MinuteVersion
	err := tx.WithContext(ctx).Select(minuteColumns()).Where("meeting_id = ? AND is_current = 1", meetingID).Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.MinuteVersion{}, false, nil
	}
	if err != nil {
		return models.MinuteVersion{}, false, fmt.Errorf("读取当前纪要失败：%w", err)
	}
	return version, true, nil
}

// versionByID 查找全局唯一版本 ID。
func versionByID(ctx context.Context, tx *gorm.DB, versionID string) (models.MinuteVersion, bool, error) {
	var version models.MinuteVersion
	err := tx.WithContext(ctx).Select(minuteColumns()).Where("id = ?", versionID).Take(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.MinuteVersion{}, false, nil
	}
	if err != nil {
		return models.MinuteVersion{}, false, fmt.Errorf("读取幂等纪要版本失败：%w", err)
	}
	return version, true, nil
}

// clearCurrent 使用来源状态谓词取消 current。
func clearCurrent(ctx context.Context, tx *gorm.DB, versionID string) error {
	result := tx.WithContext(ctx).Model(&models.MinuteVersion{}).Where("id = ? AND is_current = 1", versionID).Update("is_current", false)
	if result.Error != nil || result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

// nextVersionNo 在单 writer 事务内分配单调版本号。
func nextVersionNo(ctx context.Context, tx *gorm.DB, meetingID string) int {
	var maximum int
	if err := tx.WithContext(ctx).Raw("SELECT COALESCE(MAX(version_no), 0) FROM minute_versions WHERE meeting_id = ?", meetingID).Scan(&maximum).Error; err != nil {
		return 0
	}
	return maximum + 1
}

// newCopiedVersion 构造带父版本关系的新 current 版本。
func newCopiedVersion(id string, meetingID string, parent models.MinuteVersion, source string, content string, updatedAt int64) models.MinuteVersion {
	parentID := parent.ID
	return models.MinuteVersion{
		ID: id, MeetingID: meetingID, ParentVersionID: &parentID, VersionNo: parent.VersionNo + 1,
		Source: source, ContentMarkdown: content, State: "draft", IsCurrent: true,
		CreatedAt: updatedAt, UpdatedAt: updatedAt,
	}
}

// finishMinutesTurn 以 provider turn 身份完成活动 turn。
func finishMinutesTurn(ctx context.Context, tx *gorm.DB, turn models.AgentTurn, providerTurnID string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state = 'running' AND provider_turn_id = ?", turn.ID, providerTurnID).
		Updates(map[string]any{"state": "completed", "ended_at": updatedAt, "updated_at": updatedAt})
	if result.Error != nil || result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

// setMinuteState 更新会议独立纪要状态轴。
func setMinuteState(ctx context.Context, tx *gorm.DB, meetingID string, state string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ?", meetingID).Updates(map[string]any{"minute_state": state, "updated_at": updatedAt})
	if result.Error != nil || result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
