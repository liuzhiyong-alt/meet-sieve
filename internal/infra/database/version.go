package database

import (
	"database/sql"
	"fmt"
)

// CurrentSchemaVersion 是当前应用支持的最新 SQLite schema 版本。
const CurrentSchemaVersion uint = 6

// SchemaVersion 表示 schema_migrations 中的版本与未完成状态。
type SchemaVersion struct {
	Version uint
	Dirty   bool
}

// ReadSchemaVersion 仅读取 migration 状态，调用方必须在读取 typed metadata 前调用它。
func ReadSchemaVersion(db *sql.DB) (SchemaVersion, error) {
	if db == nil {
		return SchemaVersion{}, fmt.Errorf("读取 schema 版本：数据库连接不能为空")
	}
	var version SchemaVersion
	if err := db.QueryRow("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version.Version, &version.Dirty); err != nil {
		return SchemaVersion{}, fmt.Errorf("读取 schema_migrations 失败：%w", err)
	}
	return version, nil
}
