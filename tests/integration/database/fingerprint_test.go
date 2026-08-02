package database_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

	"meet-sieve/internal/infra/database"

	_ "github.com/mattn/go-sqlite3"
)

// TestValidateStep0Fingerprint_AcceptsExactFoundation 验证仅有 Step 0 占位表的 clean v1 数据库可以升级。
func TestValidateStep0Fingerprint_AcceptsExactFoundation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foundation-v1.db")
	files := fstest.MapFS{
		"sqlite/000001_foundation.up.sql":   {Data: []byte("CREATE TABLE app_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)")},
		"sqlite/000001_foundation.down.sql": {Data: []byte("DROP TABLE app_metadata")},
	}
	if err := database.MigrateFS(path, files, "sqlite"); err != nil {
		t.Fatalf("准备 Step 0 数据库失败：%v", err)
	}
	db := openSQLDatabase(t, path)
	version, err := database.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("读取 schema 版本失败：%v", err)
	}
	if err := database.ValidateStep0Fingerprint(db, version); err != nil {
		t.Fatalf("精确 Step 0 指纹必须可升级：%v", err)
	}
}

// TestValidateStep0Fingerprint_RejectsUnexpectedTable 验证 version 1 多出任意用户表时拒绝猜测升级。
func TestValidateStep0Fingerprint_RejectsUnexpectedTable(t *testing.T) {
	db := createStep0LikeDatabase(t, "CREATE TABLE unexpected_data (id TEXT PRIMARY KEY)")
	version, err := database.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("读取 schema 版本失败：%v", err)
	}
	if err := database.ValidateStep0Fingerprint(db, version); err == nil {
		t.Fatal("多出用户表必须拒绝升级")
	}
}

// TestValidateStep0Fingerprint_RejectsColumnDrift 验证少列、多列或类型变更均不作为 Step 0 猜测处理。
func TestValidateStep0Fingerprint_RejectsColumnDrift(t *testing.T) {
	db := createStep0LikeDatabase(t, "DROP TABLE app_metadata; CREATE TABLE app_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL, created_at TEXT NOT NULL, updated_at DATETIME NOT NULL)")
	version, err := database.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("读取 schema 版本失败：%v", err)
	}
	if err := database.ValidateStep0Fingerprint(db, version); err == nil {
		t.Fatal("列类型变更必须拒绝升级")
	}
}

// TestValidateStep0Fingerprint_RejectsDirtyOrWrongVersion 验证版本非 1 或 dirty 不能通过 Step 0 迁移资格检查。
func TestValidateStep0Fingerprint_RejectsDirtyOrWrongVersion(t *testing.T) {
	for _, version := range []database.SchemaVersion{{Version: 1, Dirty: true}, {Version: 2, Dirty: false}} {
		db := createStep0LikeDatabase(t, "")
		if err := database.ValidateStep0Fingerprint(db, version); err == nil {
			t.Fatalf("版本 %+v 必须拒绝", version)
		}
	}
}

// TestValidateStep0Fingerprint_RejectsMultipleMigrationRows 验证额外 migration 记录不能伪装成 clean v1。
func TestValidateStep0Fingerprint_RejectsMultipleMigrationRows(t *testing.T) {
	db := createStep0LikeDatabase(t, "INSERT INTO schema_migrations(version, dirty) VALUES (0, 0)")
	version, err := database.ReadSchemaVersion(db)
	if err != nil {
		t.Fatalf("读取 schema 版本失败：%v", err)
	}
	if err := database.ValidateStep0Fingerprint(db, version); err == nil {
		t.Fatal("多个 migration 记录必须拒绝升级")
	}
}

// openSQLDatabase 打开测试专用 SQLite 连接。
func openSQLDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// createStep0LikeDatabase 创建可注入结构偏差的 Step 0 形态数据库。
func createStep0LikeDatabase(t *testing.T, extraSQL string) *sql.DB {
	t.Helper()
	db := openSQLDatabase(t, filepath.Join(t.TempDir(), "fingerprint.db"))
	statement := `CREATE TABLE schema_migrations (version INTEGER NOT NULL, dirty BOOLEAN NOT NULL);
		INSERT INTO schema_migrations(version, dirty) VALUES (1, 0);
		CREATE TABLE app_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);`
	if extraSQL != "" {
		statement += extraSQL
	}
	if _, err := db.Exec(statement); err != nil {
		t.Fatalf("准备 Step 0 形态失败：%v", err)
	}
	return db
}
