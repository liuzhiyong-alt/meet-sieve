package migration

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"

	_ "github.com/mattn/go-sqlite3"
)

// BackupRequest 描述一次升级前备份的固定输入，不包含 locator 或历史目录信息。
type BackupRequest struct {
	DatabasePath    string
	BackupDirectory string
	OperationID     string
	TargetVersion   uint
}

// BackupResult 返回已验证备份对的路径与 manifest；无待升级时 Created 为 false。
type BackupResult struct {
	Created      bool
	DatabasePath string
	ManifestPath string
	Manifest     BackupManifest
}

// BackupService 在执行 migration 前创建并验证 SQLite Online Backup 快照。
type BackupService struct {
	clock clock.Clock
}

// NewBackupService 创建升级备份服务。
func NewBackupService(currentClock clock.Clock) *BackupService {
	return &BackupService{clock: currentClock}
}

// BackupIfRequired 仅在 source version 低于目标版本时创建可验证的备份文件与 manifest 对。
func (service *BackupService) BackupIfRequired(request BackupRequest) (BackupResult, error) {
	if err := validateBackupRequest(service, request); err != nil {
		return BackupResult{}, err
	}
	source, err := readBackupSource(request.DatabasePath, request.TargetVersion)
	if err != nil {
		return BackupResult{}, err
	}
	if source.version >= request.TargetVersion {
		return BackupResult{}, nil
	}
	if err := os.MkdirAll(request.BackupDirectory, 0o700); err != nil {
		return BackupResult{}, fmt.Errorf("创建备份目录失败: %w", err)
	}
	paths := buildBackupPaths(request, source.version, service.clock.Now())
	if err := database.OnlineBackup(request.DatabasePath, paths.temporaryDatabase); err != nil {
		return BackupResult{}, fmt.Errorf("创建 SQLite Online Backup 失败: %w", err)
	}
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(paths.temporaryDatabase)
		}
	}()
	manifest, err := verifyBackupSnapshot(paths.temporaryDatabase, source, request, paths.databaseFile, service.clock.Now())
	if err != nil {
		return BackupResult{}, err
	}
	if err := os.Rename(paths.temporaryDatabase, paths.database); err != nil {
		return BackupResult{}, fmt.Errorf("安装备份数据库失败: %w", err)
	}
	installed = true
	data, err := MarshalBackupManifest(manifest)
	if err != nil {
		return BackupResult{}, err
	}
	if err := filesystem.WriteAtomic(paths.manifest, data, 0o600); err != nil {
		return BackupResult{}, fmt.Errorf("保存备份 manifest 失败: %w", err)
	}
	return BackupResult{Created: true, DatabasePath: paths.database, ManifestPath: paths.manifest, Manifest: manifest}, nil
}

// validateBackupRequest 拒绝空路径、无目标版本和不安全 operation ID。
func validateBackupRequest(service *BackupService, request BackupRequest) error {
	if service == nil || service.clock == nil {
		return fmt.Errorf("备份服务依赖不完整")
	}
	if strings.TrimSpace(request.DatabasePath) == "" || strings.TrimSpace(request.BackupDirectory) == "" || request.TargetVersion == 0 {
		return fmt.Errorf("备份请求不完整")
	}
	if strings.TrimSpace(request.OperationID) == "" || filepath.Base(request.OperationID) != request.OperationID {
		return fmt.Errorf("备份 operation ID 不合法")
	}
	return nil
}

// backupSource 是经版本和身份验证后的备份来源。
type backupSource struct {
	version    uint
	sourceKind BackupSourceKind
	databaseID *string
}

// readBackupSource 优先读取 version；仅在确认要升级时验证 v1 指纹或 typed identity。
func readBackupSource(path string, targetVersion uint) (backupSource, error) {
	db, err := sql.Open("sqlite3", buildReadOnlySQLiteDSN(path))
	if err != nil {
		return backupSource{}, fmt.Errorf("只读打开备份源失败: %w", err)
	}
	defer db.Close()
	version, err := database.ReadSchemaVersion(db)
	if err != nil || version.Dirty {
		return backupSource{}, fmt.Errorf("备份源版本不可用")
	}
	if version.Version >= targetVersion {
		return backupSource{version: version.Version}, nil
	}
	if version.Version == 1 {
		if err := database.ValidateStep0Fingerprint(db, version); err != nil {
			return backupSource{}, fmt.Errorf("备份源 Step 0 指纹无效: %w", err)
		}
		return backupSource{version: version.Version, sourceKind: BackupSourceFoundationV1}, nil
	}
	identity, err := database.ReadTypedIdentity(db)
	if err != nil {
		return backupSource{}, fmt.Errorf("备份源身份无效: %w", err)
	}
	databaseID := identity.DatabaseID
	return backupSource{version: version.Version, sourceKind: BackupSourceMeetSieve, databaseID: &databaseID}, nil
}

