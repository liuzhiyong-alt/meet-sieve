package migration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
)

const migrationSafetyMarginBytes = uint64(64 * 1024 * 1024)

// DiskSpaceReader 定义 coordinator 读取升级目标卷可用空间的系统边界。
type DiskSpaceReader func(path string) (uint64, error)

// MigrationRequest 描述同一工作目录内一次数据库升级的固定输入。
type MigrationRequest struct {
	WorkspacePath string
	JournalPath   string
	OperationID   string
}

// MigrationResult 返回升级是否发生及已创建的升级前备份。
type MigrationResult struct {
	Upgraded bool
	Backup   BackupResult
	Warnings []string
}

// MigrationCoordinator 编排备份、staging migration、文件切换与启动恢复，不负责工作目录切换。
type MigrationCoordinator struct {
	backupService *BackupService
	finalizer     *FoundationFinalizer
	clock         clock.Clock
	diskSpace     DiskSpaceReader
}

// NewMigrationCoordinator 创建数据库升级协调器；外部依赖在执行时校验以统一返回业务错误。
func NewMigrationCoordinator(
	backupService *BackupService,
	finalizer *FoundationFinalizer,
	currentClock clock.Clock,
	diskSpace DiskSpaceReader,
) *MigrationCoordinator {
	if diskSpace == nil {
		diskSpace = filesystem.AvailableBytes
	}
	return &MigrationCoordinator{
		backupService: backupService,
		finalizer:     finalizer,
		clock:         currentClock,
		diskSpace:     diskSpace,
	}
}

// Upgrade 在当前 schema 时直接返回；待升级场景将在后续 staging 步骤完成后安装。
func (coordinator *MigrationCoordinator) Upgrade(request MigrationRequest) (MigrationResult, error) {
	if err := validateMigrationRequest(coordinator, request); err != nil {
		return MigrationResult{}, err
	}
	databasePath := filepath.Join(request.WorkspacePath, "data", "meetings.db")
	source, err := readBackupSource(databasePath, database.CurrentSchemaVersion)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("读取升级源失败: %w", err)
	}
	if source.version >= database.CurrentSchemaVersion {
		return MigrationResult{}, nil
	}
	if err := coordinator.ensureMigrationDiskBudget(databasePath, request.WorkspacePath); err != nil {
		return MigrationResult{}, err
	}
	backup, err := coordinator.createUpgradeBackup(databasePath, request)
	if err != nil {
		return MigrationResult{}, err
	}
	paths, err := newStagingPaths(request)
	if err != nil {
		return MigrationResult{}, err
	}
	if err := copyVerifiedBackup(backup.DatabasePath, paths.stagingPath); err != nil {
		return MigrationResult{}, migrationFailure("copy_staging", err)
	}
	stagingInstalled := false
	journalWritten := false
	defer func() {
		if !stagingInstalled && !journalWritten {
			_ = os.Remove(paths.stagingPath)
		}
	}()
	if err := coordinator.migrateAndVerifyStaging(paths.stagingPath); err != nil {
		return MigrationResult{}, err
	}

	journal := coordinator.buildJournal(request, source.version, paths)
	if err := WriteMigrationJournal(request.JournalPath, journal); err != nil {
		return MigrationResult{}, migrationFailure("write_journal", err)
	}
	journalWritten = true
	if err := database.MoveFileExclusive(databasePath, paths.preSwitchPath); err != nil {
		return MigrationResult{}, migrationFailure("move_original", err)
	}
	journal.Phase = MigrationPhaseOriginalMoved
	if err := WriteMigrationJournal(request.JournalPath, journal); err != nil {
		return MigrationResult{}, migrationFailure("record_original_moved", err)
	}
	if err := database.MoveFileExclusive(paths.stagingPath, databasePath); err != nil {
		return MigrationResult{}, migrationFailure("install_staging", err)
	}
	stagingInstalled = true
	journal.Phase = MigrationPhaseStagingInstalled
	if err := WriteMigrationJournal(request.JournalPath, journal); err != nil {
		return MigrationResult{}, migrationFailure("record_staging_installed", err)
	}
	if err := verifyTargetDatabase(databasePath); err != nil {
		return coordinator.recoverFailedInstalledTarget(request.JournalPath, backup, err)
	}
	result := MigrationResult{Upgraded: true, Backup: backup}
	result.Warnings = cleanupSuccessfulSwitch(paths.preSwitchPath, request.JournalPath)
	return result, nil
}

