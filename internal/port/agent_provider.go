package port

import (
	"context"
	"time"
)

// AgentAvailabilityState 描述智能体整体可用状态。
type AgentAvailabilityState string

const (
	// AgentAvailabilityUnchecked 表示尚未完成可用性检测。
	AgentAvailabilityUnchecked AgentAvailabilityState = "unchecked"
	// AgentAvailabilityAvailable 表示账号和协议均允许使用。
	AgentAvailabilityAvailable AgentAvailabilityState = "available"
	// AgentAvailabilityUnavailable 表示智能体不可用，但不影响会议核心链路。
	AgentAvailabilityUnavailable AgentAvailabilityState = "unavailable"
)

// Valid 判断可用状态是否属于稳定枚举。
func (state AgentAvailabilityState) Valid() bool {
	switch state {
	case AgentAvailabilityUnchecked, AgentAvailabilityAvailable, AgentAvailabilityUnavailable:
		return true
	default:
		return false
	}
}

// AgentAccountState 描述 provider 账号状态，不包含账号信息。
type AgentAccountState string

const (
	// AgentAccountUnknown 表示尚未检测登录状态。
	AgentAccountUnknown AgentAccountState = "unknown"
	// AgentAccountLoggedIn 表示用户已在 provider 中登录。
	AgentAccountLoggedIn AgentAccountState = "logged_in"
	// AgentAccountLoggedOut 表示 provider 需要用户在外部登录。
	AgentAccountLoggedOut AgentAccountState = "logged_out"
)

// Valid 判断账号状态是否属于稳定枚举。
func (state AgentAccountState) Valid() bool {
	switch state {
	case AgentAccountUnknown, AgentAccountLoggedIn, AgentAccountLoggedOut:
		return true
	default:
		return false
	}
}

// AgentProtocolState 描述本机 provider 协议兼容状态。
type AgentProtocolState string

const (
	// AgentProtocolUnchecked 表示尚未检查协议。
	AgentProtocolUnchecked AgentProtocolState = "unchecked"
	// AgentProtocolCompatible 表示必要协议契约兼容。
	AgentProtocolCompatible AgentProtocolState = "compatible"
	// AgentProtocolIncompatible 表示必要协议契约发生漂移。
	AgentProtocolIncompatible AgentProtocolState = "incompatible"
)

// Valid 判断协议状态是否属于稳定枚举。
func (state AgentProtocolState) Valid() bool {
	switch state {
	case AgentProtocolUnchecked, AgentProtocolCompatible, AgentProtocolIncompatible:
		return true
	default:
		return false
	}
}

// AgentAvailabilityRequest 描述检测 provider 所需的本机配置。
type AgentAvailabilityRequest struct {
	ExecutablePath string
}

// AgentAvailability 描述本机智能体是否可以使用。
type AgentAvailability struct {
	State         AgentAvailabilityState
	Version       string
	AccountState  AgentAccountState
	ProtocolState AgentProtocolState
	Message       string
}

// StartAgentSessionRequest 描述新建智能体会话所需的最小上下文。
type StartAgentSessionRequest struct {
	SessionID        string
	WorkingDirectory string
	Prompt           string
	ExecutablePath   string
}

// ResumeAgentSessionRequest 描述恢复既有智能体会话的请求。
type ResumeAgentSessionRequest struct {
	SessionID         string
	ProviderSessionID string
	WorkingDirectory  string
	ExecutablePath    string
}

// AgentSession 是智能体会话的稳定业务身份。
type AgentSession struct {
	ID                string
	ProviderSessionID string
	Resumed           bool
}

// AgentTurnKind 描述 provider turn 的稳定业务用途。
type AgentTurnKind string

const (
	// AgentTurnInitialize 用于建立首次结构化快照。
	AgentTurnInitialize AgentTurnKind = "initialize"
	// AgentTurnIngest 用于分批摄入新增会议事实。
	AgentTurnIngest AgentTurnKind = "ingest"
	// AgentTurnAnswer 用于回答主持人问题。
	AgentTurnAnswer AgentTurnKind = "answer"
	// AgentTurnMinutes 用于生成结构化会议纪要。
	AgentTurnMinutes AgentTurnKind = "minutes"
)

// Valid 判断 turn 用途是否属于稳定枚举。
func (kind AgentTurnKind) Valid() bool {
	switch kind {
	case AgentTurnInitialize, AgentTurnIngest, AgentTurnAnswer, AgentTurnMinutes:
		return true
	default:
		return false
	}
}

