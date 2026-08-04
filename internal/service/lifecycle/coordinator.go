// Package lifecycle 提供按会议串行的维护锁和后台任务停止协议。
package lifecycle

import (
	"context"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
)

// TaskKind 表示删除前必须按固定顺序停止的任务类型。
type TaskKind string

const (
	TaskGap       TaskKind = "gap"
	TaskMinutes   TaskKind = "minutes"
	TaskCodex     TaskKind = "codex"
	TaskPlayback  TaskKind = "playback"
	TaskDownload  TaskKind = "download"
	TaskAudioClip TaskKind = "audio_clip"
)

var orderedTaskKinds = []TaskKind{TaskGap, TaskMinutes, TaskCodex, TaskPlayback, TaskDownload, TaskAudioClip}

// StopOrder 返回维护协议的固定停止顺序副本。
func StopOrder() []TaskKind { return append([]TaskKind(nil), orderedTaskKinds...) }

// TaskHandle 描述一个可安全停止并等待退出的会议后台任务。
type TaskHandle interface {
	Kind() TaskKind
	Stop()
	Done() <-chan struct{}
}

// Registry 保存活动任务，并在维护期间拒绝同场新任务。
type Registry struct {
	mu      sync.Mutex
	blocked map[string]bool
	tasks   map[string]map[TaskKind][]TaskHandle
}

// NewRegistry 创建空任务登记表。
func NewRegistry() *Registry {
	return &Registry{blocked: make(map[string]bool), tasks: make(map[string]map[TaskKind][]TaskHandle)}
}

// Register 登记活动任务；会议进入维护后拒绝新登记。
func (registry *Registry) Register(meetingID string, task TaskHandle) error {
	if registry == nil || meetingID == "" || task == nil {
		return apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("lifecycle.registry.register"))
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.blocked[meetingID] {
		return apperr.Biz(apperr.CodeMeetingMaintenanceLocked, apperr.WithOp("lifecycle.registry.blocked"))
	}
	if registry.tasks[meetingID] == nil {
		registry.tasks[meetingID] = make(map[TaskKind][]TaskHandle)
	}
	registry.tasks[meetingID][task.Kind()] = append(registry.tasks[meetingID][task.Kind()], task)
	return nil
}

