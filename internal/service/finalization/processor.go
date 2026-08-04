// Package finalization 编排本地保存完成后的后台会后处理。
package finalization

import (
	"context"
	"fmt"
	"sync"
)

// GapProcessor 串行处理一项缺口；remaining 表示仍需继续读取下一项。
type GapProcessor interface {
	ProcessNext(context.Context, string) (remaining bool, err error)
}

// RequestedGapProcessor 使用主持人请求 ID 执行明确的开始或重试动作。
type RequestedGapProcessor interface {
	ProcessNextRequested(context.Context, string, string, []string) (remaining bool, err error)
}

// FinalSyncer 在首轮 gap 全部终结后执行一次 Codex 结束同步。
type FinalSyncer interface {
	SyncFinal(context.Context, string) error
}

// RecoveryStore 只收敛遗留运行状态，不发起外部请求。
type RecoveryStore interface {
	RecoverInterrupted(context.Context) error
}

// ProcessorDependencies 描述会后 processor 的真实边界。
type ProcessorDependencies struct {
	Gaps     GapProcessor
	Syncer   FinalSyncer
	Recovery RecoveryStore
}

// PostMeetingProcessor 保证每场会议只有一个后台 owner。
type PostMeetingProcessor struct {
	gaps     GapProcessor
	syncer   FinalSyncer
	recovery RecoveryStore
	mu       sync.Mutex
	root     context.Context
	cancel   context.CancelFunc
	owners   map[string]*processorOwner
}

type processorOwner struct {
	cancel    context.CancelFunc
	done      chan struct{}
	requestID string
	gapIDs    []string
}

// NewPostMeetingProcessor 创建 processor；构造阶段不启动 goroutine。
func NewPostMeetingProcessor(dependencies ProcessorDependencies) *PostMeetingProcessor {
	return &PostMeetingProcessor{
		gaps: dependencies.Gaps, syncer: dependencies.Syncer, recovery: dependencies.Recovery,
		owners: make(map[string]*processorOwner),
	}
}

// Start 绑定应用生命周期 context，但不自动恢复或联网。
func (processor *PostMeetingProcessor) Start(ctx context.Context) error {
	if processor == nil || processor.gaps == nil || processor.syncer == nil || ctx == nil {
		return fmt.Errorf("会后 processor 依赖无效")
	}
	processor.mu.Lock()
	defer processor.mu.Unlock()
	if processor.root != nil {
		return nil
	}
	processor.root, processor.cancel = context.WithCancel(ctx)
	return nil
}

// Recover 只把上次异常退出遗留的运行状态收敛为可恢复终态。
func (processor *PostMeetingProcessor) Recover(ctx context.Context) error {
	if processor == nil {
		return fmt.Errorf("会后 processor 不可用")
	}
	if processor.recovery == nil {
		return nil
	}
	return processor.recovery.RecoverInterrupted(ctx)
}

// Trigger 异步启动指定会议；已有 owner 时返回 false。
func (processor *PostMeetingProcessor) Trigger(meetingID string) bool {
	return processor.trigger(meetingID, "", nil)
}

// TriggerRequested 使用明确请求 ID 启动后台处理；同场已有 owner 时拒绝。
func (processor *PostMeetingProcessor) TriggerRequested(meetingID string, requestID string, gapIDs []string) bool {
	if requestID == "" {
		return false
	}
	return processor.trigger(meetingID, requestID, gapIDs)
}

// trigger 取得单场后台 owner。
func (processor *PostMeetingProcessor) trigger(meetingID string, requestID string, gapIDs []string) bool {
	if processor == nil || meetingID == "" {
		return false
	}
	processor.mu.Lock()
	if processor.root == nil || processor.root.Err() != nil {
		processor.mu.Unlock()
		return false
	}
	if _, exists := processor.owners[meetingID]; exists {
		processor.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(processor.root)
	owner := &processorOwner{cancel: cancel, done: make(chan struct{}), requestID: requestID, gapIDs: append([]string(nil), gapIDs...)}
	processor.owners[meetingID] = owner
	processor.mu.Unlock()
	go processor.run(ctx, meetingID, owner)
	return true
}

// StopMeeting 取消指定会议 owner；持久状态由被取消的 runner 收敛。
func (processor *PostMeetingProcessor) StopMeeting(meetingID string) bool {
	if processor == nil || meetingID == "" {
		return false
	}
	processor.mu.Lock()
	owner := processor.owners[meetingID]
	processor.mu.Unlock()
	if owner == nil {
		return false
	}
	owner.cancel()
	return true
}

// StopMeetingAndWait 取消指定会议 owner，并等待它到达持久安全终点。
func (processor *PostMeetingProcessor) StopMeetingAndWait(ctx context.Context, meetingID string) error {
	if processor == nil || meetingID == "" {
		return nil
	}
	processor.mu.Lock()
	owner := processor.owners[meetingID]
	if owner != nil {
		owner.cancel()
	}
	processor.mu.Unlock()
	if owner == nil {
		return nil
	}
	select {
	case <-owner.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop 取消全部后台请求并等待 goroutine 退出。
func (processor *PostMeetingProcessor) Stop(ctx context.Context) error {
	if processor == nil {
		return nil
	}
	processor.mu.Lock()
	rootCancel := processor.cancel
	owners := make([]*processorOwner, 0, len(processor.owners))
	for _, owner := range processor.owners {
		owners = append(owners, owner)
		owner.cancel()
	}
	processor.root, processor.cancel = nil, nil
	processor.mu.Unlock()
	if rootCancel != nil {
		rootCancel()
	}
	for _, owner := range owners {
		select {
		case <-owner.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// run 串行耗尽 gap 后执行唯一结束同步；错误由持久状态和上层通知呈现。
func (processor *PostMeetingProcessor) run(ctx context.Context, meetingID string, owner *processorOwner) {
	defer processor.finishOwner(meetingID, owner)
	if owner.requestID != "" {
		requested, ok := processor.gaps.(RequestedGapProcessor)
		if !ok {
			return
		}
		remaining, err := requested.ProcessNextRequested(ctx, meetingID, owner.requestID, owner.gapIDs)
		if err != nil {
			return
		}
		if !remaining {
			_ = processor.syncer.SyncFinal(ctx, meetingID)
			return
		}
	}
	for {
		remaining, err := processor.gaps.ProcessNext(ctx, meetingID)
		if err != nil {
			return
		}
		if !remaining {
			break
		}
	}
	_ = processor.syncer.SyncFinal(ctx, meetingID)
}

// finishOwner 删除当前 owner 并通知 Stop 等待者。
func (processor *PostMeetingProcessor) finishOwner(meetingID string, owner *processorOwner) {
	processor.mu.Lock()
	if processor.owners[meetingID] == owner {
		delete(processor.owners, meetingID)
	}
	close(owner.done)
	processor.mu.Unlock()
}
