package workspace_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"meet-sieve/internal/domain/metadata"
	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
	workspace "meet-sieve/internal/service/workspace"

	_ "github.com/mattn/go-sqlite3"
)

const gibibyte = uint64(1024 * 1024 * 1024)

// TestInspect_MissingWritablePathDoesNotCreate 验证 inspect 只分类不存在路径，绝不在检查阶段创建目录。
func TestInspect_MissingWritablePathDoesNotCreate(t *testing.T) {
	base := t.TempDir()
	inspector := newInspector(t, base, 11*gibibyte)
	candidatePath := filepath.Join(base, "new-workspace")

	candidate := inspector.Inspect(candidatePath)
	if candidate.Kind != domainworkspace.CandidateKindMissing || candidate.Reason != domainworkspace.CandidateReasonNone || candidate.SchemaState != domainworkspace.SchemaStateNone {
		t.Fatalf("不存在路径分类不正确：%+v", candidate)
	}
	if _, err := os.Stat(candidatePath); !os.IsNotExist(err) {
		t.Fatalf("inspect 不得创建不存在目录：err=%v", err)
	}
}

// TestInspect_OnlyTrulyEmptyDirectoryCanInitialize 验证 .DS_Store、其他位置数据库等任意目录项都不是空目录。
func TestInspect_OnlyTrulyEmptyDirectoryCanInitialize(t *testing.T) {
	base := t.TempDir()
	inspector := newInspector(t, base, 11*gibibyte)
	emptyPath := filepath.Join(base, "empty")
	if err := os.Mkdir(emptyPath, 0o700); err != nil {
		t.Fatalf("创建空目录失败：%v", err)
	}
	if candidate := inspector.Inspect(emptyPath); candidate.Kind != domainworkspace.CandidateKindEmpty {
		t.Fatalf("真正空目录必须可初始化：%+v", candidate)
	}
	nonEmptyPath := filepath.Join(base, "non-empty")
	if err := os.Mkdir(nonEmptyPath, 0o700); err != nil {
		t.Fatalf("创建非空目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(nonEmptyPath, ".DS_Store"), []byte("metadata"), 0o600); err != nil {
		t.Fatalf("准备隐藏文件失败：%v", err)
	}
	if candidate := inspector.Inspect(nonEmptyPath); candidate.Kind != domainworkspace.CandidateKindInvalid || candidate.Reason != domainworkspace.CandidateReasonDatabaseMissing {
		t.Fatalf("含 .DS_Store 的目录必须按固定数据库缺失拒绝：%+v", candidate)
	}
}

// TestInspect_AddsLowDiskWarningWithoutBlocking 验证低于 10 GB 仅返回提示，不改变初始化分类。
func TestInspect_AddsLowDiskWarningWithoutBlocking(t *testing.T) {
	base := t.TempDir()
	inspector := newInspector(t, base, gibibyte)
	candidate := inspector.Inspect(filepath.Join(base, "new-workspace"))
	if candidate.Kind != domainworkspace.CandidateKindMissing || len(candidate.Warnings) != 1 || candidate.Warnings[0] != domainworkspace.CandidateWarningLowDiskSpace {
		t.Fatalf("低磁盘空间提示不正确：%+v", candidate)
	}
}

// TestInspect_AcceptsFixedDatabaseAndLeavesDatabaseFilesUntouched 验证只接受固定位置完整数据库，且只读检查不创建 WAL/SHM。
func TestInspect_AcceptsFixedDatabaseAndLeavesDatabaseFilesUntouched(t *testing.T) {
	base := t.TempDir()
	workspacePath := filepath.Join(base, "workspace")
	databasePath := createFinalizedWorkspaceDatabase(t, workspacePath)
	if err := os.WriteFile(filepath.Join(workspacePath, "unknown.txt"), []byte("preserve"), 0o600); err != nil {
		t.Fatalf("准备未知文件失败：%v", err)
	}
	inspector := newInspector(t, base, 11*gibibyte)

	candidate := inspector.Inspect(workspacePath)
	if candidate.Kind != domainworkspace.CandidateKindMeetSieve || candidate.SchemaState != domainworkspace.SchemaStateCurrent || candidate.DatabaseID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("完整固定数据库必须被接受：%+v", candidate)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(databasePath + suffix); !os.IsNotExist(err) {
			t.Fatalf("只读 inspect 不得创建 %s：err=%v", suffix, err)
		}
	}
	if content, err := os.ReadFile(filepath.Join(workspacePath, "unknown.txt")); err != nil || string(content) != "preserve" {
		t.Fatalf("inspect 不得修改未知文件：content=%q err=%v", content, err)
	}
}

