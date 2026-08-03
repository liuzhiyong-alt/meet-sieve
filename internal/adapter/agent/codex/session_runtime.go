package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

type sessionRuntime struct {
	id        string
	threadID  string
	process   *process
	rpc       *RPCClient
	approvals *ApprovalBridge
	context   context.Context
	cancel    context.CancelFunc
	loggedIn  bool
	mu        sync.Mutex
	active    *turnRuntime
	closeOnce sync.Once
	closeErr  error
}

type turnRuntime struct {
	localID       string
	providerID    string
	kind          port.AgentTurnKind
	emitter       *turnEmitter
	parser        *domainagent.AnswerDeltaParser
	finalSeen     bool
	terminal      bool
	interruptOnce sync.Once
}

// newSessionRuntime 创建进程唯一 owner，并立即启动 RPC reader/router 生命周期。
func newSessionRuntime(process *process) *sessionRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	rpc := NewRPCClient(process.codec)
	runtime := &sessionRuntime{process: process, rpc: rpc, context: ctx, cancel: cancel}
	runtime.approvals = NewApprovalBridge(rpc)
	rpc.Start(ctx)
	go runtime.route()
	return runtime
}

// initialize 完成 initialize/initialized/account/read，不记录账号正文。
func (runtime *sessionRuntime) initialize(ctx context.Context) error {
	callContext, cancel := boundedContext(ctx, defaultRPCTimeout)
	defer cancel()
	var initialized InitializeResult
	if err := runtime.rpc.Call(callContext, "initialize", initializeParams(), &initialized); err != nil {
		return apperr.Dependency(apperr.CodeAgentInitializeFailed, err, apperr.WithOp("agent.codex.initialize"))
	}
	if initialized.UserAgent == "" || initialized.PlatformOS == "" || initialized.PlatformFamily == "" {
		return apperr.Dependency(apperr.CodeAgentProtocolIncompatible, errors.New("initialize fields missing"), apperr.WithOp("agent.codex.initialize.result"))
	}
	if err := runtime.rpc.Notify("initialized", nil); err != nil {
		return apperr.Dependency(apperr.CodeAgentInitializeFailed, err, apperr.WithOp("agent.codex.initialized"))
	}
	var account accountResponse
	if err := runtime.rpc.Call(callContext, "account/read", map[string]any{"refreshToken": false}, &account); err != nil {
		return apperr.Dependency(apperr.CodeAgentInitializeFailed, err, apperr.WithOp("agent.codex.account"))
	}
	runtime.loggedIn = !account.RequiresOpenAIAuth || (len(account.Account) > 0 && !bytes.Equal(account.Account, []byte("null")))
	if !runtime.loggedIn {
		return apperr.Dependency(apperr.CodeAgentNotLoggedIn, errors.New("account is null"), apperr.WithOp("agent.codex.account"))
	}
	return nil
}

// startThread 创建沿用用户原生 sandbox、审批和模型配置的新 thread。
func (runtime *sessionRuntime) startThread(ctx context.Context, cwd string, developerInstructions string) (threadResponse, error) {
	var response threadResponse
	params := map[string]any{"cwd": cwd, "developerInstructions": developerInstructions}
	callContext, cancel := boundedContext(ctx, defaultRPCTimeout)
	defer cancel()
	if err := runtime.rpc.Call(callContext, "thread/start", params, &response); err != nil {
		return threadResponse{}, apperr.Dependency(apperr.CodeAgentInitializeFailed, err, apperr.WithOp("agent.codex.thread.start"))
	}
	if response.Thread.ID == "" {
		return threadResponse{}, apperr.Dependency(apperr.CodeAgentProtocolIncompatible, errors.New("thread id missing"), apperr.WithOp("agent.codex.thread.start.result"))
	}
	return response, nil
}

// resumeThread 恢复既有 thread，仍沿用用户当前原生权限。
func (runtime *sessionRuntime) resumeThread(ctx context.Context, threadID string, cwd string) (threadResponse, error) {
	var response threadResponse
	callContext, cancel := boundedContext(ctx, defaultRPCTimeout)
	defer cancel()
	if err := runtime.rpc.Call(callContext, "thread/resume", map[string]any{"threadId": threadID, "cwd": cwd}, &response); err != nil {
		return threadResponse{}, err
	}
	if response.Thread.ID == "" {
		return threadResponse{}, fmt.Errorf("恢复 thread 缺少 ID")
	}
	return response, nil
}

