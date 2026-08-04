package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"
)

type testTask struct {
	kind    TaskKind
	stopped chan struct{}
	once    sync.Once
}

func (task *testTask) Kind() TaskKind        { return task.kind }
func (task *testTask) Stop()                 { task.once.Do(func() { close(task.stopped) }) }
func (task *testTask) Done() <-chan struct{} { return task.stopped }

// TestCoordinatorSerializesMeeting 验证同场维护串行、不同会议互不阻塞。
func TestCoordinatorSerializesMeeting(t *testing.T) {
	coordinator := NewCoordinator(nil)
	first, err := coordinator.Acquire(context.Background(), "meeting-a", time.Second)
	if err != nil {
		t.Fatalf("首次获取维护锁失败: %v", err)
	}
	defer first.Release()
	if _, err := coordinator.Acquire(context.Background(), "meeting-a", time.Second); err == nil {
		t.Fatal("同一会议不应允许第二个维护 owner")
	}
	other, err := coordinator.Acquire(context.Background(), "meeting-b", time.Second)
	if err != nil {
		t.Fatalf("不同会议不应互相阻塞: %v", err)
	}
	other.Release()
}

// TestCoordinatorStopsTasksInOrder 验证维护锁按登记顺序停止全部任务后才交接。
func TestCoordinatorStopsTasksInOrder(t *testing.T) {
	registry := NewRegistry()
	for _, kind := range StopOrder() {
		task := &testTask{kind: kind, stopped: make(chan struct{})}
		if err := registry.Register("meeting-a", task); err != nil {
			t.Fatalf("登记任务失败: %v", err)
		}
	}
	lease, err := NewCoordinator(registry).Acquire(context.Background(), "meeting-a", time.Second)
	if err != nil {
		t.Fatalf("获取维护锁失败: %v", err)
	}
	lease.Release()
	if registry.HasActive("meeting-a") {
		t.Fatal("维护交接后不应仍有活动任务")
	}
}
