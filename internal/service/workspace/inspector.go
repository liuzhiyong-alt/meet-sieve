package workspace

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"

	_ "github.com/mattn/go-sqlite3"
)

const recommendedWorkspaceFreeBytes = uint64(10 * 1024 * 1024 * 1024)

// DiskSpaceReader 定义读取候选卷可用空间的系统边界。
type DiskSpaceReader func(path string) (uint64, error)

// Inspector 只对候选路径执行固定范围的轻量工作目录检查。
type Inspector struct {
	pathPolicy *PathPolicy
	diskSpace  DiskSpaceReader
}

// NewInspector 创建候选目录检查器；未注入磁盘读取器时使用当前平台真实实现。
func NewInspector(pathPolicy *PathPolicy, diskSpace DiskSpaceReader) *Inspector {
	if diskSpace == nil {
		diskSpace = filesystem.AvailableBytes
	}
	return &Inspector{pathPolicy: pathPolicy, diskSpace: diskSpace}
}

// Inspect 分类候选目录，不创建工作目录、不遍历会议文件，也不修改 SQLite。
func (inspector *Inspector) Inspect(input string) domainworkspace.WorkspaceCandidate {
	canonical, candidate := inspector.validatePath(input)
	if candidate.Kind == domainworkspace.CandidateKindInvalid {
		return candidate
	}
	if info, err := os.Stat(canonical.String()); err != nil {
		if os.IsNotExist(err) {
			return inspector.inspectMissing(canonical.String())
		}
		return invalidCandidate(canonical.String(), domainworkspace.CandidateReasonNotWritable)
	} else if !info.IsDir() {
		return invalidCandidate(canonical.String(), domainworkspace.CandidateReasonInvalidPath)
	}
	return inspector.inspectExistingDirectory(canonical.String())
}

// validatePath 将路径策略错误映射为可供 UI 显示的稳定候选原因。
func (inspector *Inspector) validatePath(input string) (filesystem.CanonicalPath, domainworkspace.WorkspaceCandidate) {
	if inspector == nil || inspector.pathPolicy == nil {
		return filesystem.CanonicalPath{}, invalidCandidate("", domainworkspace.CandidateReasonInvalidPath)
	}
	canonical, err := inspector.pathPolicy.ValidateCandidate(input)
	if err != nil {
		return filesystem.CanonicalPath{}, invalidCandidate("", candidateReasonFromError(err))
	}
	return canonical, domainworkspace.WorkspaceCandidate{Path: canonical.String()}
}

// inspectMissing 确认最近存在父目录可写，但绝不创建目标目录。
func (inspector *Inspector) inspectMissing(path string) domainworkspace.WorkspaceCandidate {
	parent, err := nearestExistingDirectory(path)
	if err != nil || filesystem.ProbeWritable(parent) != nil {
		return invalidCandidate(path, domainworkspace.CandidateReasonNotWritable)
	}
	return inspector.withDiskWarning(domainworkspace.WorkspaceCandidate{
		Path:        path,
		Kind:        domainworkspace.CandidateKindMissing,
		Reason:      domainworkspace.CandidateReasonNone,
		SchemaState: domainworkspace.SchemaStateNone,
		Writable:    true,
		LocalVolume: true,
	})
}

// inspectExistingDirectory 区分可初始化目录与只接受固定 data/meetings.db 的业务非空目录。
func (inspector *Inspector) inspectExistingDirectory(path string) domainworkspace.WorkspaceCandidate {
	if err := filesystem.ProbeWritable(path); err != nil {
		return invalidCandidate(path, domainworkspace.CandidateReasonNotWritable)
	}
	empty, err := isDirectoryEmptyForInitialization(path)
	if err != nil {
		return invalidCandidate(path, domainworkspace.CandidateReasonNotWritable)
	}
	if empty {
		return inspector.withDiskWarning(domainworkspace.WorkspaceCandidate{
			Path: path, Kind: domainworkspace.CandidateKindEmpty, Reason: domainworkspace.CandidateReasonNone, SchemaState: domainworkspace.SchemaStateNone, Writable: true, LocalVolume: true,
		})
	}
	databasePath := filepath.Join(path, "data", "meetings.db")
	info, err := os.Stat(databasePath)
	if err != nil || info.IsDir() {
		return invalidCandidate(path, domainworkspace.CandidateReasonDatabaseMissing)
	}
	return inspector.inspectMeetSieveDatabase(path, databasePath)
}

