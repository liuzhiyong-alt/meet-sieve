package migration_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/domain/metadata"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
)

// TestMigrationCoordinator_RecoversOriginalMovedCrash 验证崩溃发生在原库移出、staging 尚未安装时，重启会安装已验证新库。
func TestMigrationCoordinator_RecoversOriginalMovedCrash(t *testing.T) {
	workspacePath := t.TempDir()
	databasePath := createFoundationWorkspaceDatabase(t, workspacePath)
	operationID := "11111111-1111-4111-8111-111111111111"
	dataPath := filepath.Dir(databasePath)
	preSwitchPath := filepath.Join(dataPath, ".meetings-pre-switch-"+operationID+".db")
	stagingPath := filepath.Join(dataPath, ".meetings-staging-"+operationID+".db")
	if err := os.Rename(databasePath, preSwitchPath); err != nil {
		t.Fatalf("模拟移动原数据库失败：%v", err)
	}
	createFinalizedV2Database(t, stagingPath)

	journalPath := filepath.Join(t.TempDir(), "database-migration.json")
	journal := migration.MigrationJournal{
		SchemaVersion: 1, OperationID: operationID, WorkspacePath: workspacePath,
		SourceVersion: 1, TargetVersion: 2,
		StagingFile: filepath.Base(stagingPath), PreSwitchFile: filepath.Base(preSwitchPath),
		Phase: migration.MigrationPhaseOriginalMoved, CreatedAtUTC: "2026-07-31T12:00:00Z",
	}
	if err := migration.WriteMigrationJournal(journalPath, journal); err != nil {
		t.Fatalf("写入 crash journal 失败：%v", err)
	}

	result, err := newMigrationCoordinator().Recover(journalPath)
	if err != nil {
		t.Fatalf("original_moved crash 必须可恢复：%v", err)
	}
	if !result.Recovered || result.InstalledSource != migration.RecoveryInstalledStaging {
		t.Fatalf("恢复结果不正确：%+v", result)
	}
	assertCurrentTypedDatabase(t, databasePath)
	for _, path := range []string{journalPath, preSwitchPath, stagingPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("恢复成功不得遗留切换现场：path=%s err=%v", path, err)
		}
	}
}

// TestMigrationCoordinator_RecoversPreparedCrash 验证 crash 发生在 journal 持久化后、原库移动前时保留原库并删除已验证 staging。
func TestMigrationCoordinator_RecoversPreparedCrash(t *testing.T) {
	workspacePath := t.TempDir()
	databasePath := createFoundationWorkspaceDatabase(t, workspacePath)
	operationID := "11111111-1111-4111-8111-111111111111"
	stagingPath := filepath.Join(filepath.Dir(databasePath), ".meetings-staging-"+operationID+".db")
	createFinalizedV2Database(t, stagingPath)
	journalPath := filepath.Join(t.TempDir(), "database-migration.json")
	journal := migration.MigrationJournal{
		SchemaVersion: 1, OperationID: operationID, WorkspacePath: workspacePath,
		SourceVersion: 1, TargetVersion: 2,
		StagingFile: filepath.Base(stagingPath), PreSwitchFile: ".meetings-pre-switch-" + operationID + ".db",
		Phase: migration.MigrationPhasePrepared, CreatedAtUTC: "2026-07-31T12:00:00Z",
	}
	if err := migration.WriteMigrationJournal(journalPath, journal); err != nil {
		t.Fatalf("写入 prepared journal 失败：%v", err)
	}

	result, err := newMigrationCoordinator().Recover(journalPath)
	if err != nil {
		t.Fatalf("prepared crash 必须可恢复：%v", err)
	}
	if !result.Recovered {
		t.Fatalf("prepared crash 应报告完成恢复：%+v", result)
	}
	assertFoundationDatabase(t, databasePath)
	for _, path := range []string{journalPath, stagingPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("prepared 恢复不得遗留已验证 staging/journal：path=%s err=%v", path, err)
		}
	}
}