// recoverFailedInstalledTarget 在新库落盘后复验失败时立即按 journal 恢复，避免把损坏文件留给下次启动处理。
func (coordinator *MigrationCoordinator) recoverFailedInstalledTarget(journalPath string, backup BackupResult, verifyErr error) (MigrationResult, error) {
	recovery, recoveryErr := coordinator.Recover(journalPath)
	if recoveryErr != nil {
		return MigrationResult{Backup: backup}, migrationFailure(
			"verify_and_recover_installed",
			fmt.Errorf("新库复验失败：%w；自动恢复失败：%v", verifyErr, recoveryErr),
		)
	}
	return MigrationResult{Backup: backup, Warnings: recovery.Warnings}, migrationFailure(
		"verify_installed",
		fmt.Errorf("新库复验失败，已自动恢复：%w", verifyErr),
	)
}

// createUpgradeBackup 在确认存在 pending migration 后创建并验证 SQLite Online Backup 对。
func (coordinator *MigrationCoordinator) createUpgradeBackup(databasePath string, request MigrationRequest) (BackupResult, error) {
	backup, err := coordinator.backupService.BackupIfRequired(BackupRequest{
		DatabasePath:    databasePath,
		BackupDirectory: filepath.Join(request.WorkspacePath, "data", "backups"),
		OperationID:     request.OperationID,
		TargetVersion:   database.CurrentSchemaVersion,
	})
	if err != nil {
		return BackupResult{}, apperr.Dependency(apperr.CodeDatabaseBackupFailed, err, apperr.WithOp("migration.backup.create"))
	}
	if !backup.Created {
		return BackupResult{}, migrationFailure("backup_missing", fmt.Errorf("待升级数据库未创建备份"))
	}
	return backup, nil
}

// migrateAndVerifyStaging 只操作 staging 副本，正式 meetings.db 在此之前保持字节不变。
func (coordinator *MigrationCoordinator) migrateAndVerifyStaging(stagingPath string) error {
	if err := database.Migrate(stagingPath); err != nil {
		return migrationFailure("migrate_staging", err)
	}
	staging, err := database.Open(stagingPath)
	if err != nil {
		return migrationFailure("open_staging", err)
	}
	sqlDB, err := staging.DB()
	if err == nil {
		_, err = coordinator.finalizer.Finalize(sqlDB, FinalizationTargetStaging)
	}
	closeErr := database.Close(staging)
	if err != nil {
		return migrationFailure("finalize_staging", err)
	}
	if closeErr != nil {
		return migrationFailure("close_staging", closeErr)
	}
	if err := verifyTargetDatabase(stagingPath); err != nil {
		return migrationFailure("verify_staging", err)
	}
	return nil
}

// buildJournal 构造不含绝对 data 文件路径的恢复记录，文件解析始终相对 workspace/data。
func (coordinator *MigrationCoordinator) buildJournal(request MigrationRequest, sourceVersion uint, paths stagingPaths) MigrationJournal {
	return MigrationJournal{
		SchemaVersion: migrationJournalSchemaVersion,
		OperationID:   request.OperationID,
		WorkspacePath: request.WorkspacePath,
		SourceVersion: sourceVersion,
		TargetVersion: database.CurrentSchemaVersion,
		StagingFile:   filepath.Base(paths.stagingPath),
		PreSwitchFile: filepath.Base(paths.preSwitchPath),
		Phase:         MigrationPhasePrepared,
		CreatedAtUTC:  coordinator.clock.Now().UTC().Format(time.RFC3339),
	}
}

// stagingPaths 限定本次 switch 可拥有的两个 data 目录临时文件。
type stagingPaths struct {
	stagingPath   string
	preSwitchPath string
}

// newStagingPaths 预先拒绝残留的同 operation 文件，避免重试覆盖未归属现场。
func newStagingPaths(request MigrationRequest) (stagingPaths, error) {
	dataPath := filepath.Join(request.WorkspacePath, "data")
	paths := stagingPaths{
		stagingPath:   filepath.Join(dataPath, ".meetings-staging-"+request.OperationID+".db"),
		preSwitchPath: filepath.Join(dataPath, ".meetings-pre-switch-"+request.OperationID+".db"),
	}
	for _, path := range []string{paths.stagingPath, paths.preSwitchPath} {
		if _, err := os.Lstat(path); err == nil {
			return stagingPaths{}, fmt.Errorf("升级临时文件已存在")
		} else if !os.IsNotExist(err) {
			return stagingPaths{}, fmt.Errorf("检查升级临时文件失败: %w", err)
		}
	}
	return paths, nil
}

