package people

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// ErrGroupNameConflict 表示活动小组的规范化名称命中数据库唯一约束。
var ErrGroupNameConflict = errors.New("活动小组名称重复")

// GroupRepository 负责小组及其当前成员关系的持久化，不决定事务边界。
type GroupRepository struct {
	reader *gorm.DB
}

// NewGroupRepository 创建小组 Repository，reader 供后续查询操作使用。
func NewGroupRepository(reader *gorm.DB) *GroupRepository {
	return &GroupRepository{reader: reader}
}

// Create 在调用方事务中创建小组及其有序成员关系。
func (repository *GroupRepository) Create(ctx context.Context, tx *gorm.DB, group models.Group, members []models.GroupMember) error {
	if tx == nil {
		return fmt.Errorf("创建小组：事务不能为空")
	}
	if err := tx.WithContext(ctx).Create(&group).Error; err != nil {
		if isGroupNameConflict(err) {
			return ErrGroupNameConflict
		}
		return fmt.Errorf("创建小组记录失败: %w", err)
	}
	if len(members) == 0 {
		return nil
	}
	if err := tx.WithContext(ctx).Create(&members).Error; err != nil {
		return fmt.Errorf("创建小组成员关系失败: %w", err)
	}
	return nil
}

// Update 更新小组基础资料并用提交顺序替换全部当前成员关系。
func (repository *GroupRepository) Update(ctx context.Context, tx *gorm.DB, group models.Group, members []models.GroupMember) (models.Group, bool, error) {
	if tx == nil {
		return models.Group{}, false, fmt.Errorf("修改小组：事务不能为空")
	}
	result := tx.WithContext(ctx).
		Model(&models.Group{}).
		Where("id = ? AND archived_at IS NULL", group.ID).
		Updates(map[string]any{
			"name":                group.Name,
			"name_normalized":     group.NameNormalized,
			"default_lan_enabled": group.DefaultLANEnabled,
			"updated_at":          group.UpdatedAt,
		})
	if result.Error != nil {
		if isGroupNameConflict(result.Error) {
			return models.Group{}, false, ErrGroupNameConflict
		}
		return models.Group{}, false, fmt.Errorf("修改小组记录失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Group{}, false, nil
	}
	if err := tx.WithContext(ctx).Where("group_id = ?", group.ID).Delete(&models.GroupMember{}).Error; err != nil {
		return models.Group{}, false, fmt.Errorf("替换小组成员关系时删除旧关系失败: %w", err)
	}
	if len(members) > 0 {
		if err := tx.WithContext(ctx).Create(&members).Error; err != nil {
			return models.Group{}, false, fmt.Errorf("替换小组成员关系失败: %w", err)
		}
	}
	updated, err := findGroupByID(ctx, tx, group.ID)
	if err != nil {
		return models.Group{}, false, err
	}
	return updated, true, nil
}

// ListActive 读取全部当前小组及其显式排序成员关系。
func (repository *GroupRepository) ListActive(ctx context.Context) ([]models.Group, map[string][]models.GroupMember, error) {
	if repository.reader == nil {
		return nil, nil, fmt.Errorf("查询小组：数据库不能为空")
	}
	var groups []models.Group
	if err := repository.reader.WithContext(ctx).
		Select(groupColumns()).
		Where("archived_at IS NULL").
		Order("created_at DESC").
		Order("id ASC").
		Find(&groups).Error; err != nil {
		return nil, nil, fmt.Errorf("查询小组失败: %w", err)
	}
	if len(groups) == 0 {
		return groups, map[string][]models.GroupMember{}, nil
	}
	groupIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	var members []models.GroupMember
	if err := repository.reader.WithContext(ctx).
		Select(groupMemberColumns()).
		Where("group_id IN ?", groupIDs).
		Order("group_id ASC").
		Order("sort_order ASC").
		Find(&members).Error; err != nil {
		return nil, nil, fmt.Errorf("查询小组成员关系失败: %w", err)
	}
	groupMembers := make(map[string][]models.GroupMember, len(groups))
	for _, member := range members {
		groupMembers[member.GroupID] = append(groupMembers[member.GroupID], member)
	}
	return groups, groupMembers, nil
}

// GetActiveByID 读取单个当前小组及其显式排序成员关系。
func (repository *GroupRepository) GetActiveByID(ctx context.Context, groupID string) (models.Group, []models.GroupMember, bool, error) {
	if repository.reader == nil {
		return models.Group{}, nil, false, fmt.Errorf("查询小组：数据库不能为空")
	}
	var group models.Group
	err := repository.reader.WithContext(ctx).Select(groupColumns()).
		Where("id = ? AND archived_at IS NULL", groupID).Take(&group).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Group{}, nil, false, nil
	}
	if err != nil {
		return models.Group{}, nil, false, fmt.Errorf("查询小组失败: %w", err)
	}
	var members []models.GroupMember
	if err := repository.reader.WithContext(ctx).Select(groupMemberColumns()).
		Where("group_id = ?", groupID).Order("sort_order ASC").Find(&members).Error; err != nil {
		return models.Group{}, nil, false, fmt.Errorf("查询小组成员关系失败: %w", err)
	}
	return group, members, true, nil
}

// Delete 在调用方事务中删除小组及其当前成员关系，不触碰成员资料。
func (repository *GroupRepository) Delete(ctx context.Context, tx *gorm.DB, groupID string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("删除小组：事务不能为空")
	}
	if err := tx.WithContext(ctx).Where("group_id = ?", groupID).Delete(&models.GroupMember{}).Error; err != nil {
		return false, fmt.Errorf("删除小组成员关系失败: %w", err)
	}
	result := tx.WithContext(ctx).Where("id = ?", groupID).Delete(&models.Group{})
	if result.Error != nil {
		return false, fmt.Errorf("删除小组记录失败: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// isGroupNameConflict 判断 SQLite 返回的活动小组名称部分唯一索引冲突。
func isGroupNameConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: groups.name_normalized")
}

// groupColumns 集中维护 groups 表读取列，避免查询依赖 SELECT *。
func groupColumns() []string {
	return []string{"id", "name", "name_normalized", "default_lan_enabled", "created_at", "updated_at", "archived_at"}
}

// groupMemberColumns 集中维护 group_members 表读取列。
func groupMemberColumns() []string {
	return []string{"id", "group_id", "member_id", "sort_order", "created_at", "updated_at"}
}

// findGroupByID 在调用方事务内读取更新后的完整小组字段。
func findGroupByID(ctx context.Context, tx *gorm.DB, groupID string) (models.Group, error) {
	var group models.Group
	if err := tx.WithContext(ctx).Select(groupColumns()).Where("id = ?", groupID).Take(&group).Error; err != nil {
		return models.Group{}, fmt.Errorf("读取小组记录失败: %w", err)
	}
	return group, nil
}
