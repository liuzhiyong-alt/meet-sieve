package transcript

import (
	"context"
	"fmt"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/port"
)

const finalPersistMaxAttempts = 3

const finalPersistRetryInterval = 25 * time.Millisecond

// FinalPersistFunc 持久化一条 provider final。
type FinalPersistFunc func(context.Context, port.TranscriptionEvent) error

// FinalFailureHandler 接收失败 final 及原始错误，调用方据此隔离单条或恢复 ASR。
type FinalFailureHandler func(port.TranscriptionEvent, error)

// FinalProcessorDependencies 描述 final 有界处理器依赖。
type FinalProcessorDependencies struct {
	// Capacity 是等待持久化的最大 final 数量。
	Capacity int
	// PersistTimeout 是单条 final 的持久化上限。
	PersistTimeout time.Duration
	// Persist 执行真实事件事务。
	Persist FinalPersistFunc
	// OnFailure 接收首个及后续持久化失败。
	OnFailure FinalFailureHandler
}

// FinalProcessor 串行持久化 final，并在关闭时排空全部已接受事件。
type FinalProcessor struct {
	dependencies FinalProcessorDependencies
	queue        chan port.TranscriptionEvent
	done         chan struct{}

	mu      sync.RWMutex
	started bool
	closed  bool
}

// NewFinalProcessor 创建有界处理器；显式 Start 前不启动 goroutine。
func NewFinalProcessor(dependencies FinalProcessorDependencies) *FinalProcessor {
	capacity := dependencies.Capacity
	if capacity <= 0 {
		capacity = 128
	}
	return &FinalProcessor{dependencies: dependencies, queue: make(chan port.TranscriptionEvent, capacity), done: make(chan struct{})}
}

// Start 启动唯一消费者；重复启动被拒绝。
func (processor *FinalProcessor) Start(ctx context.Context) error {
	if processor == nil || processor.dependencies.Persist == nil || processor.dependencies.PersistTimeout <= 0 || ctx == nil {
		return fmt.Errorf("final processor 依赖无效")
	}
	processor.mu.Lock()
	defer processor.mu.Unlock()
	if processor.started || processor.closed {
		return fmt.Errorf("final processor 已启动或关闭")
	}
	processor.started = true
	go processor.run(ctx)
	return nil
}

// TrySubmit 非阻塞接受 final；返回 false 表示队列背压或处理器不可用。
func (processor *FinalProcessor) TrySubmit(event port.TranscriptionEvent) bool {
	if processor == nil || event.Type != port.TranscriptionFinal {
		return false
	}
	processor.mu.RLock()
	defer processor.mu.RUnlock()
	if !processor.started || processor.closed {
		return false
	}
	select {
	case processor.queue <- event:
		return true
	default:
		return false
	}
}

