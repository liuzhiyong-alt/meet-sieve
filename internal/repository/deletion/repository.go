// Package deletion 使用 SQLite 短事务持久化可恢复删除任务。
package deletion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"meet-sieve/internal/infra/database"
	"meet-sieve/models"

	"gorm.io/gorm"
)

var ErrActiveJob = errors.New("会议已有活动删除任务")

// FailedItem 是允许持久化和展示的最小失败项目，不包含绝对路径或底层错误。
type FailedItem struct {
	ItemID   string `json:"item_id"`
	SafeName string `json:"safe_name"`
	Code     string `json:"code"`
}

// Repository 维护 deletion_jobs 和删除最终事实。
type Repository struct {
	reader       *gorm.DB
	transactions *database.TransactionManager
}

// NewRepository 创建删除任务 Repository。
func NewRepository(reader *gorm.DB, transactions *database.TransactionManager) *Repository {
	return &Repository{reader: reader, transactions: transactions}
}

// Create 创建 pending 任务；唯一索引保证每场每类只有一个活动任务。
func (repository *Repository) Create(ctx context.Context, job models.DeletionJob) error {
	if repository == nil || repository.transactions == nil || job.ID == "" || job.MeetingID == "" {
		return fmt.Errorf("创建删除任务：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Select(jobColumns()).Create(&job).Error; err != nil {
			if isUniqueConflict(err) {
				return ErrActiveJob
			}
			return fmt.Errorf("创建删除任务失败: %w", err)
		}
		return nil
	})
}

// Get 返回指定任务的当前持久状态。
func (repository *Repository) Get(ctx context.Context, jobID string) (models.DeletionJob, error) {
	if repository == nil || repository.reader == nil || jobID == "" {
		return models.DeletionJob{}, fmt.Errorf("读取删除任务：参数无效")
	}
	var job models.DeletionJob
	if err := repository.reader.WithContext(ctx).Select(jobColumns()).Where("id = ?", jobID).Take(&job).Error; err != nil {
		return models.DeletionJob{}, err
	}
	return job, nil
}

// GetActiveByMeeting 返回同场任一未终结删除任务。
func (repository *Repository) GetActiveByMeeting(ctx context.Context, meetingID string) (*models.DeletionJob, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取会议删除任务：参数无效")
	}
	var job models.DeletionJob
	err := repository.reader.WithContext(ctx).Select(jobColumns()).
		Where("meeting_id = ? AND state IN ?", meetingID, []string{"pending", "running", "failed"}).
		Order("updated_at DESC").Take(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取会议删除任务失败: %w", err)
	}
	return &job, nil
}

// GetMeeting 返回删除预览所需的会议目录、会议号和 revision。
func (repository *Repository) GetMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return models.Meeting{}, fmt.Errorf("读取待删除会议：参数无效")
	}
	var meeting models.Meeting
	if err := repository.reader.WithContext(ctx).
		Select("id", "meeting_no", "relative_dir", "lifecycle_state", "updated_at").
		Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		return models.Meeting{}, err
	}
	return meeting, nil
}

// ListAudioAssets 返回录音删除预览所需的全部未删除音频资产。
func (repository *Repository) ListAudioAssets(ctx context.Context, meetingID string) ([]models.AudioAsset, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取待删除音频：参数无效")
	}
	var assets []models.AudioAsset
	if err := repository.reader.WithContext(ctx).
		Select("id", "meeting_id", "kind", "relative_path", "size_bytes", "state", "updated_at").
		Where("meeting_id = ? AND state <> 'deleted'", meetingID).Order("relative_path ASC").Find(&assets).Error; err != nil {
		return nil, fmt.Errorf("读取待删除音频失败: %w", err)
	}
	return assets, nil
}

// MarkRunning 使用 CAS 把 pending/failed 任务切到 running，显式重试时增加 attempt。
func (repository *Repository) MarkRunning(ctx context.Context, jobID string, retry bool, updatedAt int64) error {
	return repository.updateJob(ctx, func(tx *gorm.DB) error {
		updates := map[string]any{"state": "running", "last_error_code": nil, "updated_at": updatedAt}
		query := tx.WithContext(ctx).Model(&models.DeletionJob{}).Where("id = ? AND state IN ?", jobID, []string{"pending", "failed"})
		if retry {
			updates["attempt_count"] = gorm.Expr("attempt_count + 1")
		}
		result := query.Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrActiveJob
		}
		return nil
	})
}

