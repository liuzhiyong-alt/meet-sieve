package database_test

import (
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/database"

	"gorm.io/gorm"
)

// TestOpenRuntime_SeparatesWriterAndReaderPools 验证 writer 固定单连接，reader 使用已校验的小型读池。
func TestOpenRuntime_SeparatesWriterAndReaderPools(t *testing.T) {
	runtime, err := database.OpenRuntime(filepath.Join(t.TempDir(), "runtime.db"), config.DatabaseConfig{
		BusyTimeoutMS:      2345,
		ReadMaxOpenConns:   3,
		ReadMaxIdleConns:   2,
		WriteQueueCapacity: config.Step1WriteQueueCapacity,
	})
	if err != nil {
		t.Fatalf("打开 SQLite runtime 失败：%v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	assertPragmaInt(t, runtime.Writer(), "foreign_keys", 1)
	assertPragmaInt(t, runtime.Reader(), "foreign_keys", 1)
	assertPragmaString(t, runtime.Writer(), "journal_mode", "wal")
	assertPragmaInt(t, runtime.Writer(), "synchronous", 1)
	assertPragmaInt(t, runtime.Writer(), "busy_timeout", 2345)
	assertPragmaInt(t, runtime.Reader(), "busy_timeout", 2345)
	assertMaxOpenConnections(t, runtime.Writer(), 1)
	assertMaxOpenConnections(t, runtime.Reader(), 3)
}

// assertMaxOpenConnections 验证 GORM 底层连接池没有偏离传入的技术配置。
func assertMaxOpenConnections(t *testing.T, db *gorm.DB, expected int) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取连接池失败：%v", err)
	}
	if actual := sqlDB.Stats().MaxOpenConnections; actual != expected {
		t.Fatalf("最大连接数不正确：got=%d want=%d", actual, expected)
	}
}

// TestOpenRuntime_RejectsInvalidDatabaseConfig 验证运行时不为非法连接配置静默回退默认值。
func TestOpenRuntime_RejectsInvalidDatabaseConfig(t *testing.T) {
	_, err := database.OpenRuntime(filepath.Join(t.TempDir(), "invalid.db"), config.DatabaseConfig{
		BusyTimeoutMS:      0,
		ReadMaxOpenConns:   9,
		ReadMaxIdleConns:   9,
		WriteQueueCapacity: 1,
	})
	if err == nil {
		t.Fatal("非法数据库配置必须拒绝")
	}
}