// inspectMeetSieveDatabase 只读检查固定数据库的版本、Step 0 指纹或 typed identity。
func (inspector *Inspector) inspectMeetSieveDatabase(workspacePath string, databasePath string) domainworkspace.WorkspaceCandidate {
	db, err := openReadOnlySQLite(databasePath)
	if err != nil {
		return invalidCandidate(workspacePath, domainworkspace.CandidateReasonDatabaseInvalid)
	}
	defer db.Close()
	version, err := database.ReadSchemaVersion(db)
	if err != nil || version.Dirty {
		return invalidCandidate(workspacePath, domainworkspace.CandidateReasonDatabaseInvalid)
	}
	if version.Version > database.CurrentSchemaVersion {
		return domainworkspace.WorkspaceCandidate{Path: workspacePath, Kind: domainworkspace.CandidateKindInvalid, Reason: domainworkspace.CandidateReasonSchemaNewer, SchemaState: domainworkspace.SchemaStateNewer}
	}
	if version.Version == 1 {
		if err := database.ValidateStep0Fingerprint(db, version); err != nil {
			return invalidCandidate(workspacePath, domainworkspace.CandidateReasonDatabaseInvalid)
		}
		return inspector.withDiskWarning(domainworkspace.WorkspaceCandidate{Path: workspacePath, Kind: domainworkspace.CandidateKindMeetSieve, Reason: domainworkspace.CandidateReasonNone, SchemaState: domainworkspace.SchemaStateUpgradeRequired, Writable: true, LocalVolume: true})
	}
	identity, err := database.ReadTypedIdentity(db)
	if err != nil {
		return invalidCandidate(workspacePath, domainworkspace.CandidateReasonDatabaseInvalid)
	}
	if version.Version < database.CurrentSchemaVersion {
		return inspector.withDiskWarning(domainworkspace.WorkspaceCandidate{
			Path: workspacePath, Kind: domainworkspace.CandidateKindMeetSieve, Reason: domainworkspace.CandidateReasonNone,
			SchemaState: domainworkspace.SchemaStateUpgradeRequired, DatabaseID: identity.DatabaseID, Writable: true, LocalVolume: true,
		})
	}
	return inspector.withDiskWarning(domainworkspace.WorkspaceCandidate{Path: workspacePath, Kind: domainworkspace.CandidateKindMeetSieve, Reason: domainworkspace.CandidateReasonNone, SchemaState: domainworkspace.SchemaStateCurrent, DatabaseID: identity.DatabaseID, Writable: true, LocalVolume: true})
}

// withDiskWarning 仅在可读取磁盘空间且低于建议值时增加非阻断提示。
func (inspector *Inspector) withDiskWarning(candidate domainworkspace.WorkspaceCandidate) domainworkspace.WorkspaceCandidate {
	if inspector.diskSpace == nil {
		return candidate
	}
	freeBytes, err := inspector.diskSpace(candidate.Path)
	if err == nil {
		candidate.FreeBytes = freeBytes
		if freeBytes < recommendedWorkspaceFreeBytes {
			candidate.Warnings = []domainworkspace.CandidateWarning{domainworkspace.CandidateWarningLowDiskSpace}
		}
	}
	return candidate
}

// openReadOnlySQLite 强制使用 mode=ro，避免检查过程创建 WAL、SHM 或修改数据库字节。
func openReadOnlySQLite(path string) (*sql.DB, error) {
	query := url.Values{}
	query.Set("mode", "ro")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("只读打开 SQLite 失败: %w", err)
	}
	return db, nil
}

// nearestExistingDirectory 向上查找候选路径对应的最近存在目录。
func nearestExistingDirectory(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		info, err := os.Stat(current)
		if err == nil {
			if info.IsDir() {
				return current, nil
			}
			return "", fmt.Errorf("最近存在路径不是目录")
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("找不到存在的父目录")
		}
		current = parent
	}
}

// isDirectoryEmptyForInitialization 忽略精确登记的系统元数据，其他任意目录项仍视为非空。
func isDirectoryEmptyForInitialization(path string) (bool, error) {
	directory, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer directory.Close()
	for {
		names, readErr := directory.Readdirnames(1)
		if errors.Is(readErr, io.EOF) {
			return true, nil
		}
		if readErr != nil {
			return false, readErr
		}
		if len(names) != 1 || !isRegisteredSystemMetadata(names[0]) {
			return false, nil
		}
	}
}

// isRegisteredSystemMetadata 只登记操作系统可能自动创建且无需由 MeetSieve 管理的精确文件名。
func isRegisteredSystemMetadata(name string) bool {
	switch name {
	case ".DS_Store", "Thumbs.db":
		return true
	default:
		return false
	}
}

// candidateReasonFromError 将路径策略的稳定 AppError 转换为对应领域原因。
func candidateReasonFromError(err error) domainworkspace.CandidateReason {
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		return domainworkspace.CandidateReasonInvalidPath
	}
	switch appErr.ErrorCode {
	case apperr.CodeWorkspaceInstallPathForbidden.ErrorCode:
		return domainworkspace.CandidateReasonInstallPathForbidden
	case apperr.CodeWorkspaceUnsupportedVolume.ErrorCode:
		return domainworkspace.CandidateReasonUnsupportedVolume
	default:
		return domainworkspace.CandidateReasonInvalidPath
	}
}

// invalidCandidate 构造无可用 schema 的阻断候选结果。
func invalidCandidate(path string, reason domainworkspace.CandidateReason) domainworkspace.WorkspaceCandidate {
	return domainworkspace.WorkspaceCandidate{Path: path, Kind: domainworkspace.CandidateKindInvalid, Reason: reason, SchemaState: domainworkspace.SchemaStateNone}
}
