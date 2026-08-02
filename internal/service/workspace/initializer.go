package workspace

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"

	_ "github.com/mattn/go-sqlite3"
)

const (
	workspaceDirectoryPermission = 0o700
	databaseFilePermission       = 0o600
)

// Initializer 仅为不存在或真正空的工作目录创建 Step 1 登记结构和数据库。
type Initializer struct {
	inspector   *Inspector
	finalizer   *migration.FoundationFinalizer
	operationID identity.Generator
}

// NewInitializer 创建工作目录初始化器；其依赖均由上层显式装配。
func NewInitializer(inspector *Inspector, finalizer *migration.FoundationFinalizer, operationID identity.Generator) *Initializer {
	return &Initializer{inspector: inspector, finalizer: finalizer, operationID: operationID}
}

// Initialize 原子安装新数据库；失败时只清理由本次操作创建的临时数据库文件。
func (initializer *Initializer) Initialize(path string) (candidate domainworkspace.WorkspaceCandidate, err error) {
	if err := initializer.validateDependencies(); err != nil {
		return domainworkspace.WorkspaceCandidate{}, err
	}
	candidate = initializer.inspector.Inspect(path)
	if err := initializationEligibilityError(candidate); err != nil {
		return candidate, err
	}
	if err := initializer.ensureWorkspaceDirectories(candidate); err != nil {
		return candidate, err
	}
	temporaryPath, err := initializer.createTemporaryDatabasePath(candidate.Path)
	if err != nil {
		return candidate, err
	}
	installed := false
	defer func() {
		if !installed {
			removeOwnedInitFiles(temporaryPath)
		}
	}()
	if err := initializer.prepareTemporaryDatabase(temporaryPath); err != nil {
		return candidate, err
	}
	if err := initializer.installTemporaryDatabase(candidate.Path, temporaryPath); err != nil {
		return candidate, err
	}
	installed = true
	result := initializer.inspector.Inspect(candidate.Path)
	if result.Kind != domainworkspace.CandidateKindMeetSieve || result.SchemaState != domainworkspace.SchemaStateCurrent {
		return candidate, apperr.Biz(apperr.CodeWorkspaceDatabaseInvalid, apperr.WithOp("workspace.initialize.verify_install"))
	}
	return result, nil
}

// validateDependencies 防止初始化在缺少 finalizer 或操作 ID 边界时生成不可诊断文件。
func (initializer *Initializer) validateDependencies() error {
	if initializer == nil || initializer.inspector == nil || initializer.finalizer == nil || initializer.operationID == nil {
		return fmt.Errorf("工作目录初始化依赖不完整")
	}
	return nil
}

// initializationEligibilityError 只允许 missing/empty；有效非空目录由 UseWorkspace 直接接入。
func initializationEligibilityError(candidate domainworkspace.WorkspaceCandidate) error {
	if candidate.Kind == domainworkspace.CandidateKindMissing || candidate.Kind == domainworkspace.CandidateKindEmpty {
		return nil
	}
	if candidate.Kind == domainworkspace.CandidateKindInvalid {
		return candidateAppError(candidate.Reason)
	}
	return apperr.Biz(apperr.CodeWorkspaceNotEmpty, apperr.WithOp("workspace.initialize.eligibility"))
}

// ensureWorkspaceDirectories 仅创建本方案登记目录，不更改既有目录的权限或未知内容。
func (initializer *Initializer) ensureWorkspaceDirectories(candidate domainworkspace.WorkspaceCandidate) error {
	if candidate.Kind == domainworkspace.CandidateKindMissing {
		if err := os.MkdirAll(candidate.Path, workspaceDirectoryPermission); err != nil {
			return apperr.Dependency(apperr.CodeWorkspaceNotWritable, err, apperr.WithOp("workspace.initialize.root"))
		}
		if err := os.Chmod(candidate.Path, workspaceDirectoryPermission); err != nil {
			return apperr.Dependency(apperr.CodeWorkspaceNotWritable, err, apperr.WithOp("workspace.initialize.root_permission"))
		}
	}
	for _, directory := range []string{
		filepath.Join(candidate.Path, "data"),
		filepath.Join(candidate.Path, "data", "backups"),
		filepath.Join(candidate.Path, "data", "voice-samples"),
		filepath.Join(candidate.Path, "meetings"),
	} {
		if err := createRegisteredDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

// createRegisteredDirectory 创建不存在的方案登记目录，存在目录保持用户原有权限不变。
func createRegisteredDirectory(path string) error {
	if err := os.Mkdir(path, workspaceDirectoryPermission); err != nil && !os.IsExist(err) {
		return apperr.Dependency(apperr.CodeWorkspaceNotWritable, err, apperr.WithOp("workspace.initialize.directory"))
	}
	return nil
}

// createTemporaryDatabasePath 预留同目录临时文件名，确保不会覆盖已有文件。
func (initializer *Initializer) createTemporaryDatabasePath(workspacePath string) (string, error) {
	operationID := initializer.operationID.New()
	if strings.TrimSpace(operationID) == "" || strings.ContainsAny(operationID, `/\\`) {
		return "", fmt.Errorf("生成初始化 operation ID 失败")
	}
	temporaryPath := filepath.Join(workspacePath, "data", ".meetings-init-"+operationID+".db")
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, databaseFilePermission)
	if err != nil {
		return "", apperr.Dependency(apperr.CodeWorkspaceNotWritable, err, apperr.WithOp("workspace.initialize.temp"))
	}
	if err := file.Close(); err != nil {
		removeOwnedInitFiles(temporaryPath)
		return "", apperr.Dependency(apperr.CodeWorkspaceNotWritable, err, apperr.WithOp("workspace.initialize.temp_close"))
	}
	return temporaryPath, nil
}

// prepareTemporaryDatabase 在临时数据库中执行 migration、finalizer 与重新打开验证。
func (initializer *Initializer) prepareTemporaryDatabase(temporaryPath string) error {
	if err := database.Migrate(temporaryPath); err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.migrate"))
	}
	db, err := sql.Open("sqlite3", temporaryPath)
	if err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.finalizer_open"))
	}
	if _, err := initializer.finalizer.Finalize(db, migration.FinalizationTargetNewDatabase); err != nil {
		_ = db.Close()
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.finalize"))
	}
	if err := db.Close(); err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.finalizer_close"))
	}
	if err := verifyTemporaryDatabase(temporaryPath); err != nil {
		return err
	}
	return nil
}

