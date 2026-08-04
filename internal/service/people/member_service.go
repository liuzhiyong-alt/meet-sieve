// Package people 编排成员与小组资料的业务流程。
package people

import (
	"context"
	"errors"
	"fmt"
	"strings"

	peopledomain "meet-sieve/internal/domain/people"
	voicedomain "meet-sieve/internal/domain/voice"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MemberServiceDependencies 描述 MemberService 所需的显式基础设施依赖。
type MemberServiceDependencies struct {
	// Repository 负责成员记录的持久化。
	Repository *peoplerepository.MemberRepository
	// Transactions 统一约束 SQLite 写入必须经过事务管理器。
	Transactions *database.TransactionManager
	// IDs 提供可替换的成员 UUID 生成能力。
	IDs identity.Generator
	// Clock 提供可替换的当前时间。
	Clock clock.Clock
	// VoiceModel 返回当前已激活模型；不可用时成员仍可正常管理。
	VoiceModel func() (port.ModelInfo, error)
	// DeleteVoiceSamples 在永久删除成员前清理受控 WAV 与 embedding。
	DeleteVoiceSamples func(context.Context, string) error
}

// CreateMemberInput 是新增成员所需的用户输入。
type CreateMemberInput struct {
	// Name 是成员展示名称。
	Name string
	// Notes 是可选成员备注；空字符串不保存备注。
	Notes string
}

// UpdateMemberInput 是成员资料允许修改的字段。
type UpdateMemberInput struct {
	// Name 是修改后的成员展示名称。
	Name string
	// Notes 是修改后的可选成员备注；空字符串会清空备注。
	Notes string
	// Revision 是详情页读取到的 updated_at；零值兼容既有列表内编辑入口。
	Revision int64
}

// MemberDetail 是独立成员路由需要的引用、状态和动作摘要。
type MemberDetail struct {
	Member             peopledomain.Member
	Revision           int64
	GroupCount         int64
	HistoricalMeetings int64
	CanArchive         bool
	CanRestore         bool
	CanDelete          bool
}

// MemberService 编排成员资料的事务型业务操作。
type MemberService struct {
	repository         *peoplerepository.MemberRepository
	transactions       *database.TransactionManager
	ids                identity.Generator
	clock              clock.Clock
	voiceModel         func() (port.ModelInfo, error)
	deleteVoiceSamples func(context.Context, string) error
}

// NewMemberService 创建成员服务；构造阶段不执行数据库写入。
func NewMemberService(dependencies MemberServiceDependencies) *MemberService {
	return &MemberService{
		repository:         dependencies.Repository,
		transactions:       dependencies.Transactions,
		ids:                dependencies.IDs,
		clock:              dependencies.Clock,
		voiceModel:         dependencies.VoiceModel,
		deleteVoiceSamples: dependencies.DeleteVoiceSamples,
	}
}

// CreateMember 创建一个活动成员，并返回与持久化内容一致的领域投影。
func (service *MemberService) CreateMember(ctx context.Context, input CreateMemberInput) (peopledomain.Member, error) {
	name, normalized, err := validateMemberName(input.Name)
	if err != nil {
		return peopledomain.Member{}, err
	}
	member, err := service.buildNewMember(name, normalized, input.Notes)
	if err != nil {
		return peopledomain.Member{}, err
	}
	if service.transactions == nil || service.repository == nil {
		return peopledomain.Member{}, fmt.Errorf("成员服务依赖未初始化")
	}
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.Create(ctx, tx, member)
	}); err != nil {
		return peopledomain.Member{}, mapCreateMemberError(err)
	}
	return mapMember(member), nil
}

// ListActiveMembers 返回当前可用于新小组和会议候选的活动成员。
func (service *MemberService) ListActiveMembers(ctx context.Context) ([]peopledomain.Member, error) {
	if service.repository == nil {
		return nil, fmt.Errorf("成员服务 Repository 未初始化")
	}
	members, err := service.repository.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	return service.mapMembersWithVoice(ctx, members)
}

// GetMember 返回单个活动成员的当前资料。
func (service *MemberService) GetMember(ctx context.Context, memberID string) (peopledomain.Member, error) {
	if service.repository == nil {
		return peopledomain.Member{}, fmt.Errorf("成员服务 Repository 未初始化")
	}
	member, found, err := service.repository.GetActiveByID(ctx, memberID)
	if err != nil {
		return peopledomain.Member{}, err
	}
	if !found {
		return peopledomain.Member{}, apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.get"))
	}
	result, err := service.mapMembersWithVoice(ctx, []models.Member{member})
	if err != nil {
		return peopledomain.Member{}, err
	}
	return result[0], nil
}