// HasActive 返回会议是否仍存在未退出的登记任务。
func (registry *Registry) HasActive(meetingID string) bool {
	if registry == nil {
		return false
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.pruneLocked(meetingID)
	return len(registry.tasks[meetingID]) > 0
}

// blockAndSnapshot 阻止新任务，并按停止顺序获取活动任务快照。
func (registry *Registry) blockAndSnapshot(meetingID string) ([]TaskHandle, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.blocked[meetingID] {
		return nil, apperr.Biz(apperr.CodeMeetingMaintenanceLocked, apperr.WithOp("lifecycle.registry.acquire"))
	}
	registry.blocked[meetingID] = true
	registry.pruneLocked(meetingID)
	var result []TaskHandle
	for _, kind := range orderedTaskKinds {
		result = append(result, registry.tasks[meetingID][kind]...)
	}
	return result, nil
}

// unblock 清除维护阻断，并丢弃已经退出的任务。
func (registry *Registry) unblock(meetingID string) {
	registry.mu.Lock()
	delete(registry.blocked, meetingID)
	registry.pruneLocked(meetingID)
	registry.mu.Unlock()
}

// pruneLocked 清理已经退出的任务句柄。
func (registry *Registry) pruneLocked(meetingID string) {
	byKind := registry.tasks[meetingID]
	for kind, tasks := range byKind {
		active := tasks[:0]
		for _, task := range tasks {
			select {
			case <-task.Done():
			default:
				active = append(active, task)
			}
		}
		if len(active) == 0 {
			delete(byKind, kind)
		} else {
			byKind[kind] = active
		}
	}
	if len(byKind) == 0 {
		delete(registry.tasks, meetingID)
	}
}

// Coordinator 是每场维护 owner 的唯一内存协调器。
type Coordinator struct {
	mu       sync.Mutex
	owners   map[string]bool
	registry *Registry
	stopper  MeetingStopper
}

// MeetingStopper 停止尚未接入 TaskHandle 的既有同场运行时，并等待安全终点。
type MeetingStopper interface {
	StopMeeting(context.Context, string) error
}

// Lease 表示完成任务交接后的维护独占权，必须释放。
type Lease struct {
	coordinator *Coordinator
	meetingID   string
	once        sync.Once
}

// NewCoordinator 创建会议维护协调器。
func NewCoordinator(registry *Registry) *Coordinator {
	return NewCoordinatorWithStopper(registry, nil)
}

// NewCoordinatorWithStopper 创建同时兼容既有运行时停止协议的维护协调器。
func NewCoordinatorWithStopper(registry *Registry, stopper MeetingStopper) *Coordinator {
	if registry == nil {
		registry = NewRegistry()
	}
	return &Coordinator{owners: make(map[string]bool), registry: registry, stopper: stopper}
}

// Acquire 阻止新任务、停止现有任务并在时限内等待安全交接。
func (coordinator *Coordinator) Acquire(ctx context.Context, meetingID string, timeout time.Duration) (*Lease, error) {
	if coordinator == nil || meetingID == "" || timeout <= 0 {
		return nil, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("lifecycle.acquire.validate"))
	}
	if !coordinator.claim(meetingID) {
		return nil, apperr.Biz(apperr.CodeMeetingMaintenanceLocked, apperr.WithOp("lifecycle.acquire.claim"))
	}
	tasks, err := coordinator.registry.blockAndSnapshot(meetingID)
	if err != nil {
		coordinator.releaseOwner(meetingID)
		return nil, err
	}
	stopContext, cancelStop := context.WithTimeout(ctx, timeout)
	defer cancelStop()
	if coordinator.stopper != nil {
		if err := coordinator.stopper.StopMeeting(stopContext, meetingID); err != nil {
			coordinator.registry.unblock(meetingID)
			coordinator.releaseOwner(meetingID)
			return nil, apperr.Biz(apperr.CodeDeleteTaskStopTimeout, apperr.WithOp("lifecycle.acquire.stop_existing"))
		}
	}
	if err := stopAndWait(stopContext, tasks, timeout); err != nil {
		coordinator.registry.unblock(meetingID)
		coordinator.releaseOwner(meetingID)
		return nil, err
	}
	return &Lease{coordinator: coordinator, meetingID: meetingID}, nil
}

// Release 幂等释放维护锁并允许新任务。
func (lease *Lease) Release() {
	if lease == nil || lease.coordinator == nil {
		return
	}
	lease.once.Do(func() {
		lease.coordinator.registry.unblock(lease.meetingID)
		lease.coordinator.releaseOwner(lease.meetingID)
	})
}

// claim 原子占用单场维护 owner。
func (coordinator *Coordinator) claim(meetingID string) bool {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.owners[meetingID] {
		return false
	}
	coordinator.owners[meetingID] = true
	return true
}

// releaseOwner 释放单场 owner 标记。
func (coordinator *Coordinator) releaseOwner(meetingID string) {
	coordinator.mu.Lock()
	delete(coordinator.owners, meetingID)
	coordinator.mu.Unlock()
}

// stopAndWait 发出停止信号并等待所有任务退出。
func stopAndWait(parent context.Context, tasks []TaskHandle, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	for _, task := range tasks {
		task.Stop()
	}
	for _, task := range tasks {
		select {
		case <-task.Done():
		case <-ctx.Done():
			return apperr.Biz(apperr.CodeDeleteTaskStopTimeout, apperr.WithOp("lifecycle.acquire.wait"))
		}
	}
	return nil
}