// verifyTemporaryDatabase 重新打开临时库，确认完整性、外键与 typed identity 均已成立。
func verifyTemporaryDatabase(path string) error {
	db, err := database.Open(path)
	if err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.verify_open"))
	}
	defer database.Close(db)
	if _, err := database.CheckSQLite(db); err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.verify_handle"))
	}
	if _, err := database.ReadTypedIdentity(sqlDB); err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.verify_identity"))
	}
	return nil
}

// installTemporaryDatabase 在确认目标不存在后原子替换为正式 meetings.db。
func (initializer *Initializer) installTemporaryDatabase(workspacePath string, temporaryPath string) error {
	targetPath := filepath.Join(workspacePath, "data", "meetings.db")
	if _, err := os.Lstat(targetPath); err == nil {
		return apperr.Biz(apperr.CodeWorkspaceNotEmpty, apperr.WithOp("workspace.initialize.target_exists"))
	} else if !os.IsNotExist(err) {
		return apperr.Dependency(apperr.CodeWorkspaceNotWritable, err, apperr.WithOp("workspace.initialize.target_check"))
	}
	if err := os.Rename(temporaryPath, targetPath); err != nil {
		return apperr.Dependency(apperr.CodeDatabaseMigrationFailed, err, apperr.WithOp("workspace.initialize.install"))
	}
	if err := os.Chmod(targetPath, databaseFilePermission); err != nil {
		return apperr.Dependency(apperr.CodeWorkspaceNotWritable, err, apperr.WithOp("workspace.initialize.database_permission"))
	}
	return nil
}

// removeOwnedInitFiles 只移除当前操作精确创建的临时 SQLite 文件及其 sidecar。
func removeOwnedInitFiles(temporaryPath string) {
	base := filepath.Base(temporaryPath)
	if !strings.HasPrefix(base, ".meetings-init-") || !strings.HasSuffix(base, ".db") {
		return
	}
	for _, path := range []string{temporaryPath, temporaryPath + "-wal", temporaryPath + "-shm"} {
		_ = os.Remove(path)
	}
}

// candidateAppError 将 Inspector 的稳定拒绝原因恢复为统一 AppError。
func candidateAppError(reason domainworkspace.CandidateReason) error {
	switch reason {
	case domainworkspace.CandidateReasonInstallPathForbidden:
		return apperr.Biz(apperr.CodeWorkspaceInstallPathForbidden, apperr.WithOp("workspace.initialize.candidate"))
	case domainworkspace.CandidateReasonUnsupportedVolume:
		return apperr.Biz(apperr.CodeWorkspaceUnsupportedVolume, apperr.WithOp("workspace.initialize.candidate"))
	case domainworkspace.CandidateReasonNotWritable:
		return apperr.Biz(apperr.CodeWorkspaceNotWritable, apperr.WithOp("workspace.initialize.candidate"))
	case domainworkspace.CandidateReasonDatabaseMissing, domainworkspace.CandidateReasonNotEmpty:
		return apperr.Biz(apperr.CodeWorkspaceNotEmpty, apperr.WithOp("workspace.initialize.candidate"))
	default:
		return apperr.Biz(apperr.CodeWorkspaceDatabaseInvalid, apperr.WithOp("workspace.initialize.candidate"))
	}
}
