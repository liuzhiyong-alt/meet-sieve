package migration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"meet-sieve/internal/infra/filesystem"

	"github.com/google/uuid"
)

const migrationJournalSchemaVersion = 1

// MigrationPhase 表示 database 文件切换已经持久化到的阶段。
type MigrationPhase string

const (
	// MigrationPhasePrepared 表示 staging 已验证，原库尚未移动。
	MigrationPhasePrepared MigrationPhase = "prepared"
	// MigrationPhaseOriginalMoved 表示原库已移动至 pre-switch，staging 尚未安装。
	MigrationPhaseOriginalMoved MigrationPhase = "original_moved"
	// MigrationPhaseStagingInstalled 表示 staging 已安装，仍需重新打开验证。
	MigrationPhaseStagingInstalled MigrationPhase = "staging_installed"
)

// MigrationJournal 是仅用于同一工作目录 meetings.db 文件切换的可恢复记录。
type MigrationJournal struct {
	SchemaVersion int            `json:"schema_version"`
	OperationID   string         `json:"operation_id"`
	WorkspacePath string         `json:"workspace_path"`
	SourceVersion uint           `json:"source_version"`
	TargetVersion uint           `json:"target_version"`
	StagingFile   string         `json:"staging_file"`
	PreSwitchFile string         `json:"pre_switch_file"`
	Phase         MigrationPhase `json:"phase"`
	CreatedAtUTC  string         `json:"created_at_utc"`
}

// MarshalMigrationJournal 校验并编码 journal，禁止将任意路径或不完整恢复状态持久化。
func MarshalMigrationJournal(journal MigrationJournal) ([]byte, error) {
	if err := validateMigrationJournal(journal); err != nil {
		return nil, err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return nil, fmt.Errorf("编码 migration journal 失败: %w", err)
	}
	return data, nil
}

// ParseMigrationJournal 严格解析 journal，不接受未知字段、尾随数据或不安全文件名。
func ParseMigrationJournal(data []byte) (MigrationJournal, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var journal MigrationJournal
	if err := decoder.Decode(&journal); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return MigrationJournal{}, fmt.Errorf("migration journal 包含未知字段: %w", err)
		}
		return MigrationJournal{}, fmt.Errorf("解析 migration journal 失败: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return MigrationJournal{}, fmt.Errorf("migration journal 不允许尾随内容")
		}
		return MigrationJournal{}, fmt.Errorf("migration journal 尾随内容不合法: %w", err)
	}
	if err := validateMigrationJournal(journal); err != nil {
		return MigrationJournal{}, err
	}
	return journal, nil
}

// WriteMigrationJournal 将已校验 journal 原子写入系统应用目录，避免 crash 看到半个 JSON 文件。
func WriteMigrationJournal(path string, journal MigrationJournal) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("migration journal 路径必须为绝对路径")
	}
	data, err := MarshalMigrationJournal(journal)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建 migration journal 目录失败: %w", err)
	}
	if err := filesystem.WriteAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("原子保存 migration journal 失败: %w", err)
	}
	return nil
}

// ReadMigrationJournal 从指定系统文件读取严格格式的恢复记录。
func ReadMigrationJournal(path string) (MigrationJournal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MigrationJournal{}, fmt.Errorf("读取 migration journal 失败: %w", err)
	}
	return ParseMigrationJournal(data)
}

// validateMigrationJournal 确保恢复仅处理本次 operation 所属的同目录 staging/pre-switch 文件。
func validateMigrationJournal(journal MigrationJournal) error {
	if journal.SchemaVersion != migrationJournalSchemaVersion {
		return fmt.Errorf("migration journal schema_version 不受支持")
	}
	if !isUUIDv4OperationID(journal.OperationID) {
		return fmt.Errorf("migration journal operation_id 不合法")
	}
	if !filepath.IsAbs(journal.WorkspacePath) {
		return fmt.Errorf("migration journal workspace_path 必须为绝对路径")
	}
	if journal.SourceVersion == 0 || journal.SourceVersion >= journal.TargetVersion {
		return fmt.Errorf("migration journal version 范围不合法")
	}
	if !isOwnedSwitchFile(journal.StagingFile, ".meetings-staging-") || !isOwnedSwitchFile(journal.PreSwitchFile, ".meetings-pre-switch-") {
		return fmt.Errorf("migration journal 切换文件名不合法")
	}
	if journal.Phase != MigrationPhasePrepared && journal.Phase != MigrationPhaseOriginalMoved && journal.Phase != MigrationPhaseStagingInstalled {
		return fmt.Errorf("migration journal phase 不合法")
	}
	if _, err := time.Parse(time.RFC3339, journal.CreatedAtUTC); err != nil {
		return fmt.Errorf("migration journal created_at_utc 不合法: %w", err)
	}
	return nil
}

// isUUIDv4OperationID 约束切换 journal 的操作标识，不把任意文件名当作可删除对象。
func isUUIDv4OperationID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 4
}

// isOwnedSwitchFile 仅接受 coordinator 按固定前缀创建的同目录 SQLite 临时文件。
func isOwnedSwitchFile(file string, prefix string) bool {
	return filepath.Base(file) == file && strings.HasPrefix(file, prefix) && strings.HasSuffix(file, ".db")
}
