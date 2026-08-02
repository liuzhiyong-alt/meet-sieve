package migration

import (
	"fmt"
	"os"
	"path/filepath"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
)

// RecoveryInstalledSource 表示恢复后 meetings.db 实际采用的已验证副本。
type RecoveryInstalledSource string

const (
	// RecoveryInstalledNone 表示没有待恢复 journal。
	RecoveryInstalledNone RecoveryInstalledSource = "none"
	// RecoveryInstalledOriginal 表示恢复了已验证的 pre-switch 原库。
	RecoveryInstalledOriginal RecoveryInstalledSource = "pre_switch"
	// RecoveryInstalledStaging 表示安装了已验证的升级后 staging 库。
	RecoveryInstalledStaging RecoveryInstalledSource = "staging"
	// RecoveryInstalledCurrent 表示已安装目标库已经通过复验。
	RecoveryInstalledCurrent RecoveryInstalledSource = "current"
)

// RecoveryResult 返回启动恢复是否处理了 journal、采用的数据库及非阻断清理告警。
type RecoveryResult struct {
	Recovered       bool
	InstalledSource RecoveryInstalledSource
	Warnings        []string
}

// Recover 按 journal 和真实文件状态恢复同一 workspace 内未完成的 meetings.db 切换。
func (coordinator *MigrationCoordinator) Recover(journalPath string) (RecoveryResult, error) {
	if coordinator == nil || !filepath.IsAbs(journalPath) {
		return RecoveryResult{}, migrationFailure("recovery.validate", fmt.Errorf("migration journal 路径不合法"))
	}
	if _, err := os.Stat(journalPath); os.IsNotExist(err) {
		return RecoveryResult{InstalledSource: RecoveryInstalledNone}, nil
	} else if err != nil {
		return RecoveryResult{}, migrationFailure("recovery.stat_journal", err)
	}
	journal, err := ReadMigrationJournal(journalPath)
	if err != nil {
		return RecoveryResult{}, migrationFailure("recovery.read_journal", err)
	}
	paths := buildRecoveryPaths(journal)
	if err := verifyRecoveryPaths(paths); err != nil {
		return RecoveryResult{}, migrationFailure("recovery.paths", err)
	}
	return recoverJournalSwitch(journalPath, journal, paths)
}

// recoveryPaths 是 journal 中受严格文件名约束的 data 目录三个固定路径。
type recoveryPaths struct {
	databasePath  string
	stagingPath   string
	preSwitchPath string
}

// buildRecoveryPaths 根据严格校验过的 journal 解析同一工作目录内文件，禁止使用任意绝对文件路径。
func buildRecoveryPaths(journal MigrationJournal) recoveryPaths {
	dataPath := filepath.Join(journal.WorkspacePath, "data")
	return recoveryPaths{
		databasePath:  filepath.Join(dataPath, "meetings.db"),
		stagingPath:   filepath.Join(dataPath, journal.StagingFile),
		preSwitchPath: filepath.Join(dataPath, journal.PreSwitchFile),
	}
}

// verifyRecoveryPaths 防止 journal 指向不存在 workspace/data 之外的路径。
func verifyRecoveryPaths(paths recoveryPaths) error {
	for _, path := range []string{paths.databasePath, paths.stagingPath, paths.preSwitchPath} {
		if filepath.Base(path) != "meetings.db" && !isOwnedSwitchFile(filepath.Base(path), ".meetings-staging-") && !isOwnedSwitchFile(filepath.Base(path), ".meetings-pre-switch-") {
			return fmt.Errorf("恢复文件名不合法")
		}
	}
	return nil
}

// recoverJournalSwitch 基于可验证内容而非仅 phase 推断崩溃点，优先安装完整 staging。
func recoverJournalSwitch(journalPath string, journal MigrationJournal, paths recoveryPaths) (RecoveryResult, error) {
	originalExists := fileExists(paths.databasePath)
	stagingValid := isValidTargetDatabase(paths.stagingPath)
	preSwitchValid := isValidPreSwitchDatabase(paths.preSwitchPath, journal)

	if originalExists && isValidTargetDatabase(paths.databasePath) {
		return finishRecoveredCurrent(journalPath, paths, preSwitchValid, stagingValid)
	}
	if originalExists && isValidPreSwitchDatabase(paths.databasePath, journal) && !fileExists(paths.preSwitchPath) && stagingValid {
		return finishRecoveredPrepared(journalPath, paths.stagingPath)
	}
	if !originalExists && stagingValid && preSwitchValid {
		if err := database.MoveFileExclusive(paths.stagingPath, paths.databasePath); err != nil {
			return RecoveryResult{}, migrationFailure("recovery.install_staging", err)
		}
		if err := verifyTargetDatabase(paths.databasePath); err != nil {
			return RecoveryResult{}, migrationFailure("recovery.verify_staging", err)
		}
		return finishRecoveredSwitch(journalPath, paths.preSwitchPath, RecoveryInstalledStaging)
	}
	if !originalExists && !fileExists(paths.stagingPath) && preSwitchValid {
		if err := database.MoveFileExclusive(paths.preSwitchPath, paths.databasePath); err != nil {
			return RecoveryResult{}, migrationFailure("recovery.restore_pre_switch", err)
		}
		return finishRecoveredSwitch(journalPath, "", RecoveryInstalledOriginal)
	}
	if originalExists && journal.Phase == MigrationPhaseStagingInstalled && !fileExists(paths.stagingPath) && preSwitchValid {
		return restorePreSwitchAfterInvalidInstall(journalPath, paths, journal)
	}
	return RecoveryResult{}, apperr.Dependency(
		apperr.CodeDatabaseMigrationFailed,
		fmt.Errorf("migration journal 现场无法安全恢复"),
		apperr.WithOp("migration.recovery.unrecoverable"),
	)
}

