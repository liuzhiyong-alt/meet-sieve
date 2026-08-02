package meeting

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/models"

	"gorm.io/gorm"
)

var (
	// ErrMeetingNoConflict 表示会议号已被历史或当前会议占用。
	ErrMeetingNoConflict = errors.New("会议号重复")
	// ErrActiveMeetingConflict 表示数据库已经存在另一场活动会议。
	ErrActiveMeetingConflict = errors.New("已有活动会议")
)

// Repository 负责会议与参会者快照持久化，不决定业务状态转换。
type Repository struct {
	reader *gorm.DB
}

// NewRepository 创建会议 Repository，reader 仅用于事务外查询。
func NewRepository(reader *gorm.DB) *Repository {
	return &Repository{reader: reader}
}

// AllocateSequence 在调用方短事务内分配当日设备序号并递增下一序号。
func (repository *Repository) AllocateSequence(ctx context.Context, tx *gorm.DB, sequence models.MeetingNumberSequence) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("分配会议序号：事务不能为空")
	}
	var current models.MeetingNumberSequence
	err := tx.WithContext(ctx).Select(sequenceColumns()).
		Where("local_date = ? AND device_code = ?", sequence.LocalDate, sequence.DeviceCode).Take(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.WithContext(ctx).Create(&sequence).Error; err != nil {
			return 0, fmt.Errorf("创建会议序号失败: %w", err)
		}
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("读取会议序号失败: %w", err)
	}
	allocated := current.NextSequence
	result := tx.WithContext(ctx).Model(&models.MeetingNumberSequence{}).
		Where("id = ? AND next_sequence = ?", current.ID, current.NextSequence).
		Updates(map[string]any{"next_sequence": current.NextSequence + 1, "updated_at": sequence.UpdatedAt})
	if result.Error != nil {
		return 0, fmt.Errorf("递增会议序号失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return 0, fmt.Errorf("会议序号并发更新失败")
	}
	return allocated, nil
}

// CreatePreparing 在调用方事务内创建 preparing 会议及其有序快照。
func (repository *Repository) CreatePreparing(ctx context.Context, tx *gorm.DB, meeting models.Meeting, participants []models.MeetingParticipant) error {
	if tx == nil {
		return fmt.Errorf("创建会议：事务不能为空")
	}
	if err := tx.WithContext(ctx).Create(&meeting).Error; err != nil {
		return mapCreateError(err)
	}
	if err := tx.WithContext(ctx).Create(&participants).Error; err != nil {
		return fmt.Errorf("创建参会者快照失败: %w", err)
	}
	return nil
}

// ListActiveMembersByIDs 在调用方事务内读取指定活动成员，供创建不可变快照。
func (repository *Repository) ListActiveMembersByIDs(ctx context.Context, tx *gorm.DB, memberIDs []string) ([]models.Member, error) {
	if tx == nil {
		return nil, fmt.Errorf("查询会议成员：事务不能为空")
	}
	if len(memberIDs) == 0 {
		return []models.Member{}, nil
	}
	var members []models.Member
	if err := tx.WithContext(ctx).
		Select("id", "name", "name_normalized", "notes", "created_at", "updated_at", "archived_at").
		Where("id IN ? AND archived_at IS NULL", memberIDs).Find(&members).Error; err != nil {
		return nil, fmt.Errorf("查询会议活动成员失败: %w", err)
	}
	return members, nil
}

// PeekNextSequence 只读返回当地日期和设备码的下一个建议序号，不创建或递增记录。
func (repository *Repository) PeekNextSequence(ctx context.Context, localDate string, deviceCode string) (int, error) {
	if repository == nil || repository.reader == nil {
		return 0, fmt.Errorf("预览会议序号：Repository 不可用")
	}
	var sequence models.MeetingNumberSequence
	err := repository.reader.WithContext(ctx).Select(sequenceColumns()).
		Where("local_date = ? AND device_code = ?", localDate, deviceCode).Take(&sequence).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("读取建议会议序号失败: %w", err)
	}
	return sequence.NextSequence, nil
}

// sequenceColumns 返回会议序列表的显式读取字段。
func sequenceColumns() []string {
	return []string{"id", "local_date", "device_code", "next_sequence", "created_at", "updated_at"}
}

// mapCreateError 将 SQLite 唯一约束转换为稳定 Repository 错误。
func mapCreateError(err error) error {
	message := err.Error()
	if strings.Contains(message, "meetings.meeting_no") {
		return ErrMeetingNoConflict
	}
	if strings.Contains(message, "idx_meetings_single_active") {
		return ErrActiveMeetingConflict
	}
	return fmt.Errorf("创建会议记录失败: %w", err)
}
