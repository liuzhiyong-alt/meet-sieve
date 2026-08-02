package database_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"

	_ "github.com/mattn/go-sqlite3"
)

// TestCheckSQLite_ReportsHealthyDatabase 验证 quick、integrity 和 foreign key 检查通过时返回健康结果。
func TestCheckSQLite_ReportsHealthyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	if result, err := database.CheckSQLite(db); err != nil || result.ForeignKeyViolationCount != 0 {
		t.Fatalf("健康数据库检查失败：result=%+v err=%v", result, err)
	}
}

// TestCheckSQLite_RejectsForeignKeyViolation 验证已存在的外键损坏不会被健康检查掩盖。
func TestCheckSQLite_RejectsForeignKeyViolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign-key.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	prepareForeignKeyViolation(t, path)
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	result, err := database.CheckSQLite(db)
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeDatabaseIntegrityFailed.ErrorCode {
		t.Fatalf("外键损坏必须映射为数据库完整性错误：result=%+v err=%v", result, err)
	}
	if result.ForeignKeyViolationCount != 1 {
		t.Fatalf("外键损坏数量不正确：%+v", result)
	}
}

// prepareForeignKeyViolation 通过外键关闭的独立连接准备一个真实的损坏样本。
func prepareForeignKeyViolation(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开损坏样本连接失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	statement := `PRAGMA foreign_keys = OFF;
		INSERT INTO meetings (
			id, meeting_no, subject, relative_dir, local_timezone,
			lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at
		) VALUES (
			'11111111-1111-4111-8111-111111111111', 'MS-1', '损坏会议', 'meetings/bad', 'Asia/Shanghai',
			'preparing', 'pending', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0
		);
		INSERT INTO meeting_participants (
			id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at
		) VALUES (
			'22222222-2222-4222-8222-222222222222', '11111111-1111-4111-8111-111111111111',
			'33333333-3333-4333-8333-333333333333', 'member', '不存在成员', 0, 0, 0
		);`
	if _, err := db.Exec(statement); err != nil {
		t.Fatalf("准备外键损坏样本失败：%v", err)
	}
}
