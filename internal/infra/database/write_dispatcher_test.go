package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"

	"gorm.io/gorm"
)

// TestWriteDispatcher_ExecutesAcceptedTasksInOrder 验证阻塞中的首任务之后，已接受任务按 FIFO 串行执行。
func TestWriteDispatcher_ExecutesAcceptedTasksInOrder(t *testing.T) {
	dispatcher := newTestWriteDispatcher(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var order []int
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- dispatcher.Submit(context.Background(), func(*gorm.DB) error {
			mu.Lock()
			order = append(order, 1)
			mu.Unlock()
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- dispatcher.Submit(context.Background(), func(*gorm.DB) error {
			mu.Lock()
			order = append(order, 2)
			mu.Unlock()
			return nil
		})
	}()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("首任务失败：%v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("次任务失败：%v", err)
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("写任务顺序不正确：%v", order)
	}
}

// TestWriteDispatcher_ReturnsRetryableBusyWhenQueueIsFull 验证恰好 256 个等待任务可接受，第 257 个立即返回 busy。
func TestWriteDispatcher_ReturnsRetryableBusyWhenQueueIsFull(t *testing.T) {
	dispatcher := newTestWriteDispatcher(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	firstResult, err := dispatcher.enqueue(context.Background(), func(*gorm.DB) error {
		close(entered)
		<-release
		return nil
	})
	if err != nil {
		t.Fatalf("提交首任务失败：%v", err)
	}
	<-entered
	for index := 0; index < config.Step1WriteQueueCapacity; index++ {
		if _, err := dispatcher.enqueue(context.Background(), func(*gorm.DB) error { return nil }); err != nil {
			t.Fatalf("第 %d 个等待任务不应被拒绝：%v", index+1, err)
		}
	}
	if _, err := dispatcher.enqueue(context.Background(), func(*gorm.DB) error { return nil }); err == nil {
		t.Fatal("第 257 个等待任务必须返回 busy")
	} else {
		assertDatabaseBusy(t, err)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("首任务执行失败：%v", err)
	}
}

// TestWriteDispatcher_RollsBackFailuresAndContinues 验证失败或 panic 会回滚，且不影响后续已接受任务。
func TestWriteDispatcher_RollsBackFailuresAndContinues(t *testing.T) {
	dispatcher := newTestWriteDispatcher(t)
	if err := dispatcher.writer.Exec("CREATE TABLE task_rows (id INTEGER PRIMARY KEY, value TEXT NOT NULL)").Error; err != nil {
		t.Fatalf("创建测试表失败：%v", err)
	}
	failingErr := errors.New("expected callback failure")
	if err := dispatcher.Submit(context.Background(), func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO task_rows(id, value) VALUES (1, 'rolled-back')").Error; err != nil {
			return err
		}
		return failingErr
	}); !errors.Is(err, failingErr) {
		t.Fatalf("回调错误必须保留 cause：%v", err)
	}
	if err := dispatcher.Submit(context.Background(), func(*gorm.DB) error { panic("expected panic") }); err == nil {
		t.Fatal("panic 必须转换为安全错误")
	}
	if err := dispatcher.Submit(context.Background(), func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO task_rows(id, value) VALUES (2, 'committed')").Error
	}); err != nil {
		t.Fatalf("后续任务必须继续执行：%v", err)
	}
	var count int
	if err := dispatcher.writer.Raw("SELECT count(*) FROM task_rows").Scan(&count).Error; err != nil {
		t.Fatalf("统计任务记录失败：%v", err)
	}
	if count != 1 {
		t.Fatalf("失败写任务未回滚或后续任务丢失：count=%d", count)
	}
}

// TestWriteDispatcher_SkipsTaskCanceledBeforeTransaction 验证任务在事务开始前取消时绝不执行回调。
func TestWriteDispatcher_SkipsTaskCanceledBeforeTransaction(t *testing.T) {
	dispatcher := newTestWriteDispatcher(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	firstResult, err := dispatcher.enqueue(context.Background(), func(*gorm.DB) error {
		close(entered)
		<-release
		return nil
	})
	if err != nil {
		t.Fatalf("提交首任务失败：%v", err)
	}
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	var executed atomic.Bool
	secondResult, err := dispatcher.enqueue(ctx, func(*gorm.DB) error {
		executed.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("入队可取消任务失败：%v", err)
	}
	cancel()
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("首任务失败：%v", err)
	}
	if err := <-secondResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("事务前取消必须原样返回：%v", err)
	}
	if executed.Load() {
		t.Fatal("已取消任务不得开始事务")
	}
}

