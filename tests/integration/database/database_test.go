package database_test

import (
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"

	"gorm.io/gorm"
)

// TestOpen_AppliesSQLitePragmas 验证桌面单进程 SQLite 连接使用固定安全参数。
func TestOpen_AppliesSQLitePragmas(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("读取 foreign_keys 失败：%v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys 未启用：got %d", foreignKeys)
	}

	assertPragmaString(t, db, "journal_mode", "wal")
	assertPragmaInt(t, db, "synchronous", 1)
	assertPragmaInt(t, db, "busy_timeout", 5000)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取连接池失败：%v", err)
	}
	if sqlDB.Stats().MaxOpenConnections != 1 {
		t.Fatalf("最大连接数不正确：got %d", sqlDB.Stats().MaxOpenConnections)
	}
}

func assertPragmaString(t *testing.T, db *gorm.DB, name string, expected string) {
	t.Helper()

	var actual string
	if err := db.Raw("PRAGMA " + name).Scan(&actual).Error; err != nil {
		t.Fatalf("读取 PRAGMA %s 失败：%v", name, err)
	}
	if actual != expected {
		t.Fatalf("PRAGMA %s 不正确：got %s, want %s", name, actual, expected)
	}
}

func assertPragmaInt(t *testing.T, db *gorm.DB, name string, expected int) {
	t.Helper()

	var actual int
	if err := db.Raw("PRAGMA " + name).Scan(&actual).Error; err != nil {
		t.Fatalf("读取 PRAGMA %s 失败：%v", name, err)
	}
	if actual != expected {
		t.Fatalf("PRAGMA %s 不正确：got %d, want %d", name, actual, expected)
	}
}
