package migration_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/domain/metadata"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
)

// TestMigrationCoordinator_SkipsCurrentDatabase 验证当前 schema 不创建备份、staging 或 journal。
func TestMigrationCoordinator_SkipsCurrentDatabase(t *testing.T) {
	workspacePath := t.TempDir()
	databasePath := filepath.Join(workspacePath, "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatalf("创建 data 目录失败：%v", err)
	}
	if err := database.Migrate(databasePath); err != nil {
		t.Fatalf("准备当前 schema 数据库失败：%v", err)
	}

	journalPath := filepath.Join(t.TempDir(), "database-migration.json")
	result, err := newMigrationCoordinator().Upgrade(migration.MigrationRequest{
		WorkspacePath: workspacePath,
		JournalPath:   journalPath,
		OperationID:   "11111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatalf("当前数据库不应升级失败：%v", err)
	}
	if result.Upgraded || result.Backup.Created {
		t.Fatalf("当前数据库不得执行升级或备份：%+v", result)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("当前数据库不得创建 journal：%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(workspacePath, "data"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "meetings.db" {
		t.Fatalf("当前数据库不得创建 staging 文件：entries=%v err=%v", entries, err)
	}
}

// TestMigrationCoordinator_BlocksDirtyDatabaseBeforeBackup 验证 dirty migration 状态在创建任何备份前阻断升级。
func TestMigrationCoordinator_BlocksDirtyDatabaseBeforeBackup(t *testing.T) {
	workspacePath := t.TempDir()
	databasePath := createFoundationWorkspaceDatabase(t, workspacePath)
	db, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("打开 foundation 数据库失败：%v", err)
	}
	if err := db.Exec("UPDATE schema_migrations SET dirty = 1").Error; err != nil {
		t.Fatalf("设置 dirty 状态失败：%v", err)
	}
	if err := database.Close(db); err != nil {
		t.Fatalf("关闭 dirty 数据库失败：%v", err)
	}

	_, err = newMigrationCoordinator().Upgrade(migration.MigrationRequest{
		WorkspacePath: workspacePath,
		JournalPath:   filepath.Join(t.TempDir(), "database-migration.json"),
		OperationID:   "11111111-1111-4111-8111-111111111111",
	})
	if err == nil {
		t.Fatal("dirty 数据库必须阻断升级")
	}
	if _, statErr := os.Stat(filepath.Join(workspacePath, "data", "backups")); !os.IsNotExist(statErr) {
		t.Fatalf("dirty 数据库不得创建备份目录：%v", statErr)
	}
}

// TestMigrationCoordinator_BlocksLowDiskBeforeBackup 验证 staging 预算不足时原库与备份目录均不被修改。
func TestMigrationCoordinator_BlocksLowDiskBeforeBackup(t *testing.T) {
	workspacePath := t.TempDir()
	createFoundationWorkspaceDatabase(t, workspacePath)

	_, err := newMigrationCoordinatorWithDisk(func(string) (uint64, error) { return 0, nil }).Upgrade(migration.MigrationRequest{
		WorkspacePath: workspacePath,
		JournalPath:   filepath.Join(t.TempDir(), "database-migration.json"),
		OperationID:   "11111111-1111-4111-8111-111111111111",
	})
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeDatabaseDiskSpaceLow.ErrorCode {
		t.Fatalf("空间不足必须返回稳定错误码：%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspacePath, "data", "backups")); !os.IsNotExist(statErr) {
		t.Fatalf("空间不足不得创建备份目录：%v", statErr)
	}
}

// TestMigrationCoordinator_UpgradesStagingAndInstallsVerifiedDatabase 验证 v1 只在 staging 升级，安装后正式库为完整 v2，且留下可验证备份对。
func TestMigrationCoordinator_UpgradesStagingAndInstallsVerifiedDatabase(t *testing.T) {
	workspacePath := t.TempDir()
	databasePath := createFoundationWorkspaceDatabase(t, workspacePath)
	journalPath := filepath.Join(t.TempDir(), "database-migration.json")

	result, err := newMigrationCoordinator().Upgrade(migration.MigrationRequest{
		WorkspacePath: workspacePath,
		JournalPath:   journalPath,
		OperationID:   "11111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatalf("v1 数据库升级失败：%v", err)
	}
	if !result.Upgraded || !result.Backup.Created {
		t.Fatalf("升级结果必须包含已安装数据库和备份：%+v", result)
	}
	assertCurrentTypedDatabase(t, databasePath)
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("成功升级必须删除 journal：%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(workspacePath, "data"))
	if err != nil {
		t.Fatalf("读取 data 目录失败：%v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "meetings.db" && entry.Name() != "backups" {
			t.Fatalf("成功升级不得遗留 staging/pre-switch 文件：%s", entry.Name())
		}
	}
	backupEntries, err := os.ReadDir(filepath.Join(workspacePath, "data", "backups"))
	if err != nil || len(backupEntries) != 2 {
		t.Fatalf("升级前必须保留一对备份文件：entries=%v err=%v", backupEntries, err)
	}
}

// newMigrationCoordinator 创建使用真实 migration/finalizer 的 coordinator，仅把磁盘空间固定为充足值。
func newMigrationCoordinator() *migration.MigrationCoordinator {
	return newMigrationCoordinatorWithDisk(func(string) (uint64, error) { return math.MaxUint64, nil })
}

// newMigrationCoordinatorWithDisk 创建可固定磁盘空间的 coordinator，避免集成测试依赖真实卷容量。
func newMigrationCoordinatorWithDisk(diskSpace migration.DiskSpaceReader) *migration.MigrationCoordinator {
	currentClock := clock.NewFixed(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	finalizer := migration.NewFoundationFinalizer(
		identity.NewUUIDGenerator(),
		metadata.NewSecureDeviceCodeGenerator(),
		currentClock,
		"0.1.0-test",
	)
	return migration.NewMigrationCoordinator(
		migration.NewBackupService(currentClock),
		finalizer,
		currentClock,
		diskSpace,
	)
}

// createFoundationWorkspaceDatabase 准备只含 Step 0 foundation 的工作目录数据库。
func createFoundationWorkspaceDatabase(t *testing.T, workspacePath string) string {
	t.Helper()
	databasePath := filepath.Join(workspacePath, "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatalf("创建 data 目录失败：%v", err)
	}
	if err := database.MigrateFS(databasePath, foundationMigrationFiles(), "sqlite"); err != nil {
		t.Fatalf("准备 foundation 数据库失败：%v", err)
	}
	return databasePath
}

// assertCurrentTypedDatabase 验证正式库已完成 v2 finalizer，不能保留 legacy singleton。
func assertCurrentTypedDatabase(t *testing.T, databasePath string) {
	t.Helper()
	db, err := database.Open(databasePath)
	if err != nil {
		t.Fatalf("打开升级后数据库失败：%v", err)
	}
	defer database.Close(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取升级后原生数据库连接失败：%v", err)
	}
	version, err := database.ReadSchemaVersion(sqlDB)
	if err != nil || version.Version != database.CurrentSchemaVersion || version.Dirty {
		t.Fatalf("升级后 schema 不正确：version=%+v err=%v", version, err)
	}
	if _, err := database.ReadTypedIdentity(sqlDB); err != nil {
		t.Fatalf("升级后必须有合法 typed identity：%v", err)
	}
}