// buildReadOnlySQLiteDSN 对用户可选工作目录做 URL 编码，避免 #、空格等字符截断 SQLite 文件路径。
func buildReadOnlySQLiteDSN(path string) string {
	query := url.Values{}
	query.Set("mode", "ro")
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

// backupPaths 保存一个备份对的临时与正式路径。
type backupPaths struct {
	temporaryDatabase string
	database          string
	manifest          string
	databaseFile      string
}

// buildBackupPaths 使用 UTC、from/to 和 operation ID 形成稳定且不含用户路径的备份文件名。
func buildBackupPaths(request BackupRequest, sourceVersion uint, now time.Time) backupPaths {
	timestamp := now.UTC().Format("20060102T150405Z")
	file := fmt.Sprintf("meetings-v%d-to-v%d-%s-%s.db", sourceVersion, request.TargetVersion, timestamp, request.OperationID)
	return backupPaths{
		temporaryDatabase: filepath.Join(request.BackupDirectory, "."+file+".tmp"),
		database:          filepath.Join(request.BackupDirectory, file),
		manifest:          filepath.Join(request.BackupDirectory, strings.TrimSuffix(file, ".db")+".json"),
		databaseFile:      file,
	}
}

// verifyBackupSnapshot fsync 并复验版本、身份、SQLite 完整性、大小与 SHA-256。
func verifyBackupSnapshot(path string, source backupSource, request BackupRequest, databaseFile string, now time.Time) (BackupManifest, error) {
	if err := syncDatabaseFile(path); err != nil {
		return BackupManifest{}, err
	}
	if err := verifySnapshotDatabase(path, source); err != nil {
		return BackupManifest{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("读取备份文件信息失败: %w", err)
	}
	digest, err := filesystem.SHA256File(path)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("计算备份文件哈希失败: %w", err)
	}
	return BackupManifest{
		SchemaVersion: backupManifestSchemaVersion,
		OperationID:   request.OperationID,
		CreatedAtUTC:  now.UTC().Format(time.RFC3339),
		SourceKind:    source.sourceKind,
		DatabaseID:    source.databaseID,
		FromVersion:   source.version,
		ToVersion:     request.TargetVersion,
		DatabaseFile:  databaseFile,
		SizeBytes:     info.Size(),
		SHA256:        digest,
	}, nil
}

// syncDatabaseFile 在 manifest 被写入前持久化备份数据库文件。
func syncDatabaseFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开备份文件失败: %w", err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步备份文件失败: %w", err)
	}
	return nil
}

// verifySnapshotDatabase 验证快照保留来源版本/身份，且可通过 SQLite 三项检查。
func verifySnapshotDatabase(path string, source backupSource) error {
	db, err := database.Open(path)
	if err != nil {
		return fmt.Errorf("打开备份快照失败: %w", err)
	}
	defer database.Close(db)
	if _, err := database.CheckSQLite(db); err != nil {
		return fmt.Errorf("校验备份快照失败: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取备份快照连接失败: %w", err)
	}
	version, err := database.ReadSchemaVersion(sqlDB)
	if err != nil || version.Version != source.version || version.Dirty {
		return fmt.Errorf("备份快照版本不一致")
	}
	if source.sourceKind == BackupSourceFoundationV1 {
		return database.ValidateStep0Fingerprint(sqlDB, version)
	}
	identity, err := database.ReadTypedIdentity(sqlDB)
	if err != nil || source.databaseID == nil || identity.DatabaseID != *source.databaseID {
		return fmt.Errorf("备份快照身份不一致")
	}
	return nil
}

// ParseBackupManifestFile 读取并严格解析一个已经配对的 manifest 文件。
func ParseBackupManifestFile(path string) (BackupManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("读取备份 manifest 失败: %w", err)
	}
	return ParseBackupManifest(data)
}
