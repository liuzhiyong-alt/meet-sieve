package people

import (
	"context"
	"errors"
	"fmt"
	"strings"

	peopledomain "meet-sieve/internal/domain/people"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	peoplerepository "meet-sieve/internal/repository/people"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GroupServiceDependencies 描述 GroupService 所需的显式基础设施依赖。
type GroupServiceDependencies struct {
	// Repository 负责小组和成员关系持久化。
	Repository *peoplerepository.GroupRepository
	// Members 负责读取活动成员以校验小组提交。
	Members *peoplerepository.MemberRepository
	// Transactions 统一约束 SQLite 写入必须经过事务管理器。
	Transactions *database.TransactionManager
	// IDs 提供可替换的小组和关系 UUID 生成能力。
	IDs identity.Generator
	// Clock 提供可替换的当前时间。
	Clock clock.Clock
}

// CreateGroupInput 是创建小组所需的用户输入。
type CreateGroupInput struct {
	// Name 是小组展示名称。
	Name string
	// DefaultLANEnabled 是小组的默认访客页开关。
	DefaultLANEnabled bool
	// MemberIDs 按用户明确提交的顺序排列。
	MemberIDs []string
}

// UpdateGroupInput 是修改小组所需的全部当前配置。
type UpdateGroupInput struct {
	// Name 是修改后的展示名称。
	Name string
	// DefaultLANEnabled 是修改后的默认访客页开关。
	DefaultLANEnabled bool
	// MemberIDs 按用户明确提交的顺序排列，并完整替换当前关系。
	MemberIDs []string
	// Revision 是详情页读取到的 updated_at；零值兼容既有列表内编辑入口。
	Revision int64
}

// GroupService 编排小组资料的事务型业务操作。
type GroupService struct {
	repository   *peoplerepository.GroupRepository
	members      *peoplerepository.MemberRepository
	transactions *database.TransactionManager
	ids          identity.Generator
	clock        clock.Clock
}

// NewGroupService 创建小组服务；构造阶段不执行数据库写入。
func NewGroupService(dependencies GroupServiceDependencies) *GroupService {
	return &GroupService{
		repository:   dependencies.Repository,
		members:      dependencies.Members,
		transactions: dependencies.Transactions,
		ids:          dependencies.IDs,
		clock:        dependencies.Clock,
	}
}

// CreateGroup 创建小组及其保持用户提交顺序的活动成员关系。
func (service *GroupService) CreateGroup(ctx context.Context, input CreateGroupInput) (peopledomain.Group, error) {
	name, normalized, err := validateGroupName(input.Name)
	if err != nil {
		return peopledomain.Group{}, err
	}
	if err := validateSubmittedMemberIDs(input.MemberIDs); err != nil {
		return peopledomain.Group{}, err
	}
	group, members, err := service.buildNewGroup(name, normalized, input)
	if err != nil {
		return peopledomain.Group{}, err
	}
	if service.repository == nil || service.members == nil || service.transactions == nil {
		return peopledomain.Group{}, fmt.Errorf("小组服务依赖未初始化")
	}
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if err := service.ensureActiveMembers(ctx, tx, input.MemberIDs, "people.group.create"); err != nil {
			return err
		}
		return service.repository.Create(ctx, tx, group, members)
	}); err != nil {
		return peopledomain.Group{}, mapGroupError(err, "people.group.create")
	}
	return mapGroup(group, members), nil
}

