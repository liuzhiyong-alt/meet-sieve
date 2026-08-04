// Package people 提供成员与小组的 SQLite 持久化操作。
package people

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// ErrMemberNameConflict 表示活动成员的规范化名称命中数据库唯一约束。
var ErrMemberNameConflict = errors.New("活动成员名称重复")

// MemberRepository 负责成员持久化，不决定事务边界。
type MemberRepository struct {
	reader *gorm.DB
}

// VoiceSummaryRecord 是按成员聚合的声纹样本存储结果。
type VoiceSummaryRecord struct {
	MemberID              string `gorm:"column:member_id"`
	AcceptedCount         int    `gorm:"column:accepted_count"`
	RejectedCount         int    `gorm:"column:rejected_count"`
	ProcessingCount       int    `gorm:"column:processing_count"`
	CurrentEmbeddingCount int    `gorm:"column:current_embedding_count"`
}

// VoiceModelIdentity 是 Repository 查询当前 embedding 所需的最小模型四元组。
type VoiceModelIdentity struct {
	ID        string
	Version   string
	SHA256    string
	Dimension int
}

// NewMemberRepository 创建成员 Repository，reader 仅用于后续只读查询。
func NewMemberRepository(reader *gorm.DB) *MemberRepository {
	return &MemberRepository{reader: reader}
}

// Create 在调用方提供的事务中创建成员记录。
func (repository *MemberRepository) Create(ctx context.Context, tx *gorm.DB, member models.Member) error {
	if tx == nil {
		return fmt.Errorf("创建成员：事务不能为空")
	}
	if err := tx.WithContext(ctx).Create(&member).Error; err != nil {
		if isMemberNameConflict(err) {
			return ErrMemberNameConflict
		}
		return fmt.Errorf("创建成员记录失败: %w", err)
	}
	return nil
}

// Update 修改活动成员允许编辑的字段，并返回更新后的持久化记录。
func (repository *MemberRepository) Update(ctx context.Context, tx *gorm.DB, memberID string, name string, normalized string, notes *string, expectedRevision int64, updatedAt int64) (models.Member, bool, error) {
	if tx == nil {
		return models.Member{}, false, fmt.Errorf("修改成员：事务不能为空")
	}
	query := tx.WithContext(ctx).
		Model(&models.Member{}).
		Where("id = ? AND archived_at IS NULL", memberID)
	if expectedRevision > 0 {
		query = query.Where("updated_at = ?", expectedRevision)
	}
	result := query.
		Updates(map[string]any{
			"name":            name,
			"name_normalized": normalized,
			"notes":           notes,
			"updated_at":      updatedAt,
		})
	if result.Error != nil {
		if isMemberNameConflict(result.Error) {
			return models.Member{}, false, ErrMemberNameConflict
		}
		return models.Member{}, false, fmt.Errorf("修改成员记录失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Member{}, false, nil
	}
	member, err := findMemberByID(ctx, tx, memberID)
	if err != nil {
		return models.Member{}, false, err
	}
	return member, true, nil
}

// ListActive 读取未归档成员；排序语义由后续已确认的 UI 契约补充。
func (repository *MemberRepository) ListActive(ctx context.Context) ([]models.Member, error) {
	if repository.reader == nil {
		return nil, fmt.Errorf("查询活动成员：数据库不能为空")
	}
	var members []models.Member
	if err := repository.reader.WithContext(ctx).
		Select(memberColumns()).
		Where("archived_at IS NULL").
		Order("created_at DESC").
		Order("id ASC").
		Find(&members).Error; err != nil {
		return nil, fmt.Errorf("查询活动成员失败: %w", err)
	}
	return members, nil
}

// GetActiveByID 读取单个活动成员。
func (repository *MemberRepository) GetActiveByID(ctx context.Context, memberID string) (models.Member, bool, error) {
	if repository.reader == nil {
		return models.Member{}, false, fmt.Errorf("查询活动成员：数据库不能为空")
	}
	var member models.Member
	err := repository.reader.WithContext(ctx).Select(memberColumns()).
		Where("id = ? AND archived_at IS NULL", memberID).Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Member{}, false, nil
	}
	if err != nil {
		return models.Member{}, false, fmt.Errorf("查询活动成员失败: %w", err)
	}
	return member, true, nil
}