// TestInspect_ClassifiesSchemaAndHalfInitializationStates 验证 v1、过新、dirty 与半初始化数据库均走稳定分支。
func TestInspect_ClassifiesSchemaAndHalfInitializationStates(t *testing.T) {
	base := t.TempDir()
	inspector := newInspector(t, base, 11*gibibyte)

	step0Path := filepath.Join(base, "step0", "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(step0Path), 0o700); err != nil {
		t.Fatalf("创建 Step 0 目录失败：%v", err)
	}
	if err := database.MigrateFS(step0Path, step0MigrationFiles(), "sqlite"); err != nil {
		t.Fatalf("创建 Step 0 数据库失败：%v", err)
	}
	if candidate := inspector.Inspect(filepath.Join(base, "step0")); candidate.Kind != domainworkspace.CandidateKindMeetSieve || candidate.SchemaState != domainworkspace.SchemaStateUpgradeRequired {
		t.Fatalf("Step 0 数据库分类不正确：%+v", candidate)
	}

	halfPath := filepath.Join(base, "half", "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(halfPath), 0o700); err != nil {
		t.Fatalf("创建半初始化目录失败：%v", err)
	}
	if err := database.Migrate(halfPath); err != nil {
		t.Fatalf("创建半初始化数据库失败：%v", err)
	}
	if candidate := inspector.Inspect(filepath.Join(base, "half")); candidate.Kind != domainworkspace.CandidateKindInvalid || candidate.Reason != domainworkspace.CandidateReasonDatabaseInvalid {
		t.Fatalf("半初始化数据库必须拒绝：%+v", candidate)
	}

	newerPath := filepath.Join(base, "newer", "data", "meetings.db")
	createVersionOnlyDatabase(t, newerPath, int(database.CurrentSchemaVersion)+1, false)
	if candidate := inspector.Inspect(filepath.Join(base, "newer")); candidate.Kind != domainworkspace.CandidateKindInvalid || candidate.Reason != domainworkspace.CandidateReasonSchemaNewer || candidate.SchemaState != domainworkspace.SchemaStateNewer {
		t.Fatalf("过新 schema 分类不正确：%+v", candidate)
	}

	dirtyPath := filepath.Join(base, "dirty", "data", "meetings.db")
	createVersionOnlyDatabase(t, dirtyPath, 2, true)
	if candidate := inspector.Inspect(filepath.Join(base, "dirty")); candidate.Kind != domainworkspace.CandidateKindInvalid || candidate.Reason != domainworkspace.CandidateReasonDatabaseInvalid {
		t.Fatalf("dirty schema 必须拒绝：%+v", candidate)
	}
}

// newInspector 创建使用真实临时目录、固定本地卷和可控磁盘空间的检查器。
func newInspector(t *testing.T, base string, freeBytes uint64) *workspace.Inspector {
	t.Helper()
	installRoot, err := filesystem.CanonicalizePath(filepath.Join(base, "install-root"))
	if err != nil {
		t.Fatalf("规范化安装目录失败：%v", err)
	}
	policy := workspace.NewPathPolicy(installRoot, func(filesystem.CanonicalPath) (filesystem.VolumeKind, error) {
		return filesystem.VolumeLocal, nil
	})
	return workspace.NewInspector(policy, func(string) (uint64, error) { return freeBytes, nil })
}

// createFinalizedWorkspaceDatabase 创建一个已完成 Step 1 动态 singleton 的固定位置数据库。
func createFinalizedWorkspaceDatabase(t *testing.T, workspacePath string) string {
	t.Helper()
	databasePath := filepath.Join(workspacePath, "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatalf("创建 data 目录失败：%v", err)
	}
	if err := database.Migrate(databasePath); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatalf("打开 finalizer 数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	finalizer := migration.NewFoundationFinalizer(
		identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		),
		metadata.NewDeviceCodeGenerator(metadata.FixedRandomSource{0, 1, 24, 30}),
		clock.NewFixed(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)),
		"test",
	)
	if _, err := finalizer.Finalize(db, migration.FinalizationTargetNewDatabase); err != nil {
		t.Fatalf("执行 finalizer 失败：%v", err)
	}
	return databasePath
}

// step0MigrationFiles 返回只含 Step 0 占位结构的独立 migration fixture。
func step0MigrationFiles() fstest.MapFS {
	return fstest.MapFS{
		"sqlite/000001_foundation.up.sql":   {Data: []byte("CREATE TABLE app_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)")},
		"sqlite/000001_foundation.down.sql": {Data: []byte("DROP TABLE app_metadata")},
	}
}

// createVersionOnlyDatabase 创建供版本分支检查使用的最小 SQLite 文件。
func createVersionOnlyDatabase(t *testing.T, path string, version int, dirty bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("创建版本目录失败：%v", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开版本数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE schema_migrations (version INTEGER, dirty BOOLEAN); INSERT INTO schema_migrations(version, dirty) VALUES (?, ?)", version, dirty); err != nil {
		t.Fatalf("写入版本数据库失败：%v", err)
	}
}
