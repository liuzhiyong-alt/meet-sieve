package wails

import (
	"context"
	"fmt"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
	agentservice "meet-sieve/internal/service/agent"
)

// AgentServices 汇总当前工作目录下同一组智能体服务。
type AgentServices struct {
	Settings *agentservice.SettingsService
	WakeTest *agentservice.WakeWordTestService
	Session  *agentservice.Orchestrator
	Turns    *agentservice.TurnService
	Recovery *agentservice.RecoveryCommandService
}

// AgentServiceProvider 延迟返回当前工作目录的智能体服务。
type AgentServiceProvider func() (AgentServices, error)

// AgentSettingsDTO 是不含账号、凭据和 Codex home 的设置投影。
type AgentSettingsDTO struct {
	WakeWord       string               `json:"wake_word"`
	ExecutablePath string               `json:"codex_executable_path"`
	Availability   AgentAvailabilityDTO `json:"availability"`
	UpdatedAt      int64                `json:"updated_at"`
}

// AgentAvailabilityDTO 区分可执行文件、协议和登录状态。
type AgentAvailabilityDTO struct {
	State         string `json:"state"`
	Version       string `json:"version,omitempty"`
	AccountState  string `json:"account_state"`
	ProtocolState string `json:"protocol_state"`
	Message       string `json:"message"`
}

// SaveAgentSettingsDTO 是设置页允许保存的字段。
type SaveAgentSettingsDTO struct {
	WakeWord       string `json:"wake_word"`
	ExecutablePath string `json:"codex_executable_path"`
}

// AgentApprovalDTO 是主持人可见的单次原生审批摘要。
type AgentApprovalDTO struct {
	ID               string `json:"id"`
	Tool             string `json:"tool"`
	Target           string `json:"target"`
	ParameterSummary string `json:"parameter_summary"`
	Risk             string `json:"risk"`
}

// AgentStateDTO 是会中页可在刷新后恢复的状态。
type AgentStateDTO struct {
	State     string            `json:"state"`
	MeetingID string            `json:"meeting_id"`
	TurnID    string            `json:"turn_id,omitempty"`
	Partial   string            `json:"partial,omitempty"`
	Approval  *AgentApprovalDTO `json:"approval,omitempty"`
	ErrorCode string            `json:"error_code,omitempty"`
	Revision  uint64            `json:"revision"`
}

// AgentAskDTO 是已提交问题和最终回答的稳定结果。
type AgentAskDTO struct {
	TurnID      string `json:"turn_id"`
	QuestionSeq int64  `json:"question_seq"`
	Answer      string `json:"answer,omitempty"`
	AnswerSeq   int64  `json:"answer_seq,omitempty"`
}