// GetMemberDetail 返回活动或归档成员的真实引用和能力投影。
func (service *MemberService) GetMemberDetail(ctx context.Context, memberID string) (MemberDetail, error) {
	if service.repository == nil {
		return MemberDetail{}, fmt.Errorf("成员服务 Repository 未初始化")
	}
	member, found, err := service.repository.GetByID(ctx, memberID)
	if err != nil || !found {
		if err != nil {
			return MemberDetail{}, err
		}
		return MemberDetail{}, apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.detail"))
	}
	mapped, err := service.mapMembersWithVoice(ctx, []models.Member{member})
	if err != nil {
		return MemberDetail{}, err
	}
	groupCount, meetingCount, err := service.repository.CountReferences(ctx, memberID)
	if err != nil {
		return MemberDetail{}, err
	}
	archived := member.ArchivedAt != nil
	return MemberDetail{Member: mapped[0], Revision: member.UpdatedAt, GroupCount: groupCount, HistoricalMeetings: meetingCount, CanArchive: !archived, CanRestore: archived, CanDelete: meetingCount == 0}, nil
}

// UpdateMember 修改活动成员的名称和备注。
func (service *MemberService) UpdateMember(ctx context.Context, memberID string, input UpdateMemberInput) (peopledomain.Member, error) {
	name, normalized, err := validateMemberName(input.Name)
	if err != nil {
		return peopledomain.Member{}, err
	}
	if service.transactions == nil || service.repository == nil || service.clock == nil {
		return peopledomain.Member{}, fmt.Errorf("成员服务依赖未初始化")
	}
	var updated models.Member
	var found bool
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		updated, found, err = service.repository.Update(ctx, tx, memberID, name, normalized, optionalNotes(input.Notes), input.Revision, service.clock.Now().UnixMilli())
		return err
	}); err != nil {
		return peopledomain.Member{}, mapCreateMemberError(err)
	}
	if !found {
		if input.Revision > 0 {
			return peopledomain.Member{}, apperr.Biz(apperr.CodePeopleRevisionConflict, apperr.WithOp("people.member.update"))
		}
		return peopledomain.Member{}, apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.update"))
	}
	return mapMember(updated), nil
}

// ArchiveMember 归档成员，并在同一事务内移除其当前小组关系。
func (service *MemberService) ArchiveMember(ctx context.Context, memberID string) error {
	if service.transactions == nil || service.repository == nil || service.clock == nil {
		return fmt.Errorf("成员服务依赖未初始化")
	}
	archivedAt := service.clock.Now().UnixMilli()
	var archived bool
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var err error
		archived, err = service.repository.Archive(ctx, tx, memberID, archivedAt)
		return err
	}); err != nil {
		return err
	}
	if !archived {
		return apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.archive"))
	}
	return nil
}

// RestoreMember 恢复归档成员；名称已被活动成员占用时返回稳定冲突。
func (service *MemberService) RestoreMember(ctx context.Context, memberID string) error {
	if service.transactions == nil || service.repository == nil || service.clock == nil {
		return fmt.Errorf("成员服务依赖未初始化")
	}
	var restored bool
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var restoreErr error
		restored, restoreErr = service.repository.Restore(ctx, tx, memberID, service.clock.Now().UnixMilli())
		return restoreErr
	})
	if errors.Is(err, peoplerepository.ErrMemberNameConflict) {
		return apperr.Biz(apperr.CodeMemberNameConflict, apperr.WithOp("people.member.restore"))
	}
	if err != nil {
		return err
	}
	if !restored {
		return apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.restore"))
	}
	return nil
}

// DeleteAllVoiceSamples 删除活动成员全部显式样本，但保留成员和历史会议。
func (service *MemberService) DeleteAllVoiceSamples(ctx context.Context, memberID string) error {
	if _, found, err := service.repository.GetActiveByID(ctx, memberID); err != nil {
		return err
	} else if !found {
		return apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.delete_voice"))
	}
	if service.deleteVoiceSamples == nil {
		return fmt.Errorf("声纹删除服务未初始化")
	}
	return service.deleteVoiceSamples(ctx, memberID)
}

// DeleteMember 删除当前成员；有历史引用时保留内部墓碑，没有引用时执行硬删除。
func (service *MemberService) DeleteMember(ctx context.Context, memberID string) error {
	if service.transactions == nil || service.repository == nil || service.clock == nil {
		return fmt.Errorf("成员服务依赖未初始化")
	}
	if _, found, err := service.repository.GetActiveByID(ctx, memberID); err != nil {
		return err
	} else if !found {
		return apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.delete"))
	}
	if err := service.cleanVoiceSamples(ctx, memberID); err != nil {
		return err
	}
	var persisted bool
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		referenced, err := service.resolveDeletionMode(ctx, tx, memberID)
		if err != nil {
			return err
		}
		persisted, err = service.persistMemberDeletion(ctx, tx, memberID, referenced)
		return err
	}); err != nil {
		return err
	}
	if !persisted {
		return apperr.Biz(apperr.CodeMemberNotFound, apperr.WithOp("people.member.delete"))
	}
	return nil
}

// cleanVoiceSamples 在 SQLite 写事务外清理受控声纹文件；失败时不改变成员和小组关系。
func (service *MemberService) cleanVoiceSamples(ctx context.Context, memberID string) error {
	if service.deleteVoiceSamples == nil {
		return nil
	}
	return service.deleteVoiceSamples(ctx, memberID)
}

