package meeting

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	meetingdomain "meet-sieve/internal/domain/meeting"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Dependencies 描述会议服务的显式持久化、ID、时间和设备身份依赖。
type Dependencies struct {
	Repository   *meetingrepository.Repository
	Transactions *database.TransactionManager
	IDs          identity.Generator
	Clock        clock.Clock
	DeviceCode   string
}

// CreatePreparingInput 是通过开始页校验后写入会议事实的输入。
type CreatePreparingInput struct {
	MeetingNo                 string
	SuggestedMeetingNo        string
	Subject                   string
	MemberIDs                 []string
	TemporaryParticipantNames []string
	LocalTimezone             string
}

// CreateDraft 是打开开始页时只读生成、不会消耗数据库序号的默认草稿。
type CreateDraft struct {
	SuggestedMeetingNo string
	DefaultSubject     string
}

// Service 编排会议事务型业务流程，不执行音频或文件 I/O。
type Service struct {
	repository   *meetingrepository.Repository
	transactions *database.TransactionManager
	ids          identity.Generator
	clock        clock.Clock
	deviceCode   string
}

// NewService 创建会议服务；构造阶段不访问数据库或文件系统。
func NewService(dependencies Dependencies) *Service {
	return &Service{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		ids: dependencies.IDs, clock: dependencies.Clock, deviceCode: dependencies.DeviceCode,
	}
}

// GetCreateDraft 返回当前建议会议号与默认主题，不创建序号或会议记录。
func (service *Service) GetCreateDraft(ctx context.Context) (CreateDraft, error) {
	if err := service.validateDependencies(); err != nil {
		return CreateDraft{}, err
	}
	now := service.clock.Now()
	sequence, err := service.repository.PeekNextSequence(ctx, now.Format("20060102"), service.deviceCode)
	if err != nil {
		return CreateDraft{}, err
	}
	return CreateDraft{
		SuggestedMeetingNo: fmt.Sprintf("%s-%s-%02d", now.Format("20060102"), service.deviceCode, sequence),
		DefaultSubject:     meetingdomain.NormalizeSubject(""),
	}, nil
}

// GetActiveMeeting 返回当前唯一活动会议；没有活动会议时返回 nil。
func (service *Service) GetActiveMeeting(ctx context.Context) (*models.Meeting, error) {
	if err := service.validateDependencies(); err != nil {
		return nil, err
	}
	meetings, err := service.repository.ListActiveMeetings(ctx)
	if err != nil {
		return nil, err
	}
	if len(meetings) == 0 {
		return nil, nil
	}
	if len(meetings) != 1 {
		return nil, apperr.Biz(apperr.CodeDatabaseIntegrityFailed, apperr.WithOp("meeting.active.multiple"))
	}
	meeting := meetings[0]
	return &meeting, nil
}

// GetLatestInterruptedMeeting 返回最近一次恢复页会议，不把它当作可继续的活动录音。
func (service *Service) GetLatestInterruptedMeeting(ctx context.Context) (*models.Meeting, error) {
	if err := service.validateDependencies(); err != nil {
		return nil, err
	}
	return service.repository.GetLatestInterruptedMeeting(ctx)
}

// CreatePreparing 分配当日序号，并在同一短事务创建会议与有序参会者快照。
func (service *Service) CreatePreparing(ctx context.Context, input CreatePreparingInput) (models.Meeting, error) {
	if err := service.validateDependencies(); err != nil {
		return models.Meeting{}, err
	}
	meetingNo := strings.TrimSpace(input.MeetingNo)
	if !meetingdomain.IsValidMeetingNo(meetingNo) {
		return models.Meeting{}, apperr.Biz(apperr.CodeMeetingNumberInvalid, apperr.WithOp("meeting.create.number"))
	}
	subject := meetingdomain.NormalizeSubject(input.Subject)
	if !meetingdomain.IsValidSubject(subject) {
		return models.Meeting{}, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("meeting.create.subject"))
	}
	timezone := strings.TrimSpace(input.LocalTimezone)
	if timezone == "" {
		return models.Meeting{}, fmt.Errorf("本地时区不能为空")
	}
	participantInputs := make([]meetingdomain.ParticipantInput, 0, len(input.MemberIDs)+len(input.TemporaryParticipantNames))
	for _, name := range input.TemporaryParticipantNames {
		participantInputs = append(participantInputs, meetingdomain.ParticipantInput{DisplayName: name})
	}
	if len(input.MemberIDs)+len(participantInputs) == 0 {
		return models.Meeting{}, apperr.Biz(apperr.CodeMeetingParticipantsRequired, apperr.WithOp("meeting.create.participants"))
	}

	now := service.clock.Now()
	meetingID, sequenceID, err := service.nextIDs()
	if err != nil {
		return models.Meeting{}, err
	}
	useTransactionNumber := strings.TrimSpace(input.SuggestedMeetingNo) != "" && meetingNo == strings.TrimSpace(input.SuggestedMeetingNo)
	var meeting models.Meeting
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		memberInputs, err := service.loadMemberInputs(ctx, tx, input.MemberIDs)
		if err != nil {
			return err
		}
		snapshots, err := meetingdomain.BuildParticipantSnapshots(append(memberInputs, participantInputs...))
		if err != nil {
			return apperr.Dependency(apperr.CodeMeetingParticipantInvalid, err, apperr.WithOp("meeting.create.participant_snapshot"))
		}
		participants, err := service.buildParticipantModels(meetingID, snapshots, now.UnixMilli())
		if err != nil {
			return err
		}
		allocatedSequence, err := service.repository.AllocateSequence(ctx, tx, models.MeetingNumberSequence{
			ID: sequenceID, LocalDate: now.Format("20060102"), DeviceCode: service.deviceCode,
			NextSequence: 2, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		})
		if err != nil {
			return err
		}
		if useTransactionNumber {
			meetingNo = fmt.Sprintf("%s-%s-%02d", now.Format("20060102"), service.deviceCode, allocatedSequence)
		}
		meeting = buildPreparingMeeting(meetingID, meetingNo, subject, timezone, now)
		return service.repository.CreatePreparing(ctx, tx, meeting, participants)
	}); err != nil {
		if errors.Is(err, meetingrepository.ErrActiveMeetingConflict) {
			return models.Meeting{}, apperr.Biz(apperr.CodeMeetingAlreadyActive, apperr.WithOp("meeting.create.active"))
		}
		if errors.Is(err, meetingrepository.ErrMeetingNoConflict) {
			return models.Meeting{}, apperr.Biz(apperr.CodeMeetingNumberConflict, apperr.WithOp("meeting.create.number_conflict"))
		}
		return models.Meeting{}, err
	}
	return meeting, nil
}