// GetByID 读取活动或归档成员，供独立详情路由恢复状态。
func (repository *MemberRepository) GetByID(ctx context.Context, memberID string) (models.Member, bool, error) {
	if repository == nil || repository.reader == nil || memberID == "" {
		return models.Member{}, false, fmt.Errorf("查询成员详情：参数无效")
	}
	var member models.Member
	err := repository.reader.WithContext(ctx).Select(memberColumns()).Where("id = ?", memberID).Take(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Member{}, false, nil
	}
	if err != nil {
		return models.Member{}, false, fmt.Errorf("查询成员详情失败: %w", err)
	}
	return member, true, nil
}

// CountReferences 返回当前小组关系数和不可变历史会议引用数。
func (repository *MemberRepository) CountReferences(ctx context.Context, memberID string) (int64, int64, error) {
	if repository == nil || repository.reader == nil || memberID == "" {
		return 0, 0, fmt.Errorf("统计成员引用：参数无效")
	}
	var groupCount, meetingCount int64
	if err := repository.reader.WithContext(ctx).Table("group_members").Where("member_id = ?", memberID).Count(&groupCount).Error; err != nil {
		return 0, 0, fmt.Errorf("统计成员小组失败: %w", err)
	}
	if err := repository.reader.WithContext(ctx).Table("meeting_participants").Where("member_id = ?", memberID).Distinct("meeting_id").Count(&meetingCount).Error; err != nil {
		return 0, 0, fmt.Errorf("统计成员历史会议失败: %w", err)
	}
	return groupCount, meetingCount, nil
}

