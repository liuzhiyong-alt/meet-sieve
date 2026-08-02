package migration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/service/migration"
)

// TestBackupIfRequired_CreatesVerifiedFoundationPair 验证 v1 待升级数据库使用 Online Backup 生成带 foundation 身份的文件与 manifest 对。
func TestBackupIfRequired_CreatesVerifiedFoundationPair(t *testing.T) {
	base := t.TempDir()
	sourcePath := filepath.Join(base, "meetings.db")
	if err := database.MigrateFS(sourcePath, foundationMigrationFiles(), "sqlite"); err != nil {
		t.Fatalf("准备 v1 数据库失败：%v", err)
	}
	service := migration.NewBackupService(clock.NewFixed(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)))
	result, err := service.BackupIfRequired(migration.BackupRequest{
		DatabasePath: sourcePath, BackupDirectory: filepath.Join(base, "backups"), OperationID: "11111111-1111-4111-8111-111111111111", TargetVersion: 2,
	})
	if err != nil {
		t.Fatalf("创建升级备份失败：%v", err)
	}
	if !result.Created || result.Manifest.SourceKind != migration.BackupSourceFoundationV1 || result.Manifest.DatabaseID != nil {
		t.Fatalf("v1 备份结果不正确：%+v", result)
	}
	if _, err := migration.ParseBackupManifestFile(result.ManifestPath); err != nil {
		t.Fatalf("备份 manifest 必须可严格读回：%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(base, "backups"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("备份目录只能留下数据库与 manifest 配对文件：entries=%v err=%v", entries, err)
	}
}

// TestBackupIfRequired_SkipsCurrentSchema 验证无待执行 migration 时不创建备份文件或 manifest。
func TestBackupIfRequired_SkipsCurrentSchema(t *testing.T) {
	base := t.TempDir()
	sourcePath := filepath.Join(base, "meetings.db")
	if err := database.Migrate(sourcePath); err != nil {
		t.Fatalf("准备 v2 数据库失败：%v", err)
	}
	service := migration.NewBackupService(clock.NewFixed(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)))
	result, err := service.BackupIfRequired(migration.BackupRequest{
		DatabasePath: sourcePath, BackupDirectory: filepath.Join(base, "backups"), OperationID: "op", TargetVersion: 2,
	})
	if err != nil {
		t.Fatalf("无升级时检查备份失败：%v", err)
	}
	if result.Created {
		t.Fatalf("当前 schema 不得创建备份：%+v", result)
	}
}

// TestBackupIfRequired_HandlesSpecialCharactersInSourcePath 验证源库路径含 URL 特殊字符时仍以只读模式创建一致备份。
func TestBackupIfRequired_HandlesSpecialCharactersInSourcePath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "会议 #1")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("创建特殊字符目录失败：%v", err)
	}
	sourcePath := filepath.Join(base, "meetings.db")
	if err := database.MigrateFS(sourcePath, foundationMigrationFiles(), "sqlite"); err != nil {
		t.Fatalf("准备 v1 数据库失败：%v", err)
	}
	service := migration.NewBackupService(clock.NewFixed(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)))
	result, err := service.BackupIfRequired(migration.BackupRequest{
		DatabasePath: sourcePath, BackupDirectory: filepath.Join(base, "backups"), OperationID: "special-path", TargetVersion: 2,
	})
	if err != nil {
		t.Fatalf("特殊字符路径必须可以创建备份：%v", err)
	}
	if !result.Created {
		t.Fatal("特殊字符路径的待升级数据库必须生成备份")
	}
}

// TestPruneBackups_RemovesOnlyVerifiedOldPairs 验证轮转只删除超过三份的完整可验证备份对，未知文件始终保留。
func TestPruneBackups_RemovesOnlyVerifiedOldPairs(t *testing.T) {
	base := t.TempDir()
	sourcePath := filepath.Join(base, "meetings.db")
	if err := database.MigrateFS(sourcePath, foundationMigrationFiles(), "sqlite"); err != nil {
		t.Fatalf("准备 v1 数据库失败：%v", err)
	}
	backupDirectory := filepath.Join(base, "backups")
	for index := 0; index < 5; index++ {
		service := migration.NewBackupService(clock.NewFixed(time.Date(2026, 7, 31+index, 12, 0, 0, 0, time.UTC)))
		if _, err := service.BackupIfRequired(migration.BackupRequest{
			DatabasePath: sourcePath, BackupDirectory: backupDirectory, OperationID: fmt.Sprintf("operation-%d", index), TargetVersion: 2,
		}); err != nil {
			t.Fatalf("创建第 %d 份备份失败：%v", index, err)
		}
	}
	unknownPath := filepath.Join(backupDirectory, "unknown.txt")
	if err := os.WriteFile(unknownPath, []byte("preserve"), 0o600); err != nil {
		t.Fatalf("写入未知文件失败：%v", err)
	}

	result, err := migration.PruneBackups(backupDirectory, 3)
	if err != nil {
		t.Fatalf("轮转备份失败：%v", err)
	}
	if result.RemovedPairs != 2 {
		t.Fatalf("轮转数量不正确：%+v", result)
	}
	entries, err := os.ReadDir(backupDirectory)
	if err != nil {
		t.Fatalf("读取轮转目录失败：%v", err)
	}
	if len(entries) != 7 {
		t.Fatalf("轮转后应保留三对文件和未知文件：%v", entries)
	}
	if content, err := os.ReadFile(unknownPath); err != nil || string(content) != "preserve" {
		t.Fatalf("未知文件不得删除：content=%q err=%v", content, err)
	}
}

// foundationMigrationFiles 返回只含 Step 0 的独立迁移 fixture。
func foundationMigrationFiles() fstest.MapFS {
	return fstest.MapFS{
		"sqlite/000001_foundation.up.sql":   {Data: []byte("CREATE TABLE app_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)")},
		"sqlite/000001_foundation.down.sql": {Data: []byte("DROP TABLE app_metadata")},
	}
}