// AgentTimelineEntryDTO 是可与转写按 seq 合并的持久 AI 事件。
type AgentTimelineEntryDTO struct {
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	OccurredAt int64  `json:"occurred_at"`
	TurnID     string `json:"turn_id"`
	Text       string `json:"text,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// AgentRecoveryCommandsDTO 是只读可复制命令投影。
type AgentRecoveryCommandsDTO struct {
	ThreadCommand    string `json:"thread_command"`
	DirectoryCommand string `json:"directory_command"`
}

// WakeWordTestStateDTO 是真实三次测试的进程内状态。
type WakeWordTestStateDTO struct {
	State     string `json:"state"`
	Matched   int    `json:"matched"`
	Required  int    `json:"required"`
	ASRState  string `json:"asr_state"`
	ErrorCode string `json:"error_code,omitempty"`
}

// AgentEventDTO 是 Wails 持续事件使用的轻量安全投影。
type AgentEventDTO struct {
	MeetingID string            `json:"meeting_id"`
	TurnID    string            `json:"turn_id"`
	Type      string            `json:"type"`
	Delta     string            `json:"delta,omitempty"`
	Approval  *AgentApprovalDTO `json:"approval,omitempty"`
	ErrorCode string            `json:"error_code,omitempty"`
	Revision  uint64            `json:"revision"`
}

// AgentBinding 暴露主持人机器上的 Codex 设置、问答、审批和测试能力。
type AgentBinding struct {
	services        AgentServiceProvider
	contextProvider ContextProvider
	boundary        *Boundary
}

// NewAgentBinding 创建智能体 binding；构造阶段不读取数据库或启动进程。
func NewAgentBinding(services AgentServiceProvider, contextProvider ContextProvider, boundary *Boundary) *AgentBinding {
	return &AgentBinding{services: services, contextProvider: contextProvider, boundary: boundary}
}

// GetAgentSettings 返回当前唤醒词、executable 和最近探测状态。
func (binding *AgentBinding) GetAgentSettings() Result[AgentSettingsDTO] {
	return Invoke(binding.boundary, "wails.agent.settings.get", func(_ string) (AgentSettingsDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AgentSettingsDTO{}, err
		}
		view, err := services.Settings.Get(ctx)
		return mapAgentSettings(view), err
	})
}

// SaveAgentSettings 保存经过领域校验的唤醒词和单 executable。
func (binding *AgentBinding) SaveAgentSettings(input SaveAgentSettingsDTO) Result[AgentSettingsDTO] {
	return Invoke(binding.boundary, "wails.agent.settings.save", func(_ string) (AgentSettingsDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AgentSettingsDTO{}, err
		}
		view, err := services.Settings.Save(ctx, agentservice.SaveAgentSettingsInput{WakeWord: input.WakeWord, ExecutablePath: input.ExecutablePath})
		return mapAgentSettings(view), err
	})
}

// ProbeAgent 执行 schema、握手和登录探测，不返回账号信息。
func (binding *AgentBinding) ProbeAgent() Result[AgentAvailabilityDTO] {
	return Invoke(binding.boundary, "wails.agent.probe", func(_ string) (AgentAvailabilityDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AgentAvailabilityDTO{}, err
		}
		availability, err := services.Settings.Probe(ctx)
		return mapAgentAvailability(availability), err
	})
}

// GetAgentState 返回内存态或 SQLite 恢复态，不依赖已丢失的 delta。
func (binding *AgentBinding) GetAgentState(meetingID string) Result[AgentStateDTO] {
	return Invoke(binding.boundary, "wails.agent.state", func(_ string) (AgentStateDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AgentStateDTO{}, err
		}
		state, err := services.Turns.StateFor(ctx, meetingID)
		return mapAgentState(state), err
	})
}

// AskAgent 提交一个显式主持人问题；request ID 同时作为幂等键。
func (binding *AgentBinding) AskAgent(meetingID string, question string, requestID string) Result[AgentAskDTO] {
	return Invoke(binding.boundary, "wails.agent.ask", func(boundaryRequestID string) (AgentAskDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AgentAskDTO{}, err
		}
		if requestID == "" {
			requestID = boundaryRequestID
		}
		result, err := services.Turns.Ask(ctx, agentservice.AskInput{MeetingID: meetingID, Question: question, Trigger: "manual", IdempotencyKey: requestID})
		return AgentAskDTO{TurnID: result.TurnID, QuestionSeq: result.QuestionSeq, Answer: result.Answer, AnswerSeq: result.AnswerSeq}, err
	})
}

// GetAgentTimeline 按 seq 增量恢复问题、最终回答和失败状态。
func (binding *AgentBinding) GetAgentTimeline(meetingID string, afterSeq int64, limit int) Result[[]AgentTimelineEntryDTO] {
	return Invoke(binding.boundary, "wails.agent.timeline", func(_ string) ([]AgentTimelineEntryDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return nil, err
		}
		entries, err := services.Turns.ListTimeline(ctx, meetingID, afterSeq, limit)
		if err != nil {
			return nil, err
		}
		result := make([]AgentTimelineEntryDTO, 0, len(entries))
		for _, entry := range entries {
			result = append(result, AgentTimelineEntryDTO{Seq: entry.Seq, Kind: entry.Kind, OccurredAt: entry.OccurredAt, TurnID: entry.TurnID, Text: entry.Text, Reason: entry.Reason})
		}
		return result, nil
	})
}

// InterruptAgent 幂等停止当前本地 turn。
func (binding *AgentBinding) InterruptAgent(meetingID string, turnID string) Result[bool] {
	return Invoke(binding.boundary, "wails.agent.interrupt", func(_ string) (bool, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return false, err
		}
		err = services.Turns.Interrupt(ctx, meetingID, turnID)
		return err == nil, err
	})
}

// RespondAgentApproval 只响应当前内存态、当前 turn 的单次审批。
func (binding *AgentBinding) RespondAgentApproval(meetingID string, turnID string, approvalID string, decision string) Result[bool] {
	return Invoke(binding.boundary, "wails.agent.approval", func(_ string) (bool, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return false, err
		}
		state := services.Turns.State()
		if state.MeetingID != meetingID || state.TurnID != turnID || state.Approval == nil || state.Approval.ID != approvalID {
			return false, apperr.Biz(apperr.CodeAgentApprovalExpired, apperr.WithOp("wails.agent.approval"))
		}
		err = services.Turns.RespondApproval(ctx, port.RespondAgentApprovalRequest{
			SessionID: state.SessionID, TurnID: state.ProviderTurnID, ApprovalID: approvalID, Decision: port.AgentApprovalDecision(decision),
		})
		return err == nil, err
	})
}

// RetryAgent 恢复最近 thread，找不到时从本地事实创建新 thread。
func (binding *AgentBinding) RetryAgent(meetingID string) Result[bool] {
	return Invoke(binding.boundary, "wails.agent.retry", func(_ string) (bool, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return false, err
		}
		err = services.Session.Retry(ctx, meetingID)
		return err == nil, err
	})
}

// GetAgentRecoveryCommands 返回由后端可信路径构造的复制文本。
func (binding *AgentBinding) GetAgentRecoveryCommands(meetingID string) Result[AgentRecoveryCommandsDTO] {
	return Invoke(binding.boundary, "wails.agent.recovery_commands", func(_ string) (AgentRecoveryCommandsDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AgentRecoveryCommandsDTO{}, err
		}
		commands, err := services.Recovery.Get(ctx, meetingID)
		return AgentRecoveryCommandsDTO{ThreadCommand: commands.ThreadCommand, DirectoryCommand: commands.DirectoryCommand}, err
	})
}

// StartWakeWordTest 建立真实麦克风和 ASR 临时链路。
func (binding *AgentBinding) StartWakeWordTest() Result[WakeWordTestStateDTO] {
	return Invoke(binding.boundary, "wails.agent.wake_test.start", func(_ string) (WakeWordTestStateDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return WakeWordTestStateDTO{}, err
		}
		state, err := services.WakeTest.Start(ctx)
		return mapWakeWordTestState(state), err
	})
}

// StopWakeWordTest 停止测试并等待资源释放。
func (binding *AgentBinding) StopWakeWordTest() Result[WakeWordTestStateDTO] {
	return Invoke(binding.boundary, "wails.agent.wake_test.stop", func(_ string) (WakeWordTestStateDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return WakeWordTestStateDTO{}, err
		}
		err = services.WakeTest.Stop(ctx)
		return mapWakeWordTestState(services.WakeTest.State()), err
	})
}

// current 返回同一工作目录的服务和 Wails 根 context。
func (binding *AgentBinding) current() (AgentServices, context.Context, error) {
	if binding == nil || binding.services == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
		return AgentServices{}, nil, fmt.Errorf("智能体服务尚未准备")
	}
	services, err := binding.services()
	return services, binding.contextProvider(), err
}

func mapAgentSettings(view agentservice.AgentSettingsView) AgentSettingsDTO {
	return AgentSettingsDTO{WakeWord: view.WakeWord, ExecutablePath: view.ExecutablePath, Availability: mapAgentAvailability(view.Availability), UpdatedAt: view.UpdatedAt}
}

func mapAgentAvailability(value port.AgentAvailability) AgentAvailabilityDTO {
	return AgentAvailabilityDTO{State: string(value.State), Version: value.Version, AccountState: string(value.AccountState), ProtocolState: string(value.ProtocolState), Message: value.Message}
}

func mapAgentState(value agentservice.AgentRuntimeState) AgentStateDTO {
	result := AgentStateDTO{State: value.State, MeetingID: value.MeetingID, TurnID: value.TurnID, Partial: value.Partial, ErrorCode: value.ErrorCode, Revision: value.Revision}
	if value.Approval != nil {
		result.Approval = &AgentApprovalDTO{ID: value.Approval.ID, Tool: value.Approval.Tool, Target: value.Approval.Target, ParameterSummary: value.Approval.ParameterSummary, Risk: value.Approval.Risk}
	}
	return result
}

func mapWakeWordTestState(value agentservice.WakeWordTestState) WakeWordTestStateDTO {
	return WakeWordTestStateDTO{State: string(value.State), Matched: value.Matched, Required: value.Required, ASRState: value.ASRState, ErrorCode: value.ErrorCode}
}

// MapAgentEventDTO 删除 session/provider 身份和底层 envelope，只保留 UI 所需字段。
func MapAgentEventDTO(value port.AgentEvent) AgentEventDTO {
	result := AgentEventDTO{MeetingID: value.MeetingID, TurnID: value.TurnID, Type: string(value.Type), Delta: value.Delta, ErrorCode: value.FailureCode, Revision: value.Revision}
	if value.Approval != nil {
		result.Approval = &AgentApprovalDTO{ID: value.Approval.ID, Tool: value.Approval.Tool, Target: value.Approval.Target, ParameterSummary: value.Approval.ParameterSummary, Risk: value.Approval.Risk}
	}
	return result
}

// MapWakeWordTestStateDTO 把进程内测试状态转换为安全事件数据。
func MapWakeWordTestStateDTO(value agentservice.WakeWordTestState) WakeWordTestStateDTO {
	return mapWakeWordTestState(value)
}
