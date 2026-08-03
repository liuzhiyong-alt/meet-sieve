package wails

import (
	"context"

	agentservice "meet-sieve/internal/service/agent"
	meetingservice "meet-sieve/internal/service/meeting"
)

// FinalizationServices 是收尾状态和 Codex 结束同步的窄服务集合。
type FinalizationServices struct {
	Runtime   *meetingservice.RuntimeService
	FinalSync *agentservice.FinalSyncService
}

// FinalizationServiceProvider 返回当前工作目录的收尾服务。
type FinalizationServiceProvider func() (FinalizationServices, error)

// FinalizationBinding 暴露本地核心收尾与独立 Codex 同步命令。
type FinalizationBinding struct {
	services FinalizationServiceProvider
	context  ContextProvider
	boundary *Boundary
}

// NewFinalizationBinding 创建不访问数据库的收尾 binding。
func NewFinalizationBinding(services FinalizationServiceProvider, contextProvider ContextProvider, boundary *Boundary) *FinalizationBinding {
	return &FinalizationBinding{services: services, context: contextProvider, boundary: boundary}
}

// GetFinalizationState 从 runtime/SQLite 重建核心收尾状态。
func (binding *FinalizationBinding) GetFinalizationState(meetingID string) Result[FinalizationStateDTO] {
	return Invoke(binding.boundary, "wails.finalization.get", func(string) (FinalizationStateDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return FinalizationStateDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return FinalizationStateDTO{}, err
		}
		state, err := services.Runtime.GetFinalizationState(context.Background(), meetingID)
		return mapFinalizationState(state), err
	})
}

// RetryFinalization 复用 EndMeeting 的状态谓词重做未完成步骤。
func (binding *FinalizationBinding) RetryFinalization(meetingID string) Result[FinalizationStateDTO] {
	return Invoke(binding.boundary, "wails.finalization.retry", func(string) (FinalizationStateDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return FinalizationStateDTO{}, err
		}
		services, err := binding.services()
		if err != nil {
			return FinalizationStateDTO{}, err
		}
		ctx, err := binding.currentContext()
		if err != nil {
			return FinalizationStateDTO{}, err
		}
		if _, err := services.Runtime.EndMeeting(ctx, meetingID); err != nil {
			return FinalizationStateDTO{}, err
		}
		state, err := services.Runtime.GetFinalizationState(ctx, meetingID)
		return mapFinalizationState(state), err
	})
}

// RetryAgentFinalSync 显式恢复原 thread 和未成功游标。
func (binding *FinalizationBinding) RetryAgentFinalSync(meetingID string, requestID string) Result[MeetingStateEventDTO] {
	return Invoke(binding.boundary, "wails.final_sync.retry", func(string) (MeetingStateEventDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return MeetingStateEventDTO{}, err
		}
		if err := requireUUID("request ID", requestID); err != nil {
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
		err = services.FinalSync.RetryFinalSync(ctx, meetingID, requestID)
		return MeetingStateEventDTO{MeetingID: meetingID, State: "unavailable"}, err
	})
}

// currentContext 返回 Wails 生命周期 context。
func (binding *FinalizationBinding) currentContext() (context.Context, error) {
	if binding == nil || binding.context == nil || binding.context() == nil {
		return nil, context.Canceled
	}
	return binding.context(), nil
}

// mapFinalizationState 转换为稳定字符串 DTO。
func mapFinalizationState(state meetingservice.FinalizationSnapshot) FinalizationStateDTO {
	return FinalizationStateDTO{MeetingID: state.MeetingID, State: state.State, Stage: string(state.Stage), ErrorCode: state.ErrorCode, Revision: state.Revision}
}
