package speaker

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingTrackProcessor struct {
	started chan string
	release chan struct{}
	mu      sync.Mutex
	active  int
	maximum int
}

// Process 记录并发数，并等待测试释放当前任务。
func (processor *blockingTrackProcessor) Process(ctx context.Context, trackID string, _ bool) error {
	processor.mu.Lock()
	processor.active++
	if processor.active > processor.maximum {
		processor.maximum = processor.active
	}
	processor.mu.Unlock()
	processor.started <- trackID
	select {
	case <-ctx.Done():
	case <-processor.release:
	}
	processor.mu.Lock()
	processor.active--
	processor.mu.Unlock()
	return nil
}

type fixedRecoverySource struct {
	mu  sync.Mutex
	ids []string
}

// ListRecoverableTrackIDs 首次返回固定恢复任务，后续返回空。
func (source *fixedRecoverySource) ListRecoverableTrackIDs(context.Context, int) ([]string, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	result := append([]string(nil), source.ids...)
	source.ids = nil
	return result, nil
}

// TestRunnerPool_UsesFixedWorkersAndWaitsForCancellation 验证固定并发、队列背压和 WaitGroup 退出。
func TestRunnerPool_UsesFixedWorkersAndWaitsForCancellation(t *testing.T) {
	processor := &blockingTrackProcessor{started: make(chan string, 3), release: make(chan struct{})}
	pool := NewRunnerPool(RunnerPoolDependencies{
		Processor: processor, Recovery: &fixedRecoverySource{},
		Config: RunnerPoolConfig{WorkerCount: 2, QueueCapacity: 3, RecoveryBatch: 3},
	})
	wake := make(chan string, 3)
	wake <- "track-1"
	wake <- "track-2"
	wake <- "track-3"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx, wake, nil) }()

	awaitTrack(t, processor.started)
	awaitTrack(t, processor.started)
	select {
	case third := <-processor.started:
		t.Fatalf("固定两个 worker 时第三个任务不应已启动：%s", third)
	default:
	}
	close(processor.release)
	awaitTrack(t, processor.started)
	cancel()
	if err := awaitPoolExit(t, done); err != nil {
		t.Fatalf("取消 RunnerPool 失败：%v", err)
	}
	if processor.maximum != 2 {
		t.Fatalf("worker 最大并发错误：%d", processor.maximum)
	}
}

// TestRunnerPool_RecoversPersistedTracksOnStart 验证应用启动立即按 SQLite 恢复任务。
func TestRunnerPool_RecoversPersistedTracksOnStart(t *testing.T) {
	processor := &blockingTrackProcessor{started: make(chan string, 1), release: make(chan struct{})}
	pool := NewRunnerPool(RunnerPoolDependencies{
		Processor: processor, Recovery: &fixedRecoverySource{ids: []string{"recovered"}},
		Config: RunnerPoolConfig{WorkerCount: 1, QueueCapacity: 1, RecoveryBatch: 1},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pool.Run(ctx, nil, nil) }()
	if got := awaitTrack(t, processor.started); got != "recovered" {
		t.Fatalf("恢复任务错误：%s", got)
	}
	cancel()
	if err := awaitPoolExit(t, done); err != nil {
		t.Fatalf("停止恢复 RunnerPool 失败：%v", err)
	}
}

// awaitTrack 等待 worker 启动，超时只用于防止测试挂死。
func awaitTrack(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case value := <-started:
		return value
	case <-time.After(time.Second):
		t.Fatal("等待 RunnerPool worker 超时")
		return ""
	}
}

// awaitPoolExit 等待 RunnerPool 完整回收所有 goroutine。
func awaitPoolExit(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("RunnerPool 未在取消后退出")
		return nil
	}
}
