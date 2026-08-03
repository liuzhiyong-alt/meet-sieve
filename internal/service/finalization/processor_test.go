package finalization

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestPostMeetingProcessor_HasSingleOwnerAndSerializesGapsBeforeSync 验证单 owner、串行 gap 和最终同步顺序。
func TestPostMeetingProcessor_HasSingleOwnerAndSerializesGapsBeforeSync(t *testing.T) {
	runner := &blockingGapProcessor{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	syncer := &recordingFinalSyncer{done: make(chan struct{})}
	processor := NewPostMeetingProcessor(ProcessorDependencies{Gaps: runner, Syncer: syncer})
	if err := processor.Start(context.Background()); err != nil {
		t.Fatalf("启动 processor 失败：%v", err)
	}
	if !processor.Trigger("meeting") {
		t.Fatal("首次 trigger 必须取得 owner")
	}
	<-runner.firstStarted
	if processor.Trigger("meeting") {
		t.Fatal("同场第二次 trigger 不得创建 owner")
	}
	close(runner.releaseFirst)
	select {
	case <-syncer.done:
	case <-time.After(time.Second):
		t.Fatal("gap 结束后未执行 final sync")
	}
	if runner.maxConcurrent != 1 || runner.calls != 2 || syncer.calls != 1 {
		t.Fatalf("处理顺序错误：runner=%+v syncer=%+v", runner, syncer)
	}
	if err := processor.Stop(context.Background()); err != nil {
		t.Fatalf("停止 processor 失败：%v", err)
	}
}

// TestPostMeetingProcessor_RecoverDoesNotRunNetwork 验证重启恢复只调用本地状态收敛。
func TestPostMeetingProcessor_RecoverDoesNotRunNetwork(t *testing.T) {
	recovery := &recordingRecoveryStore{}
	runner := &blockingGapProcessor{}
	syncer := &recordingFinalSyncer{}
	processor := NewPostMeetingProcessor(ProcessorDependencies{Gaps: runner, Syncer: syncer, Recovery: recovery})
	if err := processor.Recover(context.Background()); err != nil {
		t.Fatalf("恢复 processor 状态失败：%v", err)
	}
	if recovery.calls != 1 || runner.calls != 0 || syncer.calls != 0 {
		t.Fatalf("恢复阶段不得联网：recovery=%+v runner=%+v syncer=%+v", recovery, runner, syncer)
	}
}

type blockingGapProcessor struct {
	mu            sync.Mutex
	calls         int
	concurrent    int
	maxConcurrent int
	firstStarted  chan struct{}
	releaseFirst  chan struct{}
}

// ProcessNext 首次阻塞以观察 owner，第二次报告队列已空。
func (processor *blockingGapProcessor) ProcessNext(ctx context.Context, _ string) (bool, error) {
	processor.mu.Lock()
	processor.calls++
	call := processor.calls
	processor.concurrent++
	if processor.concurrent > processor.maxConcurrent {
		processor.maxConcurrent = processor.concurrent
	}
	processor.mu.Unlock()
	defer func() {
		processor.mu.Lock()
		processor.concurrent--
		processor.mu.Unlock()
	}()
	if call == 1 && processor.firstStarted != nil {
		close(processor.firstStarted)
		select {
		case <-processor.releaseFirst:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		return true, nil
	}
	return false, nil
}

type recordingFinalSyncer struct {
	calls int
	done  chan struct{}
}

// SyncFinal 记录 gap 队列完成后的唯一同步。
func (syncer *recordingFinalSyncer) SyncFinal(context.Context, string) error {
	syncer.calls++
	if syncer.done != nil {
		close(syncer.done)
	}
	return nil
}

type recordingRecoveryStore struct{ calls int }

// RecoverInterrupted 记录纯本地恢复调用。
func (store *recordingRecoveryStore) RecoverInterrupted(context.Context) error {
	store.calls++
	return nil
}