// TestMigrationCoordinator_RestoresPreSwitchWhenInstalledDatabaseInvalid 验证新库复验失败时恢复原库，并保留损坏新库作为诊断现场。
func TestMigrationCoordinator_RestoresPreSwitchWhenInstalledDatabaseInvalid(t *testing.T) {
	workspacePath := t.TempDir()
	databasePath := createFoundationWorkspaceDatabase(t, workspacePath)
	operationID := "11111111-1111-4111-8111-111111111111"
	dataPath := filepath.Dir(databasePath)
	preSwitchPath := filepath.Join(dataPath, ".meetings-pre-switch-"+operationID+".db")
	stagingPath := filepath.Join(dataPath, ".meetings-staging-"+operationID+".db")
	if err := os.Rename(databasePath, preSwitchPath); err != nil {
		t.Fatalf("准备 pre-switch 原库失败：%v", err)
	}
	if err := os.WriteFile(databasePath, []byte("damaged-new-database"), 0o600); err != nil {
		t.Fatalf("准备损坏新库失败：%v", err)
	}
	journalPath := filepath.Join(t.TempDir(), "database-migration.json")
	journal := migration.MigrationJournal{
		SchemaVersion: 1, OperationID: operationID, WorkspacePath: workspacePath,
		SourceVersion: 1, TargetVersion: 2,
		StagingFile: filepath.Base(stagingPath), PreSwitchFile: filepath.Base(preSwitchPath),
		Phase: migration.MigrationPhaseStagingInstalled, CreatedAtUTC: "2026-07-31T12:00:00Z",
	}
	if err := migration.WriteMigrationJournal(journalPath, journal); err != nil {
		t.Fatalf("写入 staging_installed journal 失败：%v", err)
	}

	result, err := newMigrationCoordinator().Recover(journalPath)
	if err != nil {
		t.Fatalf("损坏新库必须恢复 pre-switch 原库：%v", err)
	}
	if !result.Recovered || result.InstalledSource != migration.RecoveryInstalledOriginal {
		t.Fatalf("恢复结果不正确：%+v", result)
	}
	assertFoundationDatabase(t, databasePath)
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("损坏新库必须保留为诊断现场：%v", err)
	}
	if _, err := os.Stat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("恢复原库成功后应删除 journal，避免再次阻断启动：%v", err)
	}
}

// TestMigrationCoordinator_RecoversStagingInstalledCrash 验证 staging 已安装但进程来不及清理时，会确认新库并清理已验证旧库。
func TestMigrationCoordinator_RecoversStagingInstalledCrash(t *testing.T) {
	workspacePath := t.TempDir()
	databasePath := createFoundationWorkspaceDatabase(t, workspacePath)
	operationID := "11111111-1111-4111-8111-111111111111"
	dataPath := filepath.Dir(databasePath)
	preSwitchPath := filepath.Join(dataPath, ".meetings-pre-switch-"+operationID+".db")
	if err := os.Rename(databasePath, preSwitchPath); err != nil {
		t.Fatalf("准备 pre-switch 原库失败：%v", err)
	}
	createFinalizedV2Database(t, databasePath)
	journalPath := filepath.Join(t.TempDir(), "database-migration.json")
	journal := migration.MigrationJournal{
		SchemaVersion: 1, OperationID: operationID, WorkspacePath: workspacePath,
		SourceVersion: 1, TargetVersion: 2,
		StagingFile: ".meetings-staging-" + operationID + ".db", PreSwitchFile: filepath.Base(preSwitchPath),
		Phase: migration.MigrationPhaseStagingInstalled, CreatedAtUTC: "2026-07-31T12:00:00Z",
	}
	if err := migration.WriteMigrationJournal(journalPath, journal); err != nil {
		t.Fatalf("写入 staging_installed journal 失败：%v", err)
	}

	result, err := newMigrationCoordinator().Recover(journalPath)
	if err != nil {
		t.Fatalf("staging_installed crash 必须可恢复：%v", err)
	}
	if !result.Recovered || result.InstalledSource != migration.RecoveryInstalledCurrent {
		t.Fatalf("恢复结果不正确：%+v", result)
	}
	assertCurrentTypedDatabase(t, databasePath)
	for _, path := range []string{journalPath, preSwitchPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("恢复成功不得遗留 journal/pre-switch：path=%s err=%v", path, err)
		}
	}
}

// createFinalizedV2Database 准备拥有合法 typed identity 的目标 schema staging fixture。
func createFinalizedV2Database(t *testing.T, path string) {
	t.Helper()
	if err := database.Migrate(path); err != nil {
		t.Fatalf("创建 v2 staging 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 v2 staging 失败：%v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取 v2 staging 原生连接失败：%v", err)
	}
	finalizer := migration.NewFoundationFinalizer(
		identity.NewUUIDGenerator(), metadata.NewSecureDeviceCodeGenerator(),
		clock.NewFixed(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)), "0.1.0-test",
	)
	if _, err := finalizer.Finalize(sqlDB, migration.FinalizationTargetStaging); err != nil {
		t.Fatalf("完成 v2 staging singleton 失败：%v", err)
	}
	if err := database.Close(db); err != nil {
		t.Fatalf("关闭 v2 staging 失败：%v", err)
	}
}

// assertFoundationDatabase 验证恢复后的原库仍是可升级且未被原地修改的 Step 0 foundation。
func assertFoundationDatabase(t *testing.T, path string) {
	t.Helper()
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 foundation 数据库失败：%v", err)
	}
	defer database.Close(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取 foundation 原生连接失败：%v", err)
	}
	version, err := database.ReadSchemaVersion(sqlDB)
	if err != nil || version.Version != 1 || version.Dirty {
		t.Fatalf("foundation 版本不正确：version=%+v err=%v", version, err)
	}
	if err := database.ValidateStep0Fingerprint(sqlDB, version); err != nil {
		t.Fatalf("foundation 指纹必须保留：%v", err)
	}
}