// copyVerifiedBackup 用 O_EXCL 复制已经验证的备份，确保不会覆盖现场文件。
func copyVerifiedBackup(sourcePath string, stagingPath string) (err error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开已验证备份失败: %w", err)
	}
	defer source.Close()
	destination, err := os.OpenFile(stagingPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("创建 staging 数据库失败: %w", err)
	}
	defer func() {
		if closeErr := destination.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("关闭 staging 数据库失败: %w", closeErr)
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("复制已验证备份失败: %w", err)
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("同步 staging 数据库失败: %w", err)
	}
	return nil
}

// verifyTargetDatabase 复验新库完整性、目标版本与 typed identity，legacy 表残留也会被拒绝。
func verifyTargetDatabase(path string) error {
	db, err := database.Open(path)
	if err != nil {
		return fmt.Errorf("打开目标数据库失败: %w", err)
	}
	defer database.Close(db)
	if _, err := database.CheckSQLite(db); err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取目标数据库连接失败: %w", err)
	}
	version, err := database.ReadSchemaVersion(sqlDB)
	if err != nil || version.Dirty || version.Version != database.CurrentSchemaVersion {
		return fmt.Errorf("目标数据库 schema 不完整")
	}
	if _, err := database.ReadTypedIdentity(sqlDB); err != nil {
		return fmt.Errorf("目标数据库身份无效: %w", err)
	}
	return nil
}

// cleanupSuccessfulSwitch 只删除本次可验证 switch 的 pre-switch/journal；失败只保留 warning。
func cleanupSuccessfulSwitch(preSwitchPath string, journalPath string) []string {
	var warnings []string
	if err := os.Remove(preSwitchPath); err != nil && !os.IsNotExist(err) {
		warnings = append(warnings, "migration_pre_switch_cleanup_failed")
	}
	if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
		warnings = append(warnings, "migration_journal_cleanup_failed")
	}
	return warnings
}

// migrationFailure 将内部路径、SQLite 错误等排障细节收敛为稳定的用户可见错误码。
func migrationFailure(step string, cause error) error {
	return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, cause, apperr.WithOp("migration."+step))
}

// ensureMigrationDiskBudget 预留一个备份、一个 staging 副本及固定安全余量，空间不足时绝不创建备份。
func (coordinator *MigrationCoordinator) ensureMigrationDiskBudget(databasePath string, workspacePath string) error {
	info, err := os.Stat(databasePath)
	if err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("migration.budget.stat"))
	}
	if info.Size() < 0 || uint64(info.Size()) > (^uint64(0)-migrationSafetyMarginBytes)/2 {
		return apperr.Biz(apperr.CodeDatabaseDiskSpaceLow, apperr.WithOp("migration.budget.overflow"))
	}
	required := uint64(info.Size())*2 + migrationSafetyMarginBytes
	available, err := coordinator.diskSpace(workspacePath)
	if err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("migration.budget.available"))
	}
	if available < required {
		return apperr.Biz(apperr.CodeDatabaseDiskSpaceLow, apperr.WithOp("migration.budget.check"))
	}
	return nil
}

// validateMigrationRequest 拒绝空路径和非 UUID operation，避免 journal 指向任意文件。
func validateMigrationRequest(coordinator *MigrationCoordinator, request MigrationRequest) error {
	if coordinator == nil || coordinator.backupService == nil || coordinator.finalizer == nil || coordinator.clock == nil || coordinator.diskSpace == nil {
		return fmt.Errorf("migration coordinator 依赖不完整")
	}
	if !filepath.IsAbs(request.WorkspacePath) || !filepath.IsAbs(request.JournalPath) {
		return fmt.Errorf("migration coordinator 路径必须为绝对路径")
	}
	if strings.TrimSpace(request.OperationID) == "" || !isUUIDv4OperationID(request.OperationID) {
		return fmt.Errorf("migration coordinator operation_id 不合法")
	}
	return nil
}
