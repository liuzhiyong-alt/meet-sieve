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

// ProcessingGate 判断当前模型、校准档案和声纹向量是否允许自动处理。
type ProcessingGate interface {
	Ready(ctx context.Context) bool
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
	Gate      ProcessingGate
	Config    RunnerPoolConfig
	OnError   func(error)
}

// RunnerPool 以有界队列运行固定 worker；SQLite 始终是可恢复事实来源。
type RunnerPool struct {
	processor TrackProcessor
	recovery  RecoverableTrackSource
	gate      ProcessingGate
	config    RunnerPoolConfig
	onError   func(error)
}

// trackReservations 防止同一持久 track 同时排队或被多个 worker 重复处理。
type trackReservations struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

// NewRunnerPool 创建后台池；Run 会校验固定容量均为正数。
func NewRunnerPool(dependencies RunnerPoolDependencies) *RunnerPool {
	return &RunnerPool{
		processor: dependencies.Processor, recovery: dependencies.Recovery,
		gate: dependencies.Gate, config: dependencies.Config, onError: dependencies.OnError,
	}
}

// Run 启动固定 worker，立即恢复一次，并由可控 poll 信号继续补拉；取消后等待全部退出。
func (pool *RunnerPool) Run(ctx context.Context, wake <-chan string, poll <-chan struct{}) error {
	if err := validateRunnerPool(pool); err != nil {
		return err
	}
	jobs := make(chan string, pool.config.QueueCapacity)
	reservations := &trackReservations{ids: make(map[string]struct{})}
	var workers sync.WaitGroup
	for index := 0; index < pool.config.WorkerCount; index++ {
		workers.Add(1)
		go pool.runWorker(ctx, jobs, reservations, &workers)
	}
	pool.enqueueRecoveryWhenReady(ctx, jobs, reservations)
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
			if pool.processingReady(ctx) {
				enqueueTrack(jobs, reservations, trackID)
			}
		case <-poll:
			pool.enqueueRecoveryWhenReady(ctx, jobs, reservations)
		}
	}
}

// enqueueRecoveryWhenReady 仅在自动识别依赖完整时读取持久任务；未就绪任务继续留在 SQLite。
func (pool *RunnerPool) enqueueRecoveryWhenReady(ctx context.Context, jobs chan<- string, reservations *trackReservations) {
	if !pool.processingReady(ctx) {
		return
	}
	pool.enqueueRecovery(ctx, jobs, reservations)
}

// processingReady 兼容未配置门禁的既有调用方；正式装配必须提供动态门禁。
func (pool *RunnerPool) processingReady(ctx context.Context) bool {
	return pool.gate == nil || pool.gate.Ready(ctx)
}

// runWorker 持续消费任务；单个任务失败或 panic 不终止其他 track。
func (pool *RunnerPool) runWorker(ctx context.Context, jobs <-chan string, reservations *trackReservations, workers *sync.WaitGroup) {
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
			reservations.release(trackID)
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
func (pool *RunnerPool) enqueueRecovery(ctx context.Context, jobs chan<- string, reservations *trackReservations) {
	trackIDs, err := pool.recovery.ListRecoverableTrackIDs(ctx, pool.config.RecoveryBatch)
	if err != nil {
		pool.reportError(err)
		return
	}
	for _, trackID := range trackIDs {
		enqueueTrack(jobs, reservations, trackID)
	}
}

// reportError 将后台错误交给宿主记录，不在池内维护不可恢复内存状态。
func (pool *RunnerPool) reportError(err error) {
	if pool.onError != nil {
		pool.onError(err)
	}
}

// enqueueTrack 非阻塞加入有界队列；满队列依靠 SQLite 恢复轮询补拉。
func enqueueTrack(jobs chan<- string, reservations *trackReservations, trackID string) {
	if trackID == "" || reservations == nil || !reservations.reserve(trackID) {
		return
	}
	select {
	case jobs <- trackID:
	default:
		reservations.release(trackID)
	}
}

// reserve 原子登记排队或处理中的 track；已登记返回 false。
func (reservations *trackReservations) reserve(trackID string) bool {
	reservations.mu.Lock()
	defer reservations.mu.Unlock()
	if _, exists := reservations.ids[trackID]; exists {
		return false
	}
	reservations.ids[trackID] = struct{}{}
	return true
}

// release 在单次处理结束后允许 SQLite 恢复轮询重新提交失败任务。
func (reservations *trackReservations) release(trackID string) {
	reservations.mu.Lock()
	delete(reservations.ids, trackID)
	reservations.mu.Unlock()
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