// restorePreSwitchAfterInvalidInstall 保留无法复验的新库，再原子恢复已验证原库，避免原地覆盖诊断现场。
func restorePreSwitchAfterInvalidInstall(journalPath string, paths recoveryPaths, journal MigrationJournal) (RecoveryResult, error) {
	if err := database.MoveFileExclusive(paths.databasePath, paths.stagingPath); err != nil {
		return RecoveryResult{}, migrationFailure("recovery.preserve_invalid_install", err)
	}
	if err := database.MoveFileExclusive(paths.preSwitchPath, paths.databasePath); err != nil {
		return RecoveryResult{}, migrationFailure("recovery.restore_pre_switch", err)
	}
	if !isValidPreSwitchDatabase(paths.databasePath, journal) {
		return RecoveryResult{}, migrationFailure("recovery.verify_restored_pre_switch", fmt.Errorf("恢复后的原库无效"))
	}
	result := RecoveryResult{
		Recovered:       true,
		InstalledSource: RecoveryInstalledOriginal,
		Warnings:        []string{"migration_invalid_staging_preserved"},
	}
	result.Warnings = append(result.Warnings, removeRecoveryFile(journalPath, "migration_journal_cleanup_failed")...)
	return result, nil
}

// finishRecoveredPrepared 保留未移动的原库，只删除已经验证且尚未安装的 staging 副本。
func finishRecoveredPrepared(journalPath string, stagingPath string) (RecoveryResult, error) {
	result := RecoveryResult{Recovered: true, InstalledSource: RecoveryInstalledOriginal}
	result.Warnings = append(result.Warnings, removeRecoveryFile(stagingPath, "migration_staging_cleanup_failed")...)
	if len(result.Warnings) == 0 {
		result.Warnings = append(result.Warnings, removeRecoveryFile(journalPath, "migration_journal_cleanup_failed")...)
	}
	return result, nil
}

// finishRecoveredCurrent 清理已验证的冗余 staging/pre-switch，无法验证的文件不会被删除。
func finishRecoveredCurrent(journalPath string, paths recoveryPaths, preSwitchValid bool, stagingValid bool) (RecoveryResult, error) {
	result := RecoveryResult{Recovered: true, InstalledSource: RecoveryInstalledCurrent}
	if preSwitchValid {
		result.Warnings = append(result.Warnings, removeRecoveryFile(paths.preSwitchPath, "migration_pre_switch_cleanup_failed")...)
	}
	if stagingValid {
		result.Warnings = append(result.Warnings, removeRecoveryFile(paths.stagingPath, "migration_staging_cleanup_failed")...)
	}
	if len(result.Warnings) == 0 {
		result.Warnings = append(result.Warnings, removeRecoveryFile(journalPath, "migration_journal_cleanup_failed")...)
	}
	return result, nil
}

// finishRecoveredSwitch 在明确安装/恢复成功后清理已验证原副本和 journal。
func finishRecoveredSwitch(journalPath string, preSwitchPath string, source RecoveryInstalledSource) (RecoveryResult, error) {
	result := RecoveryResult{Recovered: true, InstalledSource: source}
	if preSwitchPath != "" {
		result.Warnings = append(result.Warnings, removeRecoveryFile(preSwitchPath, "migration_pre_switch_cleanup_failed")...)
	}
	if len(result.Warnings) == 0 {
		result.Warnings = append(result.Warnings, removeRecoveryFile(journalPath, "migration_journal_cleanup_failed")...)
	}
	return result, nil
}

// removeRecoveryFile 只接收已由调用者验证归属的切换文件，失败时保留现场并返回 warning。
func removeRecoveryFile(path string, warning string) []string {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return []string{warning}
	}
	return nil
}

// isValidTargetDatabase 仅把版本、integrity 和 typed identity 都合法的 v2 文件认作可安装新库。
func isValidTargetDatabase(path string) bool {
	if !fileExists(path) {
		return false
	}
	return verifyTargetDatabase(path) == nil
}

// isValidPreSwitchDatabase 验证 pre-switch 保留的是 journal 所述的可升级旧库，不猜测陌生数据库。
func isValidPreSwitchDatabase(path string, journal MigrationJournal) bool {
	if !fileExists(path) {
		return false
	}
	source, err := readBackupSource(path, journal.TargetVersion)
	return err == nil && source.version == journal.SourceVersion
}

// fileExists 区分不存在与无法安全读取；后者一律当作不可恢复现场。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
