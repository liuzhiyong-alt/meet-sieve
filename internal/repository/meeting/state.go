package meeting

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

var (
	// ErrMeetingStateConflict 表示会议不存在或当前状态不允许目标转换。
	ErrMeetingStateConflict = errors.New("会议状态冲突")
)

// MarkRecordingStarted 在首帧安全持久化后原子写入 recording/saving 提交点。
func (repository *Repository) MarkRecordingStarted(ctx context.Context, meetingID string, startedAt int64) error {
	return repository.updateState(ctx, meetingID,
		"lifecycle_state = ? AND local_save_state = ?", []any{"preparing", "pending"},
		map[string]any{
			"lifecycle_state": "recording", "local_save_state": "saving",
			"started_at": startedAt, "updated_at": startedAt,
		})
}

// BeginFinalizing 原子取得正常结束的唯一收尾权。
func (repository *Repository) BeginFinalizing(ctx context.Context, meetingID string, updatedAt int64) error {
	return repository.updateState(ctx, meetingID,
		"lifecycle_state = ? AND local_save_state = ?", []any{"recording", "saving"},
		map[string]any{"lifecycle_state": "finalizing", "updated_at": updatedAt})
}

// CompleteMeeting 仅在最终录音校验通过后写入 ended/saved。
func (repository *Repository) CompleteMeeting(ctx context.Context, meetingID string, endedAt int64) error {
	return repository.updateState(ctx, meetingID,
		"lifecycle_state = ? AND local_save_state = ?", []any{"finalizing", "saving"},
		map[string]any{
			"lifecycle_state": "ended", "local_save_state": "saved",
			"ended_at": endedAt, "updated_at": endedAt,
		})
}

// MarkFinalizingFailed 保持 finalizing 并准确标记本地保存失败，供后续重试合并。
func (repository *Repository) MarkFinalizingFailed(ctx context.Context, meetingID string, updatedAt int64) error {
	return repository.updateState(ctx, meetingID,
		"lifecycle_state = ? AND local_save_state = ?", []any{"finalizing", "saving"},
		map[string]any{"local_save_state": "failed", "updated_at": updatedAt})
}

// ResumeFinalizing 将可重试的 finalizing/failed 恢复为 saving，不改变录音生命周期。
func (repository *Repository) ResumeFinalizing(ctx context.Context, meetingID string, updatedAt int64) error {
	return repository.updateState(ctx, meetingID,
		"lifecycle_state = ? AND local_save_state = ?", []any{"finalizing", "failed"},
		map[string]any{"local_save_state": "saving", "updated_at": updatedAt})
}

// GetMeeting 按 ID 返回会议聚合的完整状态投影。
func (repository *Repository) GetMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.Meeting{}, fmt.Errorf("读取会议：依赖或会议 ID 无效")
	}
	var meeting models.Meeting
	if err := repository.reader.WithContext(ctx).Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		return models.Meeting{}, fmt.Errorf("读取会议失败: %w", err)
	}
	return meeting, nil
}

// ListActiveMeetings 返回应用启动时必须恢复的 preparing、recording 和 finalizing 会议。
func (repository *Repository) ListActiveMeetings(ctx context.Context) ([]models.Meeting, error) {
	if repository == nil || repository.reader == nil {
		return nil, fmt.Errorf("读取活动会议：Repository 不可用")
	}
	var meetings []models.Meeting
	if err := repository.reader.WithContext(ctx).Where("lifecycle_state IN ?", []string{"preparing", "recording", "finalizing"}).
		Order("created_at ASC").Find(&meetings).Error; err != nil {
		return nil, fmt.Errorf("读取活动会议失败: %w", err)
	}
	return meetings, nil
}

// FinishRecovery 将遗留活动会议收敛为 interrupted，并按恢复结果记录本地保存状态。
func (repository *Repository) FinishRecovery(ctx context.Context, meetingID string, saved bool, updatedAt int64) error {
	localSaveState := "failed"
	if saved {
		localSaveState = "saved"
	}
	return repository.updateState(ctx, meetingID,
		"lifecycle_state IN ?", []any{[]string{"preparing", "recording", "finalizing"}},
		map[string]any{
			"lifecycle_state": "interrupted", "local_save_state": localSaveState,
			"ended_at": interruptedEndedAt(updatedAt), "updated_at": updatedAt,
		})
}

// GetLatestInterruptedMeeting 返回最近一次不可续录会议，供恢复页在应用重启后重建状态。
func (repository *Repository) GetLatestInterruptedMeeting(ctx context.Context) (*models.Meeting, error) {
	if repository == nil || repository.reader == nil {
		return nil, fmt.Errorf("读取中断会议：Repository 不可用")
	}
	var meeting models.Meeting
	err := repository.reader.WithContext(ctx).Where("lifecycle_state = ?", "interrupted").
		Order("updated_at DESC").Order("id ASC").Take(&meeting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取中断会议失败: %w", err)
	}
	return &meeting, nil
}

// UpdateInterruptedRecovery 只更新 interrupted 会议的本地保存结果，不伪装为 ended。
func (repository *Repository) UpdateInterruptedRecovery(ctx context.Context, meetingID string, saved bool, updatedAt int64) error {
	localSaveState := "failed"
	if saved {
		localSaveState = "saved"
	}
	return repository.updateState(ctx, meetingID,
		"lifecycle_state = ?", []any{"interrupted"},
		map[string]any{"local_save_state": localSaveState, "updated_at": updatedAt})
}

// InterruptMeeting 将任一活动会议收敛为不可续录的 interrupted/failed。
func (repository *Repository) InterruptMeeting(ctx context.Context, meetingID string, updatedAt int64) error {
	return repository.updateState(ctx, meetingID,
		"lifecycle_state IN ?", []any{[]string{"preparing", "recording", "finalizing"}},
		map[string]any{
			"lifecycle_state": "interrupted", "local_save_state": "failed",
			"ended_at": interruptedEndedAt(updatedAt), "updated_at": updatedAt,
		})
}

// interruptedEndedAt 只为已经取得首帧的会议冻结时长；preparing 没有合法开始时间。
func interruptedEndedAt(updatedAt int64) any {
	return gorm.Expr("CASE WHEN started_at IS NULL THEN NULL ELSE ? END", updatedAt)
}

// DeletePreparing 删除尚未取得首帧的会议及级联快照，已开始录音的会议不会被删除。
func (repository *Repository) DeletePreparing(ctx context.Context, meetingID string) error {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return fmt.Errorf("删除准备中会议：依赖或会议 ID 无效")
	}
	result := repository.reader.WithContext(ctx).Where("id = ? AND lifecycle_state = ?", meetingID, "preparing").Delete(&models.Meeting{})
	if result.Error != nil {
		return fmt.Errorf("删除准备中会议失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrMeetingStateConflict
	}
	return nil
}

// updateState 执行带来源状态谓词的单行更新，避免并发调用跨级覆盖。
func (repository *Repository) updateState(ctx context.Context, meetingID string, predicate string, arguments []any, updates map[string]any) error {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return fmt.Errorf("更新会议状态：依赖或会议 ID 无效")
	}
	query := repository.reader.WithContext(ctx).Model(&models.Meeting{}).Where("id = ?", meetingID)
	query = query.Where(predicate, arguments...)
	result := query.Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("更新会议状态失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrMeetingStateConflict
	}
	return nil
}
