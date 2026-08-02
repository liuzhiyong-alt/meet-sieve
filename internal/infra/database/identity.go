package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/internal/domain/metadata"

	"github.com/google/uuid"
)

// ErrTypedIdentityInvalid 表示已安装数据库缺失或破坏了 Step 1 singleton 身份。
var ErrTypedIdentityInvalid = errors.New("typed database identity invalid")

// TypedIdentity 是通过完整 singleton 校验后的数据库稳定身份。
type TypedIdentity struct {
	MetadataID            string
	DatabaseID            string
	DeviceCode            string
	CreatedWithAppVersion string
	CreatedAt             int64
	UpdatedAt             int64
}

// ReadTypedIdentity 读取并验证 v2 及后续版本数据库的 metadata/settings singleton。
func ReadTypedIdentity(db *sql.DB) (TypedIdentity, error) {
	if db == nil {
		return TypedIdentity{}, newIdentityError("数据库连接不能为空")
	}
	if exists, err := tableExists(db, "app_metadata_legacy"); err != nil {
		return TypedIdentity{}, err
	} else if exists {
		return TypedIdentity{}, newIdentityError("存在 legacy metadata 表")
	}
	identity, err := readMetadataSingleton(db)
	if err != nil {
		return TypedIdentity{}, err
	}
	if err := validateSettingsSingleton(db); err != nil {
		return TypedIdentity{}, err
	}
	return identity, nil
}

// tableExists 使用 sqlite_master 检查固定内部表名是否存在。
func tableExists(db *sql.DB, table string) (bool, error) {
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count); err != nil {
		return false, fmt.Errorf("查询数据库表失败：%w", err)
	}
	return count == 1, nil
}

// readMetadataSingleton 读取且只接受一条完整的 typed metadata。
func readMetadataSingleton(db *sql.DB) (TypedIdentity, error) {
	rows, err := db.Query(`SELECT id, singleton_key, product, database_id, device_code,
		created_with_app_version, created_at, updated_at FROM app_metadata`)
	if err != nil {
		return TypedIdentity{}, newIdentityError("无法读取 app_metadata")
	}
	defer rows.Close()

	var identities []TypedIdentity
	for rows.Next() {
		var singletonKey int
		var product string
		var identity TypedIdentity
		if err := rows.Scan(&identity.MetadataID, &singletonKey, &product, &identity.DatabaseID, &identity.DeviceCode,
			&identity.CreatedWithAppVersion, &identity.CreatedAt, &identity.UpdatedAt); err != nil {
			return TypedIdentity{}, newIdentityError("app_metadata 字段不完整")
		}
		if singletonKey != 1 || product != "meet-sieve" || !isUUIDv4(identity.MetadataID) || !isUUIDv4(identity.DatabaseID) ||
			identity.CreatedAt < 0 || identity.UpdatedAt < identity.CreatedAt || strings.TrimSpace(identity.CreatedWithAppVersion) == "" {
			return TypedIdentity{}, newIdentityError("app_metadata 内容非法")
		}
		if _, err := metadata.ParseDeviceCode(identity.DeviceCode); err != nil {
			return TypedIdentity{}, newIdentityError("设备码非法")
		}
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return TypedIdentity{}, newIdentityError("遍历 app_metadata 失败")
	}
	if len(identities) != 1 {
		return TypedIdentity{}, newIdentityError("app_metadata 必须恰有一个 singleton")
	}
	return identities[0], nil
}

// validateSettingsSingleton 确认 finalizer 同时完成 settings 的最小完整性。
func validateSettingsSingleton(db *sql.DB) error {
	rows, err := db.Query("SELECT id, singleton_key, wake_word, created_at, updated_at FROM settings")
	if err != nil {
		return newIdentityError("无法读取 settings")
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id string
		var singletonKey int
		var wakeWord string
		var createdAt, updatedAt int64
		if err := rows.Scan(&id, &singletonKey, &wakeWord, &createdAt, &updatedAt); err != nil {
			return newIdentityError("settings 字段不完整")
		}
		if !isUUIDv4(id) || singletonKey != 1 || strings.TrimSpace(wakeWord) == "" || createdAt < 0 || updatedAt < createdAt {
			return newIdentityError("settings 内容非法")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return newIdentityError("遍历 settings 失败")
	}
	if count != 1 {
		return newIdentityError("settings 必须恰有一个 singleton")
	}
	return nil
}

// isUUIDv4 验证 UUID 文本可解析且版本为 v4。
func isUUIDv4(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 4
}

// newIdentityError 统一保留可供 errors.Is 识别的半初始化错误类型。
func newIdentityError(reason string) error {
	return fmt.Errorf("%w: %s", ErrTypedIdentityInvalid, reason)
}