// runTurn 取得 session 内唯一活动 turn 并调用 turn/start。
func (runtime *sessionRuntime) runTurn(ctx context.Context, request port.RunAgentTurnRequest) (<-chan port.AgentEvent, error) {
	if request.TurnID == "" || !request.Kind.Valid() || request.Input == "" || len(request.OutputSchema) == 0 {
		return nil, apperr.Biz(apperr.CodeAgentQuestionInvalid, apperr.WithOp("agent.codex.turn.input"))
	}
	emitter := newTurnEmitter()
	turn := &turnRuntime{localID: request.TurnID, kind: request.Kind, emitter: emitter, parser: domainagent.NewAnswerDeltaParser()}
	runtime.mu.Lock()
	if runtime.active != nil {
		runtime.mu.Unlock()
		emitter.Close()
		return nil, apperr.Biz(apperr.CodeAgentBusy, apperr.WithOp("agent.codex.turn.busy"))
	}
	runtime.active = turn
	runtime.mu.Unlock()

	params := map[string]any{
		"threadId":     runtime.threadID,
		"input":        []map[string]any{{"type": "text", "text": request.Input}},
		"outputSchema": json.RawMessage(request.OutputSchema),
	}
	callContext := ctx
	var cancel context.CancelFunc = func() {}
	if !request.Deadline.IsZero() {
		callContext, cancel = context.WithDeadline(ctx, request.Deadline)
	}
	defer cancel()
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := runtime.rpc.Call(callContext, "turn/start", params, &response); err != nil {
		runtime.finishWithFailure(turn, port.AgentEventFailed, "AGENT_INITIALIZE_FAILED")
		return nil, apperr.Dependency(apperr.CodeAgentInitializeFailed, err, apperr.WithOp("agent.codex.turn.start"))
	}
	if response.Turn.ID == "" {
		runtime.finishWithFailure(turn, port.AgentEventFailed, "AGENT_PROTOCOL_INCOMPATIBLE")
		return nil, apperr.Dependency(apperr.CodeAgentProtocolIncompatible, errors.New("provider turn id missing"), apperr.WithOp("agent.codex.turn.start.result"))
	}
	runtime.mu.Lock()
	if runtime.active == turn && turn.providerID == "" {
		turn.providerID = response.Turn.ID
	}
	startedEvent := runtime.event(turn, port.AgentEventTurnStarted)
	runtime.mu.Unlock()
	turn.emitter.Emit(startedEvent)
	return emitter.Events(), nil
}

// interrupt 对当前 provider turn 最多发送一次 turn/interrupt。
func (runtime *sessionRuntime) interrupt(ctx context.Context, providerTurnID string) error {
	runtime.mu.Lock()
	turn := runtime.active
	if turn == nil || turn.providerID == "" || (providerTurnID != "" && turn.providerID != providerTurnID) {
		runtime.mu.Unlock()
		return nil
	}
	turnID := turn.providerID
	runtime.mu.Unlock()
	if err := runtime.approvals.DeclineTurn(context.Background(), runtime.id, turnID); err != nil {
		return err
	}
	var interruptErr error
	turn.interruptOnce.Do(func() {
		interruptErr = runtime.rpc.Call(ctx, "turn/interrupt", map[string]string{"threadId": runtime.threadID, "turnId": turnID}, nil)
	})
	return interruptErr
}

// close 由 session owner 执行一次 interrupt、stdin close、三秒等待与最终 kill。
func (runtime *sessionRuntime) close(ctx context.Context) error {
	runtime.closeOnce.Do(func() {
		runtime.mu.Lock()
		turn := runtime.active
		providerTurnID := ""
		if turn != nil {
			providerTurnID = turn.providerID
		}
		runtime.mu.Unlock()
		if providerTurnID != "" {
			interruptContext, cancel := boundedContext(ctx, time.Second)
			_ = runtime.interrupt(interruptContext, providerTurnID)
			cancel()
		}
		runtime.cancel()
		runtime.closeErr = runtime.process.closeAndWait(ctx)
		runtime.rpc.Close(runtime.closeErr)
	})
	return runtime.closeErr
}

// route 是 session 唯一协议路由 goroutine。
func (runtime *sessionRuntime) route() {
	for {
		select {
		case notification := <-runtime.rpc.Notifications():
			runtime.routeNotification(notification)
		case request := <-runtime.rpc.ServerRequests():
			runtime.routeServerRequest(request)
		case <-runtime.rpc.Done():
			runtime.failActive("AGENT_INITIALIZE_FAILED")
			return
		case <-runtime.context.Done():
			return
		}
	}
}

// routeNotification 只解析当前实现登记的 turn 通知，未知通知安全忽略。
func (runtime *sessionRuntime) routeNotification(message Message) {
	runtime.mu.Lock()
	turn := runtime.active
	if turn == nil {
		runtime.mu.Unlock()
		return
	}
	switch message.Method {
	case "turn/started":
		var params turnNotification
		if json.Unmarshal(message.Params, &params) == nil && params.ThreadID == runtime.threadID {
			turn.providerID = params.Turn.ID
		}
	case "item/agentMessage/delta":
		var params deltaNotification
		if json.Unmarshal(message.Params, &params) == nil && runtime.matchesTurn(turn, params.ThreadID, params.TurnID) {
			if delta := turn.parser.Push([]byte(params.Delta)); delta != "" {
				turn.emitter.Emit(runtime.eventWithDelta(turn, delta))
			}
		}
	case "item/completed":
		runtime.routeItemCompletedLocked(turn, message.Params)
	case "turn/completed":
		runtime.routeTurnCompletedLocked(turn, message.Params)
	}
	runtime.mu.Unlock()
}