// loadMemberInputs 以提交顺序映射活动成员，并拒绝重复、缺失或已归档 ID。
func (service *Service) loadMemberInputs(ctx context.Context, tx *gorm.DB, memberIDs []string) ([]meetingdomain.ParticipantInput, error) {
	members, err := service.repository.ListActiveMembersByIDs(ctx, tx, memberIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]models.Member, len(members))
	for _, member := range members {
		byID[member.ID] = member
	}
	seen := make(map[string]struct{}, len(memberIDs))
	result := make([]meetingdomain.ParticipantInput, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		member, found := byID[memberID]
		_, duplicate := seen[memberID]
		if !found || duplicate {
			return nil, apperr.Biz(apperr.CodeMeetingParticipantInvalid, apperr.WithOp("meeting.create.member"))
		}
		seen[memberID] = struct{}{}
		result = append(result, meetingdomain.ParticipantInput{MemberID: member.ID, DisplayName: member.Name})
	}
	return result, nil
}

// validateDependencies 在进入事务前拒绝不完整装配。
func (service *Service) validateDependencies() error {
	if service == nil || service.repository == nil || service.transactions == nil || service.ids == nil || service.clock == nil {
		return fmt.Errorf("会议服务依赖未初始化")
	}
	if len(service.deviceCode) != 4 {
		return fmt.Errorf("会议服务设备码无效")
	}
	return nil
}

// nextIDs 生成会议与序列 UUID，并拒绝异常生成器输出。
func (service *Service) nextIDs() (string, string, error) {
	sequenceID := service.ids.New()
	meetingID := service.ids.New()
	if !isUUIDv4(sequenceID) || !isUUIDv4(meetingID) {
		return "", "", fmt.Errorf("生成会议 UUID 失败")
	}
	return meetingID, sequenceID, nil
}

// buildParticipantModels 为每个快照生成独立 UUID，并映射 nullable member_id。
func (service *Service) buildParticipantModels(meetingID string, snapshots []meetingdomain.ParticipantSnapshot, now int64) ([]models.MeetingParticipant, error) {
	participants := make([]models.MeetingParticipant, 0, len(snapshots))
	for _, snapshot := range snapshots {
		participantID := service.ids.New()
		if !isUUIDv4(participantID) {
			return nil, fmt.Errorf("生成参会者 UUID 失败")
		}
		var memberID *string
		if snapshot.MemberID != "" {
			value := snapshot.MemberID
			memberID = &value
		}
		participants = append(participants, models.MeetingParticipant{
			ID: participantID, MeetingID: meetingID, MemberID: memberID,
			ParticipantKind: snapshot.Kind, DisplayNameSnapshot: snapshot.DisplayName,
			SortOrder: snapshot.SortOrder, CreatedAt: now, UpdatedAt: now,
		})
	}
	return participants, nil
}

// buildPreparingMeeting 组装不包含音频副作用的初始会议记录。
func buildPreparingMeeting(id string, meetingNo string, subject string, timezone string, now time.Time) models.Meeting {
	relativeDir := filepath.ToSlash(filepath.Join("meetings", meetingNo+"-"+filesystem.SafeFilename(subject)))
	timestamp := now.UnixMilli()
	return models.Meeting{
		ID: id, MeetingNo: meetingNo, Subject: subject, RelativeDir: relativeDir, LocalTimezone: timezone,
		LifecycleState: string(meetingdomain.LifecyclePreparing), LocalSaveState: string(meetingdomain.LocalSavePending),
		RealtimeASRState: "idle", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled",
		CreatedAt: timestamp, UpdatedAt: timestamp,
	}
}

// isUUIDv4 验证业务主键必须是 UUID v4。
func isUUIDv4(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 4
}
