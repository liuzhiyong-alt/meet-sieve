package minutes

import (
	"context"
	"fmt"
	"unicode/utf8"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	minutesrepository "meet-sieve/internal/repository/minutes"
	"meet-sieve/models"
)

const maxHumanMinuteBytes = 512 * 1024

// MeetingReader 读取可信相对目录和会议状态。
type MeetingReader interface {
	GetMeeting(ctx context.Context, meetingID string) (models.Meeting, error)
}

// VersionService 编排不可变版本事务与提交后的 Markdown 投影。
type VersionService struct {
	repository *minutesrepository.Repository
	meetings   MeetingReader
	projector  *MinuteProjector
	ids        identity.Generator
	clock      clock.Clock
	events     GenerationEventSink
}

// NewVersionService 创建纪要版本服务。
func NewVersionService(repository *minutesrepository.Repository, meetings MeetingReader, projector *MinuteProjector, ids identity.Generator, appClock clock.Clock) *VersionService {
	return &VersionService{repository: repository, meetings: meetings, projector: projector, ids: ids, clock: appClock}
}

// SetEventSink 在装配阶段接入版本提交后的页面失效通知。
func (service *VersionService) SetEventSink(events GenerationEventSink) {
	if service != nil {
		service.events = events
	}
}

// SaveHumanDraft 从明确 current 基线创建人工版本。
func (service *VersionService) SaveHumanDraft(ctx context.Context, meetingID string, baseVersionID string, content string, requestID string) (models.MinuteVersion, ProjectionState, error) {
	if err := service.validate(meetingID, requestID, content); err != nil {
		return models.MinuteVersion{}, ProjectionState{}, err
	}
	version, err := service.repository.SaveHumanMinute(ctx, minutesrepository.SaveHumanMinuteInput{VersionID: requestID, MeetingID: meetingID, BaseVersionID: baseVersionID, ContentMarkdown: content, UpdatedAt: service.clock.Now().UnixMilli()})
	version, state, err := service.project(ctx, meetingID, version, err)
	service.publish(meetingID, err)
	return version, state, err
}

// ConfirmCurrent 确认 current，不创建新版本。
func (service *VersionService) ConfirmCurrent(ctx context.Context, meetingID string, versionID string) (ProjectionState, error) {
	if service == nil || service.repository == nil || service.clock == nil || meetingID == "" || versionID == "" {
		return ProjectionState{}, fmt.Errorf("确认纪要：参数无效")
	}
	if err := service.repository.ConfirmCurrentMinute(ctx, minutesrepository.ConfirmMinuteInput{MeetingID: meetingID, VersionID: versionID, ConfirmedAt: service.clock.Now().UnixMilli()}); err != nil {
		return ProjectionState{}, err
	}
	_, state, err := service.project(ctx, meetingID, models.MinuteVersion{}, nil)
	service.publish(meetingID, err)
	return state, err
}

// RestoreHistory 把历史内容复制为新 current 版本。
func (service *VersionService) RestoreHistory(ctx context.Context, meetingID string, sourceVersionID string, requestID string) (models.MinuteVersion, ProjectionState, error) {
	if service == nil || service.repository == nil || service.clock == nil || meetingID == "" || sourceVersionID == "" || requestID == "" {
		return models.MinuteVersion{}, ProjectionState{}, fmt.Errorf("恢复纪要：参数无效")
	}
	version, err := service.repository.RestoreMinuteVersion(ctx, minutesrepository.RestoreMinuteInput{VersionID: requestID, MeetingID: meetingID, SourceVersionID: sourceVersionID, UpdatedAt: service.clock.Now().UnixMilli()})
	version, state, err := service.project(ctx, meetingID, version, err)
	service.publish(meetingID, err)
	return version, state, err
}

// publish 只提示页面重新查询，人工正文不会进入事件。
func (service *VersionService) publish(meetingID string, err error) {
	if service.events == nil || err != nil {
		return
	}
	service.events.PublishMinutesChanged(meetingID, GenerationState{MeetingID: meetingID, State: "version_changed"})
}

// validate 校验人工正文边界和必要依赖。
func (service *VersionService) validate(meetingID string, requestID string, content string) error {
	if service == nil || service.repository == nil || service.meetings == nil || service.projector == nil || service.clock == nil || meetingID == "" || requestID == "" || content == "" || len(content) > maxHumanMinuteBytes || !utf8.ValidString(content) {
		return fmt.Errorf("保存人工纪要：参数无效")
	}
	return nil
}

// project 在数据库成功后刷新文件；投影失败保留已创建的 SQLite 版本。
func (service *VersionService) project(ctx context.Context, meetingID string, version models.MinuteVersion, commitErr error) (models.MinuteVersion, ProjectionState, error) {
	if commitErr != nil {
		return models.MinuteVersion{}, ProjectionState{}, commitErr
	}
	meeting, err := service.meetings.GetMeeting(ctx, meetingID)
	if err != nil {
		return version, ProjectionState{State: "failed", ErrorCode: "MINUTES_PROJECTION_FAILED"}, nil
	}
	if err := service.projector.Flush(ctx, meeting); err != nil {
		return version, service.projector.State(meetingID), nil
	}
	return version, service.projector.State(meetingID), nil
}
