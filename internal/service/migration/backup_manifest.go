package migration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const backupManifestSchemaVersion = 1

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BackupSourceKind 表示备份源处于 Step 0 基础结构还是已完成身份的 MeetSieve 数据库。
type BackupSourceKind string

const (
	// BackupSourceFoundationV1 表示可升级的 Step 0 foundation 数据库。
	BackupSourceFoundationV1 BackupSourceKind = "foundation_v1"
	// BackupSourceMeetSieve 表示具有合法 database_id 的 MeetSieve 数据库。
	BackupSourceMeetSieve BackupSourceKind = "meetsieve"
)

// BackupManifest 是与单个 SQLite 备份文件配对的最小、可验证 metadata。
type BackupManifest struct {
	SchemaVersion int              `json:"schema_version"`
	OperationID   string           `json:"operation_id"`
	CreatedAtUTC  string           `json:"created_at_utc"`
	SourceKind    BackupSourceKind `json:"source_kind"`
	DatabaseID    *string          `json:"database_id"`
	FromVersion   uint             `json:"from_version"`
	ToVersion     uint             `json:"to_version"`
	DatabaseFile  string           `json:"database_file"`
	SizeBytes     int64            `json:"size_bytes"`
	SHA256        string           `json:"sha256"`
}

// MarshalBackupManifest 校验并编码 v1 manifest，避免备份元数据夹带凭据或任意字段。
func MarshalBackupManifest(manifest BackupManifest) ([]byte, error) {
	if err := validateBackupManifest(manifest); err != nil {
		return nil, err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("编码 backup manifest 失败: %w", err)
	}
	return data, nil
}

// ParseBackupManifest 严格解析并校验 v1 manifest，不接受未知字段或尾随数据。
func ParseBackupManifest(data []byte) (BackupManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest BackupManifest
	if err := decoder.Decode(&manifest); err != nil {
		return BackupManifest{}, fmt.Errorf("解析 backup manifest 失败: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return BackupManifest{}, fmt.Errorf("backup manifest 不允许尾随内容")
		}
		return BackupManifest{}, fmt.Errorf("backup manifest 尾随内容不合法: %w", err)
	}
	if err := validateBackupManifest(manifest); err != nil {
		return BackupManifest{}, err
	}
	return manifest, nil
}

// validateBackupManifest 验证备份对、安全文件名、来源身份和完整性摘要。
func validateBackupManifest(manifest BackupManifest) error {
	if manifest.SchemaVersion != backupManifestSchemaVersion {
		return fmt.Errorf("backup manifest schema_version 不受支持")
	}
	if strings.TrimSpace(manifest.OperationID) == "" || filepath.Base(manifest.OperationID) != manifest.OperationID {
		return fmt.Errorf("backup manifest operation_id 不合法")
	}
	if _, err := time.Parse(time.RFC3339, manifest.CreatedAtUTC); err != nil {
		return fmt.Errorf("backup manifest created_at_utc 不合法: %w", err)
	}
	if manifest.FromVersion >= manifest.ToVersion {
		return fmt.Errorf("backup manifest version 范围不合法")
	}
	if filepath.Base(manifest.DatabaseFile) != manifest.DatabaseFile || !strings.HasSuffix(manifest.DatabaseFile, ".db") {
		return fmt.Errorf("backup manifest database_file 不合法")
	}
	if manifest.SizeBytes <= 0 || !sha256Pattern.MatchString(manifest.SHA256) {
		return fmt.Errorf("backup manifest 文件完整性字段不合法")
	}
	return validateBackupSourceIdentity(manifest)
}

// validateBackupSourceIdentity 区分无身份的 Step 0 与必须持有 UUID v4 的 MeetSieve 备份。
func validateBackupSourceIdentity(manifest BackupManifest) error {
	switch manifest.SourceKind {
	case BackupSourceFoundationV1:
		if manifest.DatabaseID != nil {
			return fmt.Errorf("foundation_v1 manifest 不得包含 database_id")
		}
	case BackupSourceMeetSieve:
		if manifest.DatabaseID == nil {
			return fmt.Errorf("meetsieve manifest 缺少 database_id")
		}
		parsed, err := uuid.Parse(*manifest.DatabaseID)
		if err != nil || parsed.Version() != 4 {
			return fmt.Errorf("meetsieve manifest database_id 不合法")
		}
	default:
		return fmt.Errorf("backup manifest source_kind 不合法")
	}
	return nil
}
