package database

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"

	"gorm.io/gorm"
)

// ErrWriteDispatcherClosed 表示应用正在停止，不能再接受新的业务写入。
var ErrWriteDispatcherClosed = errors.New("write dispatcher closed")

// WriteTask 是必须在短 SQLite 事务内串行执行的业务写入。
type WriteTask func(tx *gorm.DB) error

// WriteDispatcher 使用固定容量队列把所有运行期业务写入串行化。
type WriteDispatcher struct {
	writer *gorm.DB
	tasks  chan queuedWriteTask
	done   chan struct{}

	mu      sync.Mutex
	closing bool
}

// queuedWriteTask 保存一次写入任务及其调用方等待结果。
type queuedWriteTask struct {
	context context.Context
	task    WriteTask
	result  chan error
}

// NewWriteDispatcher 创建并启动单 writer；队列容量必须与 Step 1 技术配置一致。
func NewWriteDispatcher(writer *gorm.DB, capacity int) (*WriteDispatcher, error) {
	if writer == nil {
		return nil, fmt.Errorf("创建单 writer：数据库连接不能为空")
	}
	if capacity != config.Step1WriteQueueCapacity {
		return nil, fmt.Errorf("创建单 writer：队列容量必须为 %d", config.Step1WriteQueueCapacity)
	}
	dispatcher := &WriteDispatcher{
		writer: writer,
		tasks:  make(chan queuedWriteTask, capacity),
		done:   make(chan struct{}),
	}
	go dispatcher.run()
	return dispatcher, nil
}

// Submit 接受写任务并等待其事务结果；队列满时立即返回可重试的 DATABASE_BUSY。
func (dispatcher *WriteDispatcher) Submit(ctx context.Context, task WriteTask) error {
	result, err := dispatcher.enqueue(ctx, task)
	if err != nil {
		return err
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		// 任务已接受；worker 会在开始事务前再次检查取消状态并把结果写入缓冲通道。
		return ctx.Err()
	}
}

// enqueue 仅负责安全入队，供 Submit 与同包并发测试复用。
func (dispatcher *WriteDispatcher) enqueue(ctx context.Context, task WriteTask) (<-chan error, error) {
	if dispatcher == nil || ctx == nil || task == nil {
		return nil, fmt.Errorf("提交写任务参数不合法")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	queued := queuedWriteTask{context: ctx, task: task, result: make(chan error, 1)}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if dispatcher.closing {
		return nil, ErrWriteDispatcherClosed
	}
	select {
	case dispatcher.tasks <- queued:
		return queued.result, nil
	default:
		return nil, apperr.Biz(apperr.CodeDatabaseBusy, apperr.WithOp("database.writer.enqueue"))
	}
}

// Close 拒绝新任务并排空已经接受的短事务；重复调用保持幂等。
func (dispatcher *WriteDispatcher) Close() error {
	if dispatcher == nil {
		return nil
	}
	dispatcher.mu.Lock()
	if !dispatcher.closing {
		dispatcher.closing = true
		close(dispatcher.tasks)
	}
	dispatcher.mu.Unlock()
	<-dispatcher.done
	return nil
}

// run 保持唯一消费者，确保相邻任务不会同时开始 SQLite 写事务。
func (dispatcher *WriteDispatcher) run() {
	defer close(dispatcher.done)
	for queued := range dispatcher.tasks {
		queued.result <- dispatcher.execute(queued)
	}
}

// execute 在事务开始前检查取消状态，并将回调 panic 转换为安全错误。
func (dispatcher *WriteDispatcher) execute(queued queuedWriteTask) (err error) {
	if err := queued.context.Err(); err != nil {
		return err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = apperr.RecoveredPanic(recovered, "database.writer.execute")
		}
	}()
	err = dispatcher.writer.WithContext(queued.context).Transaction(func(tx *gorm.DB) error {
		if contextErr := queued.context.Err(); contextErr != nil {
			return contextErr
		}
		if callbackErr := queued.task(tx); callbackErr != nil {
			return fmt.Errorf("写任务回调失败：%w", callbackErr)
		}
		return nil
	})
	if err != nil {
		return normalizeWriteError(fmt.Errorf("执行写任务事务失败：%w", err))
	}
	return nil
}
