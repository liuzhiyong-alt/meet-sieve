package codex

import (
	"sync"

	"meet-sieve/internal/port"
)

const turnEventQueueSize = 64

// turnEmitter 用有界内存队列隔离 stdout reader 和业务消费者。
type turnEmitter struct {
	mu        sync.Mutex
	cond      *sync.Cond
	queue     []port.AgentEvent
	output    chan port.AgentEvent
	closed    bool
	closeOnce sync.Once
}

// newTurnEmitter 创建单 turn 事件投递器并启动唯一 delivery goroutine。
func newTurnEmitter() *turnEmitter {
	emitter := &turnEmitter{output: make(chan port.AgentEvent)}
	emitter.cond = sync.NewCond(&emitter.mu)
	go emitter.deliver()
	return emitter
}

// Events 返回只由 provider 关闭的业务事件通道。
func (emitter *turnEmitter) Events() <-chan port.AgentEvent { return emitter.output }

// Emit 快速入队；队列满时只合并 answer delta，审批和终态永不丢弃。
func (emitter *turnEmitter) Emit(event port.AgentEvent) {
	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	if emitter.closed {
		return
	}
	if len(emitter.queue) >= turnEventQueueSize {
		emitter.mergeDeltasLocked()
	}
	if event.Type == port.AgentEventAnswerDelta && len(emitter.queue) >= turnEventQueueSize {
		last := len(emitter.queue) - 1
		if last >= 0 && emitter.queue[last].Type == port.AgentEventAnswerDelta {
			emitter.queue[last].Delta += event.Delta
			return
		}
	}
	emitter.queue = append(emitter.queue, event)
	emitter.cond.Signal()
}

// Close 在已入队事件投递完成后关闭业务通道。
func (emitter *turnEmitter) Close() {
	emitter.closeOnce.Do(func() {
		emitter.mu.Lock()
		emitter.closed = true
		emitter.mu.Unlock()
		emitter.cond.Broadcast()
	})
}

// deliver 是唯一可能等待慢消费者的 goroutine，不阻塞 stdout reader。
func (emitter *turnEmitter) deliver() {
	defer close(emitter.output)
	for {
		emitter.mu.Lock()
		for len(emitter.queue) == 0 && !emitter.closed {
			emitter.cond.Wait()
		}
		if len(emitter.queue) == 0 && emitter.closed {
			emitter.mu.Unlock()
			return
		}
		event := emitter.queue[0]
		emitter.queue = emitter.queue[1:]
		emitter.mu.Unlock()
		emitter.output <- event
	}
}

// mergeDeltasLocked 把队列中的 delta 压成一个事件，为不可丢事件腾出容量。
func (emitter *turnEmitter) mergeDeltasLocked() {
	merged := make([]port.AgentEvent, 0, len(emitter.queue))
	for _, event := range emitter.queue {
		if event.Type == port.AgentEventAnswerDelta && len(merged) > 0 && merged[len(merged)-1].Type == port.AgentEventAnswerDelta {
			merged[len(merged)-1].Delta += event.Delta
			continue
		}
		merged = append(merged, event)
	}
	emitter.queue = merged
}