// UpdateGroup 修改小组资料，并在一个短事务中替换当前成员关系。
func (service *GroupService) UpdateGroup(ctx context.Context, groupID string, input UpdateGroupInput) (peopledomain.Group, error) {
	name, normalized, err := validateGroupName(input.Name)
	if err != nil {
		return peopledomain.Group{}, err
	}
	if err := validateSubmittedMemberIDs(input.MemberIDs); err != nil {
		return peopledomain.Group{}, apperr.Biz(apperr.CodeGroupMemberInvalid, apperr.WithOp("people.group.update"))
	}
	group, members, err := service.buildUpdatedGroup(groupID, name, normalized, input)
	if err != nil {
		return peopledomain.Group{}, err
	}
	if service.repository == nil || service.members == nil || service.transactions == nil {
		return peopledomain.Group{}, fmt.Errorf("小组服务依赖未初始化")
	}
	var persisted models.Group
	var updated bool
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if err := service.ensureActiveMembers(ctx, tx, input.MemberIDs, "people.group.update"); err != nil {
			return err
		}
		var updateErr error
		persisted, updated, updateErr = service.repository.Update(ctx, tx, group, members, input.Revision)
		return updateErr
	}); err != nil {
		return peopledomain.Group{}, mapGroupError(err, "people.group.update")
	}
	if !updated {
		if input.Revision > 0 {
			return peopledomain.Group{}, apperr.Biz(apperr.CodePeopleRevisionConflict, apperr.WithOp("people.group.update"))
		}
		return peopledomain.Group{}, apperr.Biz(apperr.CodeGroupNotFound, apperr.WithOp("people.group.update"))
	}
	return mapGroup(persisted, members), nil
}

// ListGroups 返回当前小组及其有序成员关系。
func (service *GroupService) ListGroups(ctx context.Context) ([]peopledomain.Group, error) {
	if service.repository == nil {
		return nil, fmt.Errorf("小组服务 Repository 未初始化")
	}
	groups, membersByGroup, err := service.repository.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]peopledomain.Group, 0, len(groups))
	for _, group := range groups {
		result = append(result, mapGroup(group, membersByGroup[group.ID]))
	}
	return result, nil
}

// GetGroup 返回单个当前小组及其有序成员关系。
func (service *GroupService) GetGroup(ctx context.Context, groupID string) (peopledomain.Group, error) {
	if service.repository == nil {
		return peopledomain.Group{}, fmt.Errorf("小组服务 Repository 未初始化")
	}
	group, members, found, err := service.repository.GetActiveByID(ctx, groupID)
	if err != nil {
		return peopledomain.Group{}, err
	}
	if !found {
		return peopledomain.Group{}, apperr.Biz(apperr.CodeGroupNotFound, apperr.WithOp("people.group.get"))
	}
	return mapGroup(group, members), nil
}

// DeleteGroup 删除小组及其当前成员关系，不删除成员资料。
func (service *GroupService) DeleteGroup(ctx context.Context, groupID string) error {
	if service.repository == nil || service.transactions == nil {
		return fmt.Errorf("小组服务依赖未初始化")
	}
	var deleted bool
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var err error
		deleted, err = service.repository.Delete(ctx, tx, groupID)
		return err
	}); err != nil {
		return err
	}
	if !deleted {
		return apperr.Biz(apperr.CodeGroupNotFound, apperr.WithOp("people.group.delete"))
	}
	return nil
}

// ensureActiveMembers 校验提交的所有成员都存在且未归档。
func (service *GroupService) ensureActiveMembers(ctx context.Context, tx *gorm.DB, memberIDs []string, operation string) error {
	activeMembers, err := service.members.ListActiveByIDs(ctx, tx, memberIDs)
	if err != nil {
		return err
	}
	if len(activeMembers) != len(memberIDs) {
		return apperr.Biz(apperr.CodeGroupMemberInvalid, apperr.WithOp(operation))
	}
	return nil
}

// mapGroupError 将 Repository 的稳定约束错误转换为客户端可识别的错误码。
func mapGroupError(err error, operation string) error {
	if errors.Is(err, peoplerepository.ErrGroupNameConflict) {
		return apperr.Biz(apperr.CodeGroupNameConflict, apperr.WithOp(operation))
	}
	return err
}

// validateGroupName 保留展示名称并生成与成员一致的规范化唯一键。
func validateGroupName(raw string) (string, string, error) {
	name := strings.TrimSpace(raw)
	normalized, err := peopledomain.NormalizeName(name)
	if err != nil {
		return "", "", err
	}
	return name, normalized, nil
}