// CloseAndWait 停止接收并等待已接受 final 排空；仅首次调用关闭队列。
func (processor *FinalProcessor) CloseAndWait(ctx context.Context) error {
	if processor == nil || ctx == nil {
		return fmt.Errorf("final processor 不可用")
	}
	processor.mu.Lock()
	if !processor.started {
		processor.mu.Unlock()
		return fmt.Errorf("final processor 尚未启动")
	}
	if !processor.closed {
		processor.closed = true
		close(processor.queue)
	}
	processor.mu.Unlock()
	select {
	case <-processor.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run 串行处理队列；父 context 取消后仍排空已经接受的 final。
func (processor *FinalProcessor) run(parent context.Context) {
	defer close(processor.done)
	for event := range processor.queue {
		persistContext, cancel := context.WithTimeout(context.WithoutCancel(parent), processor.dependencies.PersistTimeout)
		err := processor.persistWithRetry(persistContext, event)
		cancel()
		if err != nil && processor.dependencies.OnFailure != nil {
			processor.dependencies.OnFailure(event, err)
		}
	}
}

// persistWithRetry 仅在单条 final 的总超时边界内重试明确可恢复错误。
func (processor *FinalProcessor) persistWithRetry(ctx context.Context, event port.TranscriptionEvent) error {
	var lastErr error
	for attempt := 1; attempt <= finalPersistMaxAttempts; attempt++ {
		lastErr = processor.dependencies.Persist(ctx, event)
		if lastErr == nil || !isRetryablePersistError(lastErr) || attempt == finalPersistMaxAttempts {
			return lastErr
		}
		timer := time.NewTimer(finalPersistRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return lastErr
		case <-timer.C:
		}
	}
	return lastErr
}

// isRetryablePersistError 只依赖统一 AppError 元数据，不通过错误文本猜测。
func isRetryablePersistError(err error) bool {
	appErr := apperr.Normalize(err)
	return appErr != nil && appErr.Retryable
}

// PartialPublisher 发布一条只用于 UI 的临时转写。
type PartialPublisher func(port.TranscriptionEvent)

// PartialClearEvent 描述需要从 UI 清除的 session 或单条临时转写。
type PartialClearEvent struct {
	MeetingID  string
	SessionID  string
	ResultID   string
	Generation int64
}

// PartialClearPublisher 发布 partial 生命周期终止事件。
type PartialClearPublisher func(PartialClearEvent)

type partialState struct {
	revision        int64
	lastPublishedAt time.Time
}

// PartialProjector 按 provider result key 保留最大 revision，并限制为每 key 最多 10 Hz。
type PartialProjector struct {
	mu      sync.Mutex
	now     func() time.Time
	publish PartialPublisher
	clear   PartialClearPublisher
	states  map[string]partialState
}

// NewPartialProjector 创建内存 partial 投影；它不写数据库或日志。
func NewPartialProjector(now func() time.Time, publish PartialPublisher, clearPublisher PartialClearPublisher) *PartialProjector {
	return &PartialProjector{now: now, publish: publish, clear: clearPublisher, states: make(map[string]partialState)}
}

// Accept 接受更高 revision，并在达到 100ms 间隔时同步发布。
func (projector *PartialProjector) Accept(event port.TranscriptionEvent) {
	if projector == nil || projector.now == nil || projector.publish == nil || event.Type != port.TranscriptionPartial || event.ResultID == "" || event.Revision <= 0 {
		return
	}
	projector.mu.Lock()
	state := projector.states[event.ResultID]
	if event.Revision <= state.revision {
		projector.mu.Unlock()
		return
	}
	state.revision = event.Revision
	now := projector.now()
	shouldPublish := state.lastPublishedAt.IsZero() || now.Sub(state.lastPublishedAt) >= 100*time.Millisecond
	if shouldPublish {
		state.lastPublishedAt = now
	}
	projector.states[event.ResultID] = state
	projector.mu.Unlock()
	if shouldPublish {
		projector.publish(event)
	}
}

// Clear 在 final 到达时删除并通知 UI 清除指定 partial。
func (projector *PartialProjector) Clear(event port.TranscriptionEvent) {
	if projector == nil {
		return
	}
	projector.mu.Lock()
	delete(projector.states, event.ResultID)
	projector.mu.Unlock()
	projector.publishClear(PartialClearEvent{MeetingID: event.MeetingID, SessionID: event.SessionID, ResultID: event.ResultID, Generation: event.Generation})
}

// ClearAll 清空指定物理 session，并建立拒绝迟到事件的 UI tombstone。
func (projector *PartialProjector) ClearAll(meetingID string, sessionID string, generation int64) {
	if projector == nil {
		return
	}
	projector.mu.Lock()
	clear(projector.states)
	projector.mu.Unlock()
	projector.publishClear(PartialClearEvent{MeetingID: meetingID, SessionID: sessionID, Generation: generation})
}

// publishClear 在配置发布边界时同步发送生命周期事件。
func (projector *PartialProjector) publishClear(event PartialClearEvent) {
	if projector.clear != nil && event.MeetingID != "" && event.SessionID != "" {
		projector.clear(event)
	}
}

// Size 返回内存 partial key 数量，仅用于运行状态和测试。
func (projector *PartialProjector) Size() int {
	if projector == nil {
		return 0
	}
	projector.mu.Lock()
	defer projector.mu.Unlock()
	return len(projector.states)
}
