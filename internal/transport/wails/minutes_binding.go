package wails

import (
	"context"
	"fmt"

	minutesrepository "meet-sieve/internal/repository/minutes"
	minutesservice "meet-sieve/internal/service/minutes"
	"meet-sieve/models"
)

// MinutesServices 是纪要查询、生成、设置和底层兼容命令集合。
type MinutesServices struct {
	Repository *minutesrepository.Repository
	Generation *minutesservice.GenerationService
	Settings   *minutesservice.SettingsService
	Versions   *minutesservice.VersionService
	Projector  *minutesservice.MinuteProjector
}

// MinutesSettingsDTO 是会议纪要要求设置的独立契约。
type MinutesSettingsDTO struct {
	Prompt    string `json:"prompt"`
	IsDefault bool   `json:"is_default"`
	UpdatedAt int64  `json:"updated_at"`
}

// MinutesServiceProvider 返回当前工作目录的纪要服务。
type MinutesServiceProvider func() (MinutesServices, error)

// MinutesBinding 暴露单份纪要交互；版本命令只保留给旧调用兼容。
type MinutesBinding struct {
	services MinutesServiceProvider
	context  ContextProvider
	boundary *Boundary
}

// NewMinutesBinding 创建纪要 binding。
func NewMinutesBinding(services MinutesServiceProvider, contextProvider ContextProvider, boundary *Boundary) *MinutesBinding {
	return &MinutesBinding{services: services, context: contextProvider, boundary: boundary}
}

// GetMinutesState 从 SQLite 和当前内存 partial 状态重建页面。
func (binding *MinutesBinding) GetMinutesState(meetingID string) Result[MinutesStateDTO] {
	return Invoke(binding.boundary, "wails.minutes.get", func(string) (MinutesStateDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return MinutesStateDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return MinutesStateDTO{}, err
		}
		record, err := services.Repository.ReadState(context.Background(), meetingID)
		if err != nil {
			return MinutesStateDTO{}, err
		}
		return mapMinutesState(meetingID, record, services.Generation.State(), services.Projector.State(meetingID)), nil
	})
}

// GetMinutesSettings 返回生成会议纪要时使用的业务要求。
func (binding *MinutesBinding) GetMinutesSettings() Result[MinutesSettingsDTO] {
	return Invoke(binding.boundary, "wails.minutes.settings.get", func(string) (MinutesSettingsDTO, error) {
		services, err := binding.services()
		if err != nil {
			return MinutesSettingsDTO{}, err
		}
		view, err := services.Settings.Get(context.Background())
		return mapMinutesSettings(view), err
	})
}

// SaveMinutesSettings 独立保存业务要求；空内容表示恢复默认要求。
func (binding *MinutesBinding) SaveMinutesSettings(prompt string) Result[MinutesSettingsDTO] {
	return Invoke(binding.boundary, "wails.minutes.settings.save", func(string) (MinutesSettingsDTO, error) {
		services, err := binding.services()
		if err != nil {
			return MinutesSettingsDTO{}, err
		}
		view, err := services.Settings.Save(context.Background(), prompt)
		return mapMinutesSettings(view), err
	})
}

// mapMinutesSettings 显式转换设置服务投影。
func mapMinutesSettings(view minutesservice.SettingsView) MinutesSettingsDTO {
	return MinutesSettingsDTO{Prompt: view.Prompt, IsDefault: view.IsDefault, UpdatedAt: view.UpdatedAt}
}

// GenerateMinutes 同步运行用户主动生成；前端 Promise 可用 Stop 并发取消。
func (binding *MinutesBinding) GenerateMinutes(meetingID string, showGapNotice bool, requestID string) Result[MinuteMutationDTO] {
	return Invoke(binding.boundary, "wails.minutes.generate", func(string) (MinuteMutationDTO, error) {
		if err := validateMinuteIDs(meetingID, requestID); err != nil {
			return MinuteMutationDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return MinuteMutationDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return MinuteMutationDTO{}, err
		}
		result, err := services.Generation.Generate(ctx, minutesservice.GenerateInput{MeetingID: meetingID, ShowGapNotice: showGapNotice, RequestID: requestID})
		return mapMinuteMutation(result.Version, result.Projection), err
	})
}

// StopMinutesGeneration 取消匹配的当前生成 turn。
func (binding *MinutesBinding) StopMinutesGeneration(meetingID string, turnID string) Result[MeetingStateEventDTO] {
	return Invoke(binding.boundary, "wails.minutes.stop", func(string) (MeetingStateEventDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return MeetingStateEventDTO{}, err
		}
		if err := requireUUID("turn ID", turnID); err != nil {
			return MeetingStateEventDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return MeetingStateEventDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return MeetingStateEventDTO{}, err
		}
		err = services.Generation.Stop(ctx, meetingID, turnID)
		return MeetingStateEventDTO{MeetingID: meetingID, State: "cancelled"}, err
	})
}

// SaveMinuteDraft 从明确 current 基线创建人工版本。
func (binding *MinutesBinding) SaveMinuteDraft(meetingID string, baseVersionID string, content string, requestID string) Result[MinuteMutationDTO] {
	return Invoke(binding.boundary, "wails.minutes.save", func(string) (MinuteMutationDTO, error) {
		if err := validateMinuteIDs(meetingID, requestID); err != nil {
			return MinuteMutationDTO{}, err
		}
		if err := requireUUID("base version ID", baseVersionID); err != nil {
			return MinuteMutationDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return MinuteMutationDTO{}, err
		}
		version, projection, err := services.Versions.SaveHumanDraft(context.Background(), meetingID, baseVersionID, content, requestID)
		return mapMinuteMutation(version, projection), err
	})
}

