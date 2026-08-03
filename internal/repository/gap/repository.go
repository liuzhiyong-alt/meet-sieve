// Package gap 按明确事务语义持久化会后缺口补转写事实。
package gap

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/internal/infra/database"
	"meet-sieve/models"

	"gorm.io/gorm"
)

var (
	// ErrConflict 表示补转写事实已被其他处理器取得或修改。
	ErrConflict = errors.New("补转写状态冲突")
)

// Repository 使用 reader 查询，并通过 TransactionManager 执行短写事务。
type Repository struct {
	reader       *gorm.DB
	transactions *database.TransactionManager
}

// ClaimAttemptInput 描述原子取得一组缺口执行权的已验证输入。
type ClaimAttemptInput struct {
	Attempt models.GapTranscriptionAttempt
	GapIDs  []string
}

// OverlapRow 是提交前二次冲突检测所需的当前 utterance 最小投影。
type OverlapRow struct {
	ID          string `gorm:"column:id"`
	StartSample int64  `gorm:"column:start_sample"`
	EndSample   int64  `gorm:"column:end_sample"`
}

// NewRepository 创建缺口补转写 Repository。
func NewRepository(reader *gorm.DB, transactions *database.TransactionManager) *Repository {
	return &Repository{reader: reader, transactions: transactions}
}

// ListAttemptGapIDs 按 item_order 返回一次 attempt 的稳定 gap 集合。
func (repository *Repository) ListAttemptGapIDs(ctx context.Context, attemptID string) ([]string, error) {
	if repository == nil || repository.reader == nil || attemptID == "" {
		return nil, fmt.Errorf("读取 attempt gaps：参数无效")
	}
	var items []models.GapTranscriptionAttemptItem
	if err := repository.reader.WithContext(ctx).Where("attempt_id = ?", attemptID).Order("item_order ASC").Find(&items).Error; err != nil {
		return nil, fmt.Errorf("读取 attempt gaps 失败：%w", err)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.GapID)
	}
	return ids, nil
}

// ListOverlaps 返回与任一候选存在正长度交集的当前 utterance。
func (repository *Repository) ListOverlaps(ctx context.Context, meetingID string, candidates []models.Utterance) ([]OverlapRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取补转写冲突：参数无效")
	}
	result := make([]OverlapRow, 0)
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		var rows []OverlapRow
		err := repository.reader.WithContext(ctx).Model(&models.Utterance{}).
			Select("id", "start_sample", "end_sample").
			Where("meeting_id = ? AND start_sample < ? AND end_sample > ?", meetingID, candidate.EndSample, candidate.StartSample).
			Order("start_sample ASC").Order("id ASC").Find(&rows).Error
		if err != nil {
			return nil, fmt.Errorf("读取补转写冲突失败：%w", err)
		}
		for _, row := range rows {
			if _, exists := seen[row.ID]; exists {
				continue
			}
			seen[row.ID] = struct{}{}
			result = append(result, row)
		}
	}
	return result, nil
}

// ClaimGapAttempt 原子取得 eligible gap 并创建唯一活动尝试。
func (repository *Repository) ClaimGapAttempt(ctx context.Context, input ClaimAttemptInput) error {
	if repository == nil || repository.transactions == nil || !validClaimInput(input) {
		return fmt.Errorf("取得补转写执行权：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		eligible, err := isMeetingEligible(ctx, tx, input.Attempt.MeetingID)
		if err != nil {
			return err
		}
		if !eligible {
			return ErrConflict
		}

		// 先比较并切换全部 gap；并发 owner 在这里确定，后续任一步失败都会回滚。
		claim := tx.WithContext(ctx).Model(&models.ASRGap{}).
			Where("meeting_id = ? AND id IN ? AND state IN ?", input.Attempt.MeetingID, input.GapIDs, []string{"pending", "failed"}).
			Updates(map[string]any{
				"state": "processing", "attempt_count": gorm.Expr("attempt_count + 1"),
				"last_error_code": nil, "updated_at": input.Attempt.UpdatedAt,
			})
		if claim.Error != nil {
			return fmt.Errorf("切换缺口处理状态失败：%w", claim.Error)
		}
		if claim.RowsAffected != int64(len(input.GapIDs)) {
			return ErrConflict
		}
		if err := tx.WithContext(ctx).Select(attemptColumns()).Create(&input.Attempt).Error; err != nil {
			return fmt.Errorf("创建补转写尝试失败：%w", err)
		}
		items := buildAttemptItems(input)
		if err := tx.WithContext(ctx).Select(itemColumns()).Create(&items).Error; err != nil {
			return fmt.Errorf("关联补转写缺口失败：%w", err)
		}
		return nil
	})
}

// validClaimInput 校验进入事务前必须固定的补转写身份和范围。
func validClaimInput(input ClaimAttemptInput) bool {
	attempt := input.Attempt
	if attempt.ID == "" || attempt.MeetingID == "" || attempt.AudioAssetID == "" ||
		attempt.Provider != "volcano" || attempt.ProviderRequestID == "" ||
		attempt.RequestSHA256 == "" || attempt.State != "running" || len(input.GapIDs) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(input.GapIDs))
	for _, gapID := range input.GapIDs {
		if gapID == "" {
			return false
		}
		if _, exists := seen[gapID]; exists {
			return false
		}
		seen[gapID] = struct{}{}
	}
	return true
}

// isMeetingEligible 验证会后处理只领取已经安全保存的终态会议。
func isMeetingEligible(ctx context.Context, tx *gorm.DB, meetingID string) (bool, error) {
	var count int64
	err := tx.WithContext(ctx).Model(&models.Meeting{}).
		Where("id = ? AND lifecycle_state IN ? AND local_save_state = 'saved'", meetingID, []string{"ended", "interrupted"}).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("校验补转写会议状态失败：%w", err)
	}
	return count == 1, nil
}

// buildAttemptItems 按输入顺序生成稳定的 attempt-gap 关联。
func buildAttemptItems(input ClaimAttemptInput) []models.GapTranscriptionAttemptItem {
	items := make([]models.GapTranscriptionAttemptItem, 0, len(input.GapIDs))
	for order, gapID := range input.GapIDs {
		items = append(items, models.GapTranscriptionAttemptItem{
			AttemptID: input.Attempt.ID, GapID: gapID, ItemOrder: order, CreatedAt: input.Attempt.CreatedAt,
		})
	}
	return items
}

// attemptColumns 返回 attempt 写入允许使用的显式列。
func attemptColumns() []string {
	return []string{
		"id", "meeting_id", "audio_asset_id", "provider", "provider_request_id",
		"core_start_sample", "core_end_sample", "audio_start_sample", "audio_end_sample",
		"state", "attempt_no", "request_sha256", "response_json", "provider_log_id_suffix",
		"last_error_code", "started_at", "ended_at", "created_at", "updated_at",
	}
}

// itemColumns 返回 attempt item 写入允许使用的显式列。
func itemColumns() []string {
	return []string{"attempt_id", "gap_id", "item_order", "created_at"}
}
