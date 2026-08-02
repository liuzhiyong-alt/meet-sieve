package workspace_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"meet-sieve/internal/domain/metadata"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
	workspace "meet-sieve/internal/service/workspace"

	_ "github.com/mattn/go-sqlite3"
)

// TestInitialize_CreatesFinalizedDatabaseAtomically 验证不存在路径通过临时数据库完成 migration/finalizer 后才安装 meetings.db。
func TestInitialize_CreatesFinalizedDatabaseAtomically(t *testing.T) {
	base := t.TempDir()
	workspacePath := filepath.Join(base, "new-workspace")
	initializer := newInitializer(t, base)

	candidate, err := initializer.Initialize(workspacePath)
	if err != nil {
		t.Fatalf("初始化工作目录失败：%v", err)
	}
	if candidate.DatabaseID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("初始化后 identity 不正确：%+v", candidate)
	}
	databasePath := filepath.Join(workspacePath, "data", "meetings.db")
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("初始化后 meetings.db 不存在：%v", err)
	}
	assertDirectoryExists(t, filepath.Join(workspacePath, "data", "backups"))
	assertDirectoryExists(t, filepath.Join(workspacePath, "data", "voice-samples"))
	assertDirectoryExists(t, filepath.Join(workspacePath, "meetings"))
	if matches, err := filepath.Glob(filepath.Join(workspacePath, "data", ".meetings-init-*.db")); err != nil || len(matches) != 0 {
		t.Fatalf("安装完成后不得保留 init 临时库：matches=%v err=%v", matches, err)
	}
	assertInitializedIdentity(t, databasePath)
	if runtime.GOOS == "darwin" {
		assertPermission(t, workspacePath, 0o700)
		assertPermission(t, databasePath, 0o600)
	}
}

// TestInitialize_RejectsInvalidNonEmptyDirectoryWithoutMutation 验证无效非空目录既不安装数据库也不删除未知文件。
func TestInitialize_RejectsInvalidNonEmptyDirectoryWithoutMutation(t *testing.T) {
	base := t.TempDir()
	workspacePath := filepath.Join(base, "non-empty")
	if err := os.Mkdir(workspacePath, 0o700); err != nil {
		t.Fatalf("创建非空目录失败：%v", err)
	}
	unknownPath := filepath.Join(workspacePath, "preserve.txt")
	if err := os.WriteFile(unknownPath, []byte("preserve"), 0o600); err != nil {
		t.Fatalf("写入未知文件失败：%v", err)
	}
	initializer := newInitializer(t, base)

	if _, err := initializer.Initialize(workspacePath); err == nil {
		t.Fatal("无效非空目录必须拒绝初始化")
	}
	if content, err := os.ReadFile(unknownPath); err != nil || string(content) != "preserve" {
		t.Fatalf("初始化不得修改未知文件：content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(workspacePath, "data", "meetings.db")); !os.IsNotExist(err) {
		t.Fatalf("无效非空目录不得安装数据库：err=%v", err)
	}
}

// newInitializer 创建使用确定性身份生成器的真实目录初始化器。
func newInitializer(t *testing.T, base string) *workspace.Initializer {
	t.Helper()
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
	return workspace.NewInitializer(newInspector(t, base, 11*gibibyte), finalizer, identity.NewFixedGenerator("44444444-4444-4444-8444-444444444444"))
}

// assertInitializedIdentity 验证安装后的数据库可读且 singleton 完整。
func assertInitializedIdentity(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开初始化数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	identityValue, err := database.ReadTypedIdentity(db)
	if err != nil {
		t.Fatalf("初始化 identity 无效：%v", err)
	}
	if identityValue.DatabaseID == "" {
		t.Fatal("初始化 identity 不得为空")
	}
}

// assertDirectoryExists 验证登记目录已经创建。
func assertDirectoryExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("登记目录不存在：path=%s err=%v", path, err)
	}
}

// assertPermission 验证 macOS 新建目录或数据库使用 owner-only 权限。
func assertPermission(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取权限失败：%v", err)
	}
	if actual := info.Mode().Perm(); actual != expected {
		t.Fatalf("权限不正确：path=%s got=%#o want=%#o", path, actual, expected)
	}
}