// TestWriteDispatcher_CloseDrainsAcceptedTasksAndRejectsNewOnes 验证关闭先排空已接受短事务，再拒绝新任务。
func TestWriteDispatcher_CloseDrainsAcceptedTasksAndRejectsNewOnes(t *testing.T) {
	dispatcher := newTestWriteDispatcher(t)
	var executed atomic.Int32
	result, err := dispatcher.enqueue(context.Background(), func(*gorm.DB) error {
		executed.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("提交任务失败：%v", err)
	}
	if err := dispatcher.Close(); err != nil {
		t.Fatalf("关闭 dispatcher 失败：%v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("排空任务失败：%v", err)
	}
	if executed.Load() != 1 {
		t.Fatalf("已接受任务必须在关闭前执行：%d", executed.Load())
	}
	if err := dispatcher.Submit(context.Background(), func(*gorm.DB) error { return nil }); !errors.Is(err, ErrWriteDispatcherClosed) {
		t.Fatalf("关闭后必须拒绝新任务：%v", err)
	}
	if err := dispatcher.Close(); err != nil {
		t.Fatalf("重复关闭必须幂等：%v", err)
	}
}

// TestWriteDispatcher_MapsSQLiteBusy 验证真实 SQLite 写锁超时收敛为可重试的 DATABASE_BUSY。
func TestWriteDispatcher_MapsSQLiteBusy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	runtime, err := OpenRuntime(path, config.DatabaseConfig{
		BusyTimeoutMS:      20,
		ReadMaxOpenConns:   1,
		ReadMaxIdleConns:   1,
		WriteQueueCapacity: config.Step1WriteQueueCapacity,
	})
	if err != nil {
		t.Fatalf("打开 SQLite runtime 失败：%v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.Writer().Exec("CREATE TABLE busy_rows (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("创建测试表失败：%v", err)
	}
	dispatcher, err := NewWriteDispatcher(runtime.Writer(), config.Step1WriteQueueCapacity)
	if err != nil {
		t.Fatalf("创建 dispatcher 失败：%v", err)
	}
	t.Cleanup(func() { _ = dispatcher.Close() })
	locker, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开锁定连接失败：%v", err)
	}
	t.Cleanup(func() { _ = locker.Close() })
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("取得 SQLite 写锁失败：%v", err)
	}
	if err := dispatcher.Submit(context.Background(), func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO busy_rows(id) VALUES (1)").Error
	}); err == nil {
		t.Fatal("写锁超时必须返回 busy")
	} else {
		assertDatabaseBusy(t, err)
	}
	if _, err := locker.Exec("COMMIT"); err != nil {
		t.Fatalf("释放 SQLite 写锁失败：%v", err)
	}
}

// newTestWriteDispatcher 创建使用实际 SQLite writer 的隔离 dispatcher。
func newTestWriteDispatcher(t *testing.T) *WriteDispatcher {
	t.Helper()
	runtime, err := OpenRuntime(filepath.Join(t.TempDir(), "dispatcher.db"), config.DatabaseConfig{
		BusyTimeoutMS:      1000,
		ReadMaxOpenConns:   1,
		ReadMaxIdleConns:   1,
		WriteQueueCapacity: config.Step1WriteQueueCapacity,
	})
	if err != nil {
		t.Fatalf("打开测试 SQLite runtime 失败：%v", err)
	}
	dispatcher, err := NewWriteDispatcher(runtime.Writer(), config.Step1WriteQueueCapacity)
	if err != nil {
		t.Fatalf("创建 WriteDispatcher 失败：%v", err)
	}
	t.Cleanup(func() {
		_ = dispatcher.Close()
		_ = runtime.Close()
	})
	return dispatcher
}

// assertDatabaseBusy 验证队列满返回统一、可重试的数据库 busy 错误。
func assertDatabaseBusy(t *testing.T, err error) {
	t.Helper()
	appErr, ok := err.(*apperr.AppError)
	if !ok || appErr.ErrorCode != apperr.CodeDatabaseBusy.ErrorCode || !appErr.Retryable {
		t.Fatalf("队列满错误不正确：%T %v", err, err)
	}
}
