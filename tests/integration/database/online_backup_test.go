package database_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"

	_ "github.com/mattn/go-sqlite3"
)

// TestOnlineBackup_CopiesCommittedWALSnapshot 验证 SQLite Online Backup 可从已启用 WAL 的源库生成可打开一致快照。
func TestOnlineBackup_CopiesCommittedWALSnapshot(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source, err := database.Open(sourcePath)
	if err != nil {
		t.Fatalf("打开 WAL 源数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(source) })
	if err := source.Exec("CREATE TABLE entries (id INTEGER PRIMARY KEY, value TEXT NOT NULL); INSERT INTO entries(id, value) VALUES (1, 'committed')").Error; err != nil {
		t.Fatalf("写入 WAL 源数据库失败：%v", err)
	}
	destinationPath := filepath.Join(t.TempDir(), "backup.db")
	if err := database.OnlineBackup(sourcePath, destinationPath); err != nil {
		t.Fatalf("执行 SQLite Online Backup 失败：%v", err)
	}
	destination, err := sql.Open("sqlite3", destinationPath)
	if err != nil {
		t.Fatalf("打开备份数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = destination.Close() })
	var value string
	if err := destination.QueryRow("SELECT value FROM entries WHERE id = 1").Scan(&value); err != nil || value != "committed" {
		t.Fatalf("备份快照不一致：value=%q err=%v", value, err)
	}
}
