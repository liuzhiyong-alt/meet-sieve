package speaker

import (
	"context"
	"fmt"
	"sync"
)

// TrackProcessor 定义 RunnerPool 执行单个持久 track 的最小边界。
type TrackProcessor interface {
	Process(ctx context.Context, trackID string, finalizing bool) error
}

// RecoverableTrackSource 定义应用启动和轮询恢复 SQLite 任务的读取边界。
type RecoverableTrackSource interface {
	ListRecoverableTrackIDs(ctx context.Context, limit int) ([]string, error)
}

// RunnerPoolConfig 固定 worker、内存队列和单次恢复批量上限。
type RunnerPoolConfig struct {
	WorkerCount   int
	QueueCapacity int
	RecoveryBatch int
}

// RunnerPoolDependencies 描述后台池的处理器、恢复源和错误通知。
type RunnerPoolDependencies struct {
	Processor TrackProcessor
	Recovery  RecoverableTrackSource
	Config    RunnerPoolConfig
	OnError   func(error)
}

// RunnerPool 以有界队列运行固定 worker；SQLite 始终是可恢复事实来源。
type RunnerPool struct {
	processor TrackProcessor
	recovery  RecoverableTrackSource
	config    RunnerPoolConfig
	onError   func(error)
}

// NewRunnerPool 创建后台池；Run 会校验固定容量均为正数。
func NewRunnerPool(dependencies RunnerPoolDependencies) *RunnerPool {
	return &RunnerPool{
		processor: dependencies.Processor, recovery: dependencies.Recovery,
		config: dependencies.Config, onError: dependencies.OnError,
	}
}

// Run 启动固定 worker，立即恢复一次，并由可控 poll 信号继续补拉；取消后等待全部退出。
func (pool *RunnerPool) Run(ctx context.Context, wake <-chan string, poll <-chan struct{}) error {
	if err := validateRunnerPool(pool); err != nil {
		return err
	}
	jobs := make(chan string, pool.config.QueueCapacity)
	var workers sync.WaitGroup
	for index := 0; index < pool.config.WorkerCount; index++ {
		workers.Add(1)
		go pool.runWorker(ctx, jobs, &workers)
	}
	pool.enqueueRecovery(ctx, jobs)
	for {
		select {
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return nil
		case trackID, open := <-wake:
			if !open {
				wake = nil
				continue
			}
			enqueueTrack(jobs, trackID)
		case <-poll:
			pool.enqueueRecovery(ctx, jobs)
		}
	}
}

// runWorker 持续消费任务；单个任务失败或 panic 不终止其他 track。
func (pool *RunnerPool) runWorker(ctx context.Context, jobs <-chan string, workers *sync.WaitGroup) {
	defer workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case trackID, open := <-jobs:
			if !open {
				return
			}
			pool.processSafely(ctx, trackID)
		}
	}
}

// processSafely 隔离模型或依赖 panic，并只上报不含转写、姓名、向量和音频的 track 错误。
func (pool *RunnerPool) processSafely(ctx context.Context, trackID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			pool.reportError(fmt.Errorf("speaker worker panic：track=%s", trackID))
		}
	}()
	if err := pool.processor.Process(ctx, trackID, false); err != nil {
		pool.reportError(fmt.Errorf("speaker worker 处理失败：track=%s: %w", trackID, err))
	}
}

// enqueueRecovery 按最早 final seq 查询恢复任务；队列满可等待下一次轮询，不丢 SQLite 事实。
func (pool *RunnerPool) enqueueRecovery(ctx context.Context, jobs chan<- string) {
	trackIDs, err := pool.recovery.ListRecoverableTrackIDs(ctx, pool.config.RecoveryBatch)
	if err != nil {
		pool.reportError(err)
		return
	}
	for _, trackID := range trackIDs {
		enqueueTrack(jobs, trackID)
	}
}

// reportError 将后台错误交给宿主记录，不在池内维护不可恢复内存状态。
func (pool *RunnerPool) reportError(err error) {
	if pool.onError != nil {
		pool.onError(err)
	}
}

// enqueueTrack 非阻塞加入有界队列；满队列依靠 SQLite 恢复轮询补拉。
func enqueueTrack(jobs chan<- string, trackID string) {
	if trackID == "" {
		return
	}
	select {
	case jobs <- trackID:
	default:
	}
}

// validateRunnerPool 拒绝无界、零 worker 或缺少 SQLite 恢复源的配置。
func validateRunnerPool(pool *RunnerPool) error {
	if pool == nil || pool.processor == nil || pool.recovery == nil {
		return fmt.Errorf("speaker RunnerPool 依赖不完整")
	}
	if pool.config.WorkerCount <= 0 || pool.config.QueueCapacity <= 0 || pool.config.RecoveryBatch <= 0 {
		return fmt.Errorf("speaker RunnerPool 配置无效")
	}
	return nil
}
