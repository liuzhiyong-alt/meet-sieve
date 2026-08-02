package database_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/database"

	"gorm.io/gorm"
)

// TestTransactionManager_CommitsSuccessfulCallback 验证事务回调成功时提交写入。
func TestTransactionManager_CommitsSuccessfulCallback(t *testing.T) {
	db := openTransactionDatabase(t)
	manager := database.NewTransactionManager(db)

	err := manager.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return insertTestMetadata(tx).Error
	})
	if err != nil {
		t.Fatalf("提交事务失败：%v", err)
	}
	if countMetadata(t, db) != 1 {
		t.Fatal("成功事务没有提交")
	}
}

// TestTransactionManager_RollsBackCallbackError 验证事务回调失败时回滚且保留原始 cause。
func TestTransactionManager_RollsBackCallbackError(t *testing.T) {
	db := openTransactionDatabase(t)
	manager := database.NewTransactionManager(db)
	expected := errors.New("callback failed")

	err := manager.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		if insertErr := insertTestMetadata(tx).Error; insertErr != nil {
			return insertErr
		}
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("事务错误没有保留 cause：got %v", err)
	}
	if countMetadata(t, db) != 0 {
		t.Fatal("失败事务没有回滚")
	}
}

// TestTransactionManager_UsesWriteDispatcher 验证业务事务可绑定到 Step 1 单 writer，而非绕过队列直接写库。
func TestTransactionManager_UsesWriteDispatcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dispatched-transaction.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("准备 migration 失败：%v", err)
	}
	runtime, err := database.OpenRuntime(path, config.DatabaseConfig{
		BusyTimeoutMS:      1000,
		ReadMaxOpenConns:   1,
		ReadMaxIdleConns:   1,
		WriteQueueCapacity: config.Step1WriteQueueCapacity,
	})
	if err != nil {
		t.Fatalf("打开 SQLite runtime 失败：%v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	dispatcher, err := database.NewWriteDispatcher(runtime.Writer(), config.Step1WriteQueueCapacity)
	if err != nil {
		t.Fatalf("创建 WriteDispatcher 失败：%v", err)
	}
	t.Cleanup(func() { _ = dispatcher.Close() })

	manager := database.NewDispatchedTransactionManager(dispatcher)
	if err := manager.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return insertTestMetadata(tx).Error
	}); err != nil {
		t.Fatalf("单 writer 事务提交失败：%v", err)
	}
	if countMetadata(t, runtime.Reader()) != 1 {
		t.Fatal("单 writer 事务没有提交")
	}
}

// insertTestMetadata 构造满足 typed app_metadata 最小约束的测试记录。
func insertTestMetadata(tx *gorm.DB) *gorm.DB {
	return tx.Exec(`INSERT INTO app_metadata (
        id, singleton_key, product, database_id, device_code, created_with_app_version, created_at, updated_at
    ) VALUES (
        '11111111-1111-4111-8111-111111111111', 1, 'meet-sieve',
        '22222222-2222-4222-8222-222222222222', 'ABCD', 'test', 0, 0
    )`)
}

func openTransactionDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "transaction.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("准备 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开事务数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}

func countMetadata(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var count int64
	if err := db.Table("app_metadata").Count(&count).Error; err != nil {
		t.Fatalf("统计元信息失败：%v", err)
	}
	return count
}