// routeServerRequest 映射审批；无活动 provider turn 时直接返回 method not supported。
func (runtime *sessionRuntime) routeServerRequest(message Message) {
	runtime.mu.Lock()
	turn := runtime.active
	if turn == nil || turn.providerID == "" || message.ID == nil {
		runtime.mu.Unlock()
		if message.ID != nil {
			_ = runtime.rpc.Respond(*message.ID, nil, &ProtocolError{Code: -32601, Message: "No active turn"})
		}
		return
	}
	providerTurnID := turn.providerID
	runtime.mu.Unlock()
	pending, err := runtime.approvals.Handle(message, runtime.id, providerTurnID)
	if err != nil {
		runtime.failActive("AGENT_APPROVAL_UNSUPPORTED")
		return
	}
	runtime.mu.Lock()
	if runtime.active == turn {
		event := runtime.event(turn, port.AgentEventApprovalRequested)
		event.Approval = &pending
		turn.emitter.Emit(event)
	}
	runtime.mu.Unlock()
}

// routeItemCompletedLocked 保存 agentMessage 完整 text，其他 item 不进入业务事件。
func (runtime *sessionRuntime) routeItemCompletedLocked(turn *turnRuntime, content []byte) {
	var params itemCompletedNotification
	if json.Unmarshal(content, &params) != nil || !runtime.matchesTurn(turn, params.ThreadID, params.TurnID) || params.Item.Type != "agentMessage" {
		return
	}
	turn.finalSeen = true
	event := runtime.event(turn, port.AgentEventFinalOutput)
	event.FinalOutput = &port.AgentFinalOutput{JSON: []byte(params.Item.Text)}
	turn.emitter.Emit(event)
}

// routeTurnCompletedLocked 只有已收到最终 agentMessage 且 status completed 才发布成功。
func (runtime *sessionRuntime) routeTurnCompletedLocked(turn *turnRuntime, content []byte) {
	var params turnNotification
	if json.Unmarshal(content, &params) != nil || !runtime.matchesTurn(turn, params.ThreadID, params.Turn.ID) {
		return
	}
	eventType := port.AgentEventFailed
	failureCode := "AGENT_INITIALIZE_FAILED"
	if params.Turn.Status == "completed" && turn.finalSeen {
		eventType = port.AgentEventCompleted
		failureCode = ""
	} else if params.Turn.Status == "interrupted" {
		eventType = port.AgentEventCancelled
		failureCode = "AGENT_TURN_CANCELLED"
	}
	event := runtime.event(turn, eventType)
	event.FailureCode = failureCode
	turn.emitter.Emit(event)
	turn.terminal = true
	turn.emitter.Close()
	runtime.active = nil
	_ = runtime.approvals.DeclineTurn(context.Background(), runtime.id, turn.providerID)
}

// failActive 将进程或协议终止传播到当前 turn。
func (runtime *sessionRuntime) failActive(code string) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.active != nil {
		runtime.finishWithFailureLocked(runtime.active, port.AgentEventFailed, code)
	}
}

// finishWithFailure 在持锁前提外安全终结指定 turn。
func (runtime *sessionRuntime) finishWithFailure(turn *turnRuntime, eventType port.AgentEventType, code string) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.active == turn {
		runtime.finishWithFailureLocked(turn, eventType, code)
	}
}

// finishWithFailureLocked 发布不可丢终态并清理活动指针，调用方必须持锁。
func (runtime *sessionRuntime) finishWithFailureLocked(turn *turnRuntime, eventType port.AgentEventType, code string) {
	event := runtime.event(turn, eventType)
	event.FailureCode = code
	turn.emitter.Emit(event)
	turn.emitter.Close()
	turn.terminal = true
	runtime.active = nil
	_ = runtime.approvals.DeclineTurn(context.Background(), runtime.id, turn.providerID)
}

// matchesTurn 判断通知属于当前 thread/provider turn。
func (runtime *sessionRuntime) matchesTurn(turn *turnRuntime, threadID string, turnID string) bool {
	return threadID == runtime.threadID && turn.providerID != "" && turn.providerID == turnID
}

// event 构造不泄漏 Codex envelope 的稳定业务事件。
func (runtime *sessionRuntime) event(turn *turnRuntime, eventType port.AgentEventType) port.AgentEvent {
	return port.AgentEvent{Type: eventType, SessionID: runtime.id, TurnID: turn.localID, ProviderTurnID: turn.providerID}
}

// eventWithDelta 构造已完整解码的回答增量。
func (runtime *sessionRuntime) eventWithDelta(turn *turnRuntime, delta string) port.AgentEvent {
	event := runtime.event(turn, port.AgentEventAnswerDelta)
	event.Delta = delta
	return event
}

// boundedContext 为 RPC 设置不超过调用方 deadline 的独立超时。
func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

// initializeParams 返回关闭实验能力的固定握手参数。
func initializeParams() map[string]any {
	return map[string]any{
		"clientInfo":   map[string]string{"name": "meetsieve", "title": "MeetSieve", "version": "step7"},
		"capabilities": map[string]bool{"experimentalApi": false},
	}
}

type turnNotification struct {
	ThreadID string `json:"threadId"`
	Turn     struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"turn"`
}

type deltaNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Delta    string `json:"delta"`
}

type itemCompletedNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
}
