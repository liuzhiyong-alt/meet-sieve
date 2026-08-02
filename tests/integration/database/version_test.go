package database_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"

	_ "github.com/mattn/go-sqlite3"
)

// TestReadSchemaVersion_ReadsBeforeTypedMetadata 验证 schema 版本读取不依赖 typed metadata 表。
func TestReadSchemaVersion_ReadsBeforeTypedMetadata(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "version.db"))
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version uint64, dirty bool);
		INSERT INTO schema_migrations(version, dirty) VALUES (1, 0)`); err != nil {
		t.Fatalf("准备 schema_migrations 失败：%v", err)
	}

	version, err := database.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("读取 schema version 失败：%v", err)
	}
	if version.Version != 1 || version.Dirty {
		t.Fatalf("schema version 不正确：%+v", version)
	}
}

// TestReadSchemaVersion_ReportsDirtyState 验证 dirty 标记不被读取层掩盖。
func TestReadSchemaVersion_ReportsDirtyState(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "dirty-version.db"))
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version uint64, dirty bool);
		INSERT INTO schema_migrations(version, dirty) VALUES (2, 1)`); err != nil {
		t.Fatalf("准备 dirty schema_migrations 失败：%v", err)
	}

	version, err := database.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("读取 dirty version 失败：%v", err)
	}
	if !version.Dirty {
		t.Fatal("dirty 标记必须原样返回")
	}
}