// Restore 把归档成员恢复为活动状态；活动名称唯一约束仍由数据库执行。
func (repository *MemberRepository) Restore(ctx context.Context, tx *gorm.DB, memberID string, updatedAt int64) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("恢复成员：事务不能为空")
	}
	result := tx.WithContext(ctx).Model(&models.Member{}).Where("id = ? AND archived_at IS NOT NULL", memberID).
		Updates(map[string]any{"archived_at": nil, "updated_at": updatedAt})
	if result.Error != nil {
		if isMemberNameConflict(result.Error) {
			return false, ErrMemberNameConflict
		}
		return false, fmt.Errorf("恢复成员记录失败: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// ListVoiceSummaries 聚合指定活动成员的样本状态，不读取音频或 embedding 正文。
func (repository *MemberRepository) ListVoiceSummaries(ctx context.Context, memberIDs []string, model *VoiceModelIdentity) (map[string]VoiceSummaryRecord, error) {
	if repository.reader == nil {
		return nil, fmt.Errorf("查询成员声纹汇总：数据库不能为空")
	}
	if len(memberIDs) == 0 {
		return map[string]VoiceSummaryRecord{}, nil
	}
	var records []VoiceSummaryRecord
	currentEmbeddingSQL := "0 AS current_embedding_count"
	arguments := []any{}
	if model != nil {
		currentEmbeddingSQL = `SUM(CASE WHEN quality_state = 'accepted' AND EXISTS (
			SELECT 1 FROM voice_embeddings e WHERE e.voice_sample_id = voice_samples.id
			AND e.model_id = ? AND e.model_version = ? AND e.model_sha256 = ? AND e.dimension = ?
		) THEN 1 ELSE 0 END) AS current_embedding_count`
		arguments = append(arguments, model.ID, model.Version, model.SHA256, model.Dimension)
	}
	selection := `member_id,
			SUM(CASE WHEN quality_state = 'accepted' THEN 1 ELSE 0 END) AS accepted_count,
			SUM(CASE WHEN quality_state = 'rejected' THEN 1 ELSE 0 END) AS rejected_count,
			SUM(CASE WHEN processing_state = 'processing' THEN 1 ELSE 0 END) AS processing_count, ` + currentEmbeddingSQL
	err := repository.reader.WithContext(ctx).Table("voice_samples").
		Select(selection, arguments...).
		Where("member_id IN ?", memberIDs).Group("member_id").Scan(&records).Error
	if err != nil {
		return nil, fmt.Errorf("查询成员声纹汇总失败: %w", err)
	}
	result := make(map[string]VoiceSummaryRecord, len(records))
	for _, record := range records {
		result[record.MemberID] = record
	}
	return result, nil
}

// ListActiveByIDs 在调用方事务中读取指定的活动成员，用于小组提交校验。
func (repository *MemberRepository) ListActiveByIDs(ctx context.Context, tx *gorm.DB, memberIDs []string) ([]models.Member, error) {
	if tx == nil {
		return nil, fmt.Errorf("查询指定活动成员：事务不能为空")
	}
	if len(memberIDs) == 0 {
		return []models.Member{}, nil
	}
	var members []models.Member
	if err := tx.WithContext(ctx).
		Select(memberColumns()).
		Where("id IN ? AND archived_at IS NULL", memberIDs).
		Find(&members).Error; err != nil {
		return nil, fmt.Errorf("查询指定活动成员失败: %w", err)
	}
	return members, nil
}

// Archive 将活动成员标记为归档，并移除所有当前小组关系。
// 调用方必须提供同一事务，以确保成员状态与关系不会部分提交。
func (repository *MemberRepository) Archive(ctx context.Context, tx *gorm.DB, memberID string, archivedAt int64) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("归档成员：事务不能为空")
	}
	if err := tx.WithContext(ctx).Where("member_id = ?", memberID).Delete(&models.GroupMember{}).Error; err != nil {
		return false, fmt.Errorf("删除成员小组关系失败: %w", err)
	}
	result := tx.WithContext(ctx).
		Model(&models.Member{}).
		Where("id = ? AND archived_at IS NULL", memberID).
		Updates(map[string]any{"archived_at": archivedAt, "updated_at": archivedAt})
	if result.Error != nil {
		return false, fmt.Errorf("归档成员记录失败: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// HasHistoricalReference 判断成员是否已被不可改写的会议历史引用。
func (repository *MemberRepository) HasHistoricalReference(ctx context.Context, tx *gorm.DB, memberID string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("检查成员历史引用：事务不能为空")
	}
	for _, reference := range memberHistoricalReferences() {
		found, err := hasReference(ctx, tx, reference.table, reference.column, memberID)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// DeleteUnreferenced 删除没有历史引用的成员及其当前小组关系。
// 声纹样本与 embedding 由已声明的外键级联清理。
func (repository *MemberRepository) DeleteUnreferenced(ctx context.Context, tx *gorm.DB, memberID string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("删除成员：事务不能为空")
	}
	if err := tx.WithContext(ctx).Where("member_id = ?", memberID).Delete(&models.GroupMember{}).Error; err != nil {
		return false, fmt.Errorf("删除成员小组关系失败: %w", err)
	}
	result := tx.WithContext(ctx).Where("id = ?", memberID).Delete(&models.Member{})
	if result.Error != nil {
		return false, fmt.Errorf("删除成员记录失败: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// memberColumns 集中维护 members 表的读取列，避免查询依赖 SELECT *。
func memberColumns() []string {
	return []string{"id", "name", "name_normalized", "notes", "created_at", "updated_at", "archived_at"}
}

// findMemberByID 在调用方事务内读取单个成员的完整显式字段。
func findMemberByID(ctx context.Context, tx *gorm.DB, memberID string) (models.Member, error) {
	var member models.Member
	if err := tx.WithContext(ctx).Select(memberColumns()).Where("id = ?", memberID).Take(&member).Error; err != nil {
		return models.Member{}, fmt.Errorf("读取成员记录失败: %w", err)
	}
	return member, nil
}

// isMemberNameConflict 判断 SQLite 返回的活动名称部分唯一索引冲突。
func isMemberNameConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: members.name_normalized")
}

// historicalReference 描述引用成员且阻止永久删除的历史列。
type historicalReference struct {
	table  string
	column string
}

// memberHistoricalReferences 返回当前 schema 直接保存 member ID 的历史引用范围。
// Step 5 的 utterance/cluster 只引用 meeting_participant，由 participant 的 member_id 统一阻止删除。
func memberHistoricalReferences() []historicalReference {
	return []historicalReference{
		{table: "meeting_participants", column: "member_id"},
		{table: "messages", column: "member_id"},
	}
}

// hasReference 使用短路查询确认单个受控历史表中是否存在成员引用。
func hasReference(ctx context.Context, tx *gorm.DB, table string, column string, memberID string) (bool, error) {
	var row struct {
		Value int
	}
	result := tx.WithContext(ctx).Table(table).Select("1 AS value").Where(column+" = ?", memberID).Limit(1).Find(&row)
	if result.Error != nil {
		return false, fmt.Errorf("查询 %s 成员引用失败: %w", table, result.Error)
	}
	return result.RowsAffected == 1, nil
}
