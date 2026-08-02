package port

import "context"

// AgentAvailability 描述本机智能体是否可以使用。
type AgentAvailability struct {
	Available bool
	Version   string
	Message   string
}

// StartAgentSessionRequest 描述新建智能体会话所需的最小上下文。
type StartAgentSessionRequest struct {
	WorkingDirectory string
	Prompt           string
}

// ResumeAgentSessionRequest 描述恢复既有智能体会话的请求。
type ResumeAgentSessionRequest struct {
	SessionID        string
	WorkingDirectory string
}

// AgentSession 是智能体会话的稳定业务身份。
type AgentSession struct {
	ID string
}

// RunAgentTurnRequest 描述一次只读智能体问答。
type RunAgentTurnRequest struct {
	SessionID string
	Prompt    string
}

// AgentEvent 是智能体流式输出事件。
type AgentEvent struct {
	Type   string
	TurnID string
	Text   string
	Done   bool
}

// AgentProvider 定义会议智能体的稳定业务能力。
type AgentProvider interface {
	CheckAvailability(ctx context.Context) AgentAvailability
	StartSession(ctx context.Context, request StartAgentSessionRequest) (AgentSession, error)
	ResumeSession(ctx context.Context, request ResumeAgentSessionRequest) (AgentSession, error)
	RunTurn(ctx context.Context, request RunAgentTurnRequest) (<-chan AgentEvent, error)
	InterruptTurn(ctx context.Context, sessionID string, turnID string) error
}