// resolveDeletionMode 在最终写事务内重新判断是否必须保留历史引用墓碑。
func (service *MemberService) resolveDeletionMode(ctx context.Context, tx *gorm.DB, memberID string) (bool, error) {
	return service.repository.HasHistoricalReference(ctx, tx, memberID)
}

// persistMemberDeletion 按历史引用结果执行墓碑或硬删除，并统一移除当前小组关系。
func (service *MemberService) persistMemberDeletion(ctx context.Context, tx *gorm.DB, memberID string, referenced bool) (bool, error) {
	if referenced {
		return service.repository.Archive(ctx, tx, memberID, service.clock.Now().UnixMilli())
	}
	return service.repository.DeleteUnreferenced(ctx, tx, memberID)
}

// mapCreateMemberError 将 Repository 的稳定冲突转换为应用边界可识别的业务错误。
func mapCreateMemberError(err error) error {
	if errors.Is(err, peoplerepository.ErrMemberNameConflict) {
		return apperr.Biz(apperr.CodeMemberNameConflict, apperr.WithOp("people.member.create"))
	}
	return err
}

// validateMemberName 同时保留用户展示名称与数据库唯一键，避免两者规则漂移。
func validateMemberName(raw string) (string, string, error) {
	name := strings.TrimSpace(raw)
	normalized, err := peopledomain.NormalizeName(name)
	if err != nil {
		return "", "", err
	}
	return name, normalized, nil
}

// optionalNotes 将空字符串映射为数据库 NULL，避免无意义的空备注状态。
func optionalNotes(notes string) *string {
	if notes == "" {
		return nil
	}
	return &notes
}

// buildNewMember 根据已校验输入组装待持久化成员。
func (service *MemberService) buildNewMember(name string, normalized string, notes string) (models.Member, error) {
	if service.ids == nil || service.clock == nil {
		return models.Member{}, fmt.Errorf("成员服务生成器或时钟未初始化")
	}
	id := service.ids.New()
	if _, err := uuid.Parse(id); err != nil {
		return models.Member{}, fmt.Errorf("生成成员 UUID 失败: %w", err)
	}
	now := service.clock.Now().UnixMilli()
	member := models.Member{ID: id, Name: name, NameNormalized: normalized, CreatedAt: now, UpdatedAt: now}
	member.Notes = optionalNotes(notes)
	return member, nil
}

// mapMember 将显式 ORM 映射转换为不泄漏存储实现的领域对象。
func mapMember(member models.Member) peopledomain.Member {
	return peopledomain.Member{
		ID:             member.ID,
		Name:           member.Name,
		NameNormalized: member.NameNormalized,
		Notes:          member.Notes,
		VoiceSummary:   peopledomain.VoiceSummary{Readiness: voicedomain.ReadinessNotEnrolled},
		CreatedAt:      member.CreatedAt,
		UpdatedAt:      member.UpdatedAt,
		ArchivedAt:     member.ArchivedAt,
	}
}

// mapMembersWithVoice 将成员与样本聚合合并，并在模型未锁定时保持 unavailable 语义。
func (service *MemberService) mapMembersWithVoice(ctx context.Context, members []models.Member) ([]peopledomain.Member, error) {
	memberIDs := make([]string, 0, len(members))
	for _, member := range members {
		memberIDs = append(memberIDs, member.ID)
	}
	var identity *peoplerepository.VoiceModelIdentity
	if service.voiceModel != nil {
		if model, modelErr := service.voiceModel(); modelErr == nil {
			identity = &peoplerepository.VoiceModelIdentity{ID: model.ID, Version: model.Version, SHA256: model.SHA256, Dimension: model.Dimension}
		}
	}
	summaries, err := service.repository.ListVoiceSummaries(ctx, memberIDs, identity)
	if err != nil {
		return nil, err
	}
	result := mapMembers(members)
	for index := range result {
		record := summaries[result[index].ID]
		result[index].VoiceSummary = mapVoiceSummary(record, identity != nil)
	}
	return result, nil
}

// mapVoiceSummary 在模型身份尚未通过门禁时不产生 ready 或 rebuild_required 假状态。
func mapVoiceSummary(record peoplerepository.VoiceSummaryRecord, modelAvailable bool) peopledomain.VoiceSummary {
	readiness := voicedomain.ReadinessNotEnrolled
	if record.ProcessingCount > 0 {
		readiness = voicedomain.ReadinessProcessing
	} else if record.AcceptedCount > 0 {
		switch {
		case !modelAvailable:
			readiness = voicedomain.ReadinessUnavailable
		case record.CurrentEmbeddingCount < record.AcceptedCount:
			readiness = voicedomain.ReadinessRebuildRequired
		default:
			readiness = voicedomain.ReadinessReady
		}
	}
	return peopledomain.VoiceSummary{
		AcceptedSampleCount: record.AcceptedCount,
		RejectedSampleCount: record.RejectedCount,
		Readiness:           readiness,
	}
}

// mapMembers 批量转换 Repository 返回的显式 ORM 映射。
func mapMembers(members []models.Member) []peopledomain.Member {
	result := make([]peopledomain.Member, 0, len(members))
	for _, member := range members {
		result = append(result, mapMember(member))
	}
	return result
}