// ConfirmMinute 确认 current，不创建版本；request ID 只标识用户动作。
func (binding *MinutesBinding) ConfirmMinute(meetingID string, versionID string, requestID string) Result[MinutesStateDTO] {
	return Invoke(binding.boundary, "wails.minutes.confirm", func(string) (MinutesStateDTO, error) {
		if err := validateMinuteIDs(meetingID, requestID); err != nil {
			return MinutesStateDTO{}, err
		}
		if err := requireUUID("version ID", versionID); err != nil {
			return MinutesStateDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return MinutesStateDTO{}, err
		}
		if _, err := services.Versions.ConfirmCurrent(context.Background(), meetingID, versionID); err != nil {
			return MinutesStateDTO{}, err
		}
		record, err := services.Repository.ReadState(context.Background(), meetingID)
		return mapMinutesState(meetingID, record, services.Generation.State(), services.Projector.State(meetingID)), err
	})
}

// ListMinuteVersions 按版本号游标返回不可变历史。
func (binding *MinutesBinding) ListMinuteVersions(meetingID string, cursor int, limit int) Result[MinuteVersionPageDTO] {
	return Invoke(binding.boundary, "wails.minutes.list", func(string) (MinuteVersionPageDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return MinuteVersionPageDTO{}, err
		}
		if cursor < 0 || limit < 1 || limit > 100 {
			return MinuteVersionPageDTO{}, fmt.Errorf("纪要历史游标或条数无效")
		}
		services, err := binding.services()
		if err != nil {
			return MinuteVersionPageDTO{}, err
		}
		versions, err := services.Repository.ListVersions(context.Background(), meetingID, cursor, limit)
		page := MinuteVersionPageDTO{Items: mapMinuteVersions(versions)}
		if len(versions) == limit {
			page.NextCursor = versions[len(versions)-1].VersionNo
		}
		return page, err
	})
}

// RestoreMinuteVersion 复制历史内容为新的 current 版本。
func (binding *MinutesBinding) RestoreMinuteVersion(meetingID string, versionID string, requestID string) Result[MinuteMutationDTO] {
	return Invoke(binding.boundary, "wails.minutes.restore", func(string) (MinuteMutationDTO, error) {
		if err := validateMinuteIDs(meetingID, requestID); err != nil {
			return MinuteMutationDTO{}, err
		}
		if err := requireUUID("version ID", versionID); err != nil {
			return MinuteMutationDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return MinuteMutationDTO{}, err
		}
		version, projection, err := services.Versions.RestoreHistory(context.Background(), meetingID, versionID, requestID)
		return mapMinuteMutation(version, projection), err
	})
}

// currentContext 返回 Wails 生命周期 context。
func (binding *MinutesBinding) currentContext() (context.Context, error) {
	if binding == nil || binding.context == nil || binding.context() == nil {
		return nil, context.Canceled
	}
	return binding.context(), nil
}

// validateMinuteIDs 校验会议与动作 ID。
func validateMinuteIDs(meetingID string, requestID string) error {
	if err := requireUUID("meeting ID", meetingID); err != nil {
		return err
	}
	return requireUUID("request ID", requestID)
}

// mapMinutesState 合并持久状态和当前运行态。
func mapMinutesState(meetingID string, record minutesrepository.StateRecord, runtime minutesservice.GenerationState, projection minutesservice.ProjectionState) MinutesStateDTO {
	result := MinutesStateDTO{MeetingID: meetingID, State: record.Aggregate, RuntimeState: runtime.State, ProjectionState: projection.State, Revision: runtime.Revision}
	if record.Current != nil {
		value := mapMinuteVersion(*record.Current)
		result.Current = &value
	}
	if record.LatestCandidate != nil {
		value := mapMinuteVersion(*record.LatestCandidate)
		result.LatestCandidate = &value
	}
	if record.RecentFailure != nil {
		result.TurnID = record.RecentFailure.ID
		if record.RecentFailure.LastErrorCode != nil {
			result.RecentErrorCode = *record.RecentFailure.LastErrorCode
		}
	}
	if runtime.MeetingID == meetingID && runtime.TurnID != "" {
		result.TurnID = runtime.TurnID
	}
	return result
}

// mapMinuteMutation 转换版本写入结果。
func mapMinuteMutation(version models.MinuteVersion, projection minutesservice.ProjectionState) MinuteMutationDTO {
	return MinuteMutationDTO{Version: mapMinuteVersion(version), ProjectionState: projection.State, ProjectionError: projection.ErrorCode}
}

// mapMinuteVersions 转换不可变版本列表。
func mapMinuteVersions(values []models.MinuteVersion) []MinuteVersionDTO {
	result := make([]MinuteVersionDTO, 0, len(values))
	for _, value := range values {
		result = append(result, mapMinuteVersion(value))
	}
	return result
}

// mapMinuteVersion 转换单个不可变版本。
func mapMinuteVersion(value models.MinuteVersion) MinuteVersionDTO {
	return MinuteVersionDTO{ID: value.ID, VersionNo: value.VersionNo, Source: value.Source, ContentMarkdown: value.ContentMarkdown, State: value.State, IsCurrent: value.IsCurrent, ConfirmedAt: value.ConfirmedAt, CreatedAt: value.CreatedAt}
}