// validateSubmittedMemberIDs 拒绝非法或重复成员，防止关系表约束异常泄漏到业务层。
func validateSubmittedMemberIDs(memberIDs []string) error {
	seen := make(map[string]struct{}, len(memberIDs))
	for _, memberID := range memberIDs {
		if _, err := uuid.Parse(memberID); err != nil {
			return fmt.Errorf("小组成员 ID 不合法: %w", err)
		}
		if _, found := seen[memberID]; found {
			return fmt.Errorf("小组成员重复")
		}
		seen[memberID] = struct{}{}
	}
	return nil
}

// buildNewGroup 预分配小组和成员关系 ID，使事务内不会生成不可追踪的文件或关系。
func (service *GroupService) buildNewGroup(name string, normalized string, input CreateGroupInput) (models.Group, []models.GroupMember, error) {
	if service.ids == nil || service.clock == nil {
		return models.Group{}, nil, fmt.Errorf("小组服务生成器或时钟未初始化")
	}
	groupID := service.ids.New()
	if _, err := uuid.Parse(groupID); err != nil {
		return models.Group{}, nil, fmt.Errorf("生成小组 UUID 失败: %w", err)
	}
	now := service.clock.Now().UnixMilli()
	group := models.Group{ID: groupID, Name: name, NameNormalized: normalized, DefaultLANEnabled: input.DefaultLANEnabled, CreatedAt: now, UpdatedAt: now}
	members := make([]models.GroupMember, 0, len(input.MemberIDs))
	for index, memberID := range input.MemberIDs {
		relationID := service.ids.New()
		if _, err := uuid.Parse(relationID); err != nil {
			return models.Group{}, nil, fmt.Errorf("生成小组成员关系 UUID 失败: %w", err)
		}
		members = append(members, models.GroupMember{ID: relationID, GroupID: groupID, MemberID: memberID, SortOrder: index, CreatedAt: now, UpdatedAt: now})
	}
	return group, members, nil
}

// buildUpdatedGroup 根据已校验输入组装小组更新与替换关系。
func (service *GroupService) buildUpdatedGroup(groupID string, name string, normalized string, input UpdateGroupInput) (models.Group, []models.GroupMember, error) {
	if _, err := uuid.Parse(groupID); err != nil {
		return models.Group{}, nil, fmt.Errorf("小组 ID 不合法: %w", err)
	}
	if service.ids == nil || service.clock == nil {
		return models.Group{}, nil, fmt.Errorf("小组服务生成器或时钟未初始化")
	}
	now := service.clock.Now().UnixMilli()
	group := models.Group{ID: groupID, Name: name, NameNormalized: normalized, DefaultLANEnabled: input.DefaultLANEnabled, UpdatedAt: now}
	members := make([]models.GroupMember, 0, len(input.MemberIDs))
	for index, memberID := range input.MemberIDs {
		relationID := service.ids.New()
		if _, err := uuid.Parse(relationID); err != nil {
			return models.Group{}, nil, fmt.Errorf("生成小组成员关系 UUID 失败: %w", err)
		}
		members = append(members, models.GroupMember{ID: relationID, GroupID: groupID, MemberID: memberID, SortOrder: index, CreatedAt: now, UpdatedAt: now})
	}
	return group, members, nil
}

// mapGroup 将 ORM 小组及关系转换为领域投影。
func mapGroup(group models.Group, members []models.GroupMember) peopledomain.Group {
	projection := peopledomain.Group{
		ID:                group.ID,
		Name:              group.Name,
		NameNormalized:    group.NameNormalized,
		DefaultLANEnabled: group.DefaultLANEnabled,
		CreatedAt:         group.CreatedAt,
		UpdatedAt:         group.UpdatedAt,
		Members:           make([]peopledomain.GroupMember, 0, len(members)),
	}
	for _, member := range members {
		projection.Members = append(projection.Members, peopledomain.GroupMember{MemberID: member.MemberID, SortOrder: member.SortOrder})
	}
	return projection
}