// SaveRemaining 在单个项目完成后短事务保存仍待处理项目。
func (repository *Repository) SaveRemaining(ctx context.Context, jobID string, remaining []FailedItem, updatedAt int64) error {
	data, err := json.Marshal(remaining)
	if err != nil {
		return err
	}
	return repository.updateJob(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.DeletionJob{}).Where("id = ? AND state = 'running'", jobID).
			Updates(map[string]any{"failed_items_json": string(data), "updated_at": updatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrActiveJob
		}
		return nil
	})
}

// MarkFailed 持久化安全失败清单和稳定错误码。
func (repository *Repository) MarkFailed(ctx context.Context, jobID string, remaining []FailedItem, code string, updatedAt int64) error {
	data, err := json.Marshal(remaining)
	if err != nil {
		return err
	}
	return repository.updateJob(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.DeletionJob{}).Where("id = ?", jobID).
			Updates(map[string]any{"state": "failed", "failed_items_json": string(data), "last_error_code": code, "updated_at": updatedAt})
		return result.Error
	})
}

// CompleteRecording 标记任务完成并把本场全部未删除音频资产设为 deleted。
func (repository *Repository) CompleteRecording(ctx context.Context, jobID string, meetingID string, updatedAt int64) error {
	return repository.updateJob(ctx, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&models.AudioAsset{}).
			Where("meeting_id = ? AND state <> 'deleted'", meetingID).
			Updates(map[string]any{"state": "deleted", "updated_at": updatedAt}).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&models.DeletionJob{}).Where("id = ?", jobID).
			Updates(map[string]any{"state": "completed", "failed_items_json": nil, "last_error_code": nil, "updated_at": updatedAt}).Error
	})
}

// CompleteMeeting 在全部文件删除后先移除 job，再删除会议；会议号序列表不受影响。
func (repository *Repository) CompleteMeeting(ctx context.Context, jobID string, meetingID string) error {
	return repository.updateJob(ctx, func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Where("id = ? AND meeting_id = ?", jobID, meetingID).Delete(&models.DeletionJob{}).Error; err != nil {
			return err
		}
		result := tx.WithContext(ctx).Where("id = ?", meetingID).Delete(&models.Meeting{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// RecoverRunning 把进程遗留 running 任务转为可见失败，不自动继续删除。
func (repository *Repository) RecoverRunning(ctx context.Context, updatedAt int64) error {
	return repository.updateJob(ctx, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&models.DeletionJob{}).Where("state = 'running'").
			Updates(map[string]any{"state": "failed", "last_error_code": "DELETE_INTERRUPTED", "updated_at": updatedAt}).Error
	})
}

// updateJob 在单 writer 短事务中执行状态更新。
func (repository *Repository) updateJob(ctx context.Context, operation func(*gorm.DB) error) error {
	if repository == nil || repository.transactions == nil {
		return fmt.Errorf("删除任务 Repository 不可用")
	}
	return repository.transactions.WithinTransaction(ctx, operation)
}

// jobColumns 返回 deletion_jobs 的显式字段集合。
func jobColumns() []string {
	return []string{"id", "meeting_id", "kind", "state", "target_manifest_json", "failed_items_json", "attempt_count", "last_error_code", "created_at", "updated_at"}
}

// isUniqueConflict 识别 SQLite 唯一索引冲突且不把 SQL 文本暴露到边界。
func isUniqueConflict(err error) bool {
	return err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) || containsUnique(err.Error()))
}

// containsUnique 兼容当前 SQLite driver 的唯一约束错误文本。
func containsUnique(message string) bool {
	for index := 0; index+6 <= len(message); index++ {
		if message[index:index+6] == "UNIQUE" || message[index:index+6] == "unique" {
			return true
		}
	}
	return false
}