// RunAgentTurnRequest 描述一次智能体任务的稳定输入与边界。
type RunAgentTurnRequest struct {
	SessionID    string
	TurnID       string
	Kind         AgentTurnKind
	Input        string
	OutputSchema []byte
	Deadline     time.Time
}

// AgentEventType 描述 provider 向业务层发布的事件类型。
type AgentEventType string

const (
	// AgentEventTurnStarted 表示 provider 已创建 turn。
	AgentEventTurnStarted AgentEventType = "turn_started"
	// AgentEventAnswerDelta 表示可展示的回答增量。
	AgentEventAnswerDelta AgentEventType = "answer_delta"
	// AgentEventApprovalRequested 表示原生审批请求等待主持人处理。
	AgentEventApprovalRequested AgentEventType = "approval_requested"
	// AgentEventFinalOutput 表示 provider 返回了待本地校验的最终 JSON。
	AgentEventFinalOutput AgentEventType = "final_output"
	// AgentEventCompleted 表示 turn 已成功结束。
	AgentEventCompleted AgentEventType = "completed"
	// AgentEventFailed 表示 turn 失败。
	AgentEventFailed AgentEventType = "failed"
	// AgentEventCancelled 表示 turn 已取消。
	AgentEventCancelled AgentEventType = "cancelled"
	// AgentEventTimelineChanged 表示持久化时间线已提交，可以安全重新读取。
	AgentEventTimelineChanged AgentEventType = "timeline_changed"
)

// Valid 判断事件类型是否属于稳定枚举。
func (eventType AgentEventType) Valid() bool {
	switch eventType {
	case AgentEventTurnStarted, AgentEventAnswerDelta, AgentEventApprovalRequested,
		AgentEventFinalOutput, AgentEventCompleted, AgentEventFailed, AgentEventCancelled,
		AgentEventTimelineChanged:
		return true
	default:
		return false
	}
}

// AgentFinalOutput 是等待业务层校验的 provider 最终 JSON。
type AgentFinalOutput struct {
	JSON []byte
}

// PendingAgentApproval 是只用于主持人显示的原生审批摘要。
type PendingAgentApproval struct {
	ID               string
	TurnID           string
	Tool             string
	Target           string
	ParameterSummary string
	Risk             string
}

// AgentEvent 是智能体流式输出事件。
type AgentEvent struct {
	Type           AgentEventType
	MeetingID      string
	SessionID      string
	TurnID         string
	ProviderTurnID string
	Delta          string
	FinalOutput    *AgentFinalOutput
	Approval       *PendingAgentApproval
	FailureCode    string
	Revision       uint64
}

// AgentApprovalDecision 描述主持人对单次原生审批的决定。
type AgentApprovalDecision string

const (
	// AgentApprovalAllow 仅允许当前一次原生请求。
	AgentApprovalAllow AgentApprovalDecision = "allow"
	// AgentApprovalDecline 拒绝当前一次原生请求。
	AgentApprovalDecline AgentApprovalDecision = "decline"
)

// Valid 判断审批决定是否属于稳定枚举。
func (decision AgentApprovalDecision) Valid() bool {
	return decision == AgentApprovalAllow || decision == AgentApprovalDecline
}

// RespondAgentApprovalRequest 描述主持人对当前原生审批的响应。
type RespondAgentApprovalRequest struct {
	SessionID  string
	TurnID     string
	ApprovalID string
	Decision   AgentApprovalDecision
}

// InterruptAgentTurnRequest 描述中断当前 provider turn 的请求。
type InterruptAgentTurnRequest struct {
	SessionID string
	TurnID    string
}

// AgentProvider 定义会议智能体的稳定业务能力。
type AgentProvider interface {
	CheckAvailability(ctx context.Context, request AgentAvailabilityRequest) (AgentAvailability, error)
	StartSession(ctx context.Context, request StartAgentSessionRequest) (AgentSession, error)
	ResumeSession(ctx context.Context, request ResumeAgentSessionRequest) (AgentSession, error)
	RunTurn(ctx context.Context, request RunAgentTurnRequest) (<-chan AgentEvent, error)
	RespondApproval(ctx context.Context, request RespondAgentApprovalRequest) error
	InterruptTurn(ctx context.Context, request InterruptAgentTurnRequest) error
	CloseSession(ctx context.Context, sessionID string) error
}
