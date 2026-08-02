package database

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// ValidateStep0Fingerprint 仅接受可安全升级的精确 Step 0 占位数据库。
func ValidateStep0Fingerprint(db *sql.DB, version SchemaVersion) error {
	if db == nil {
		return fmt.Errorf("校验 Step 0 指纹：数据库连接不能为空")
	}
	if version.Version != 1 || version.Dirty {
		return fmt.Errorf("校验 Step 0 指纹：schema 版本必须为 clean v1")
	}
	if err := validateStep0TableSet(db); err != nil {
		return err
	}
	if err := validateSingleSchemaMigrationRow(db, version); err != nil {
		return err
	}
	if err := validateLegacyMetadataColumns(db); err != nil {
		return err
	}
	return nil
}

// validateStep0TableSet 拒绝额外用户表，避免把未知数据库猜测为 Step 0。
func validateStep0TableSet(db *sql.DB) error {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return fmt.Errorf("读取 Step 0 表集合失败：%w", err)
	}
	defer rows.Close()

	var actual []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("读取 Step 0 表名失败：%w", err)
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历 Step 0 表集合失败：%w", err)
	}
	sort.Strings(actual)
	expected := []string{"app_metadata", "schema_migrations"}
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		return fmt.Errorf("Step 0 表集合不匹配")
	}
	return nil
}

// validateSingleSchemaMigrationRow 确认 migration runner 仅保留一个 clean v1 状态。
func validateSingleSchemaMigrationRow(db *sql.DB, version SchemaVersion) error {
	var totalCount, matchingCount int
	if err := db.QueryRow("SELECT count(*), COALESCE(sum(CASE WHEN version = ? AND dirty = ? THEN 1 ELSE 0 END), 0) FROM schema_migrations", version.Version, version.Dirty).Scan(&totalCount, &matchingCount); err != nil {
		return fmt.Errorf("校验 schema_migrations 失败：%w", err)
	}
	if totalCount != 1 || matchingCount != 1 {
		return fmt.Errorf("schema_migrations 不是唯一 clean v1 状态")
	}
	return nil
}

// validateLegacyMetadataColumns 比对 Step 0 app_metadata 的列顺序、类型、空值与主键约束。
func validateLegacyMetadataColumns(db *sql.DB) error {
	type column struct {
		Name    string
		Type    string
		NotNull int
		Primary int
	}
	expected := []column{
		{Name: "key", Type: "TEXT", NotNull: 0, Primary: 1},
		{Name: "value", Type: "TEXT", NotNull: 1, Primary: 0},
		{Name: "created_at", Type: "DATETIME", NotNull: 1, Primary: 0},
		{Name: "updated_at", Type: "DATETIME", NotNull: 1, Primary: 0},
	}
	rows, err := db.Query("PRAGMA table_info(app_metadata)")
	if err != nil {
		return fmt.Errorf("读取 app_metadata 指纹失败：%w", err)
	}
	defer rows.Close()

	var actual []column
	for rows.Next() {
		var position int
		var defaultValue any
		var item column
		if err := rows.Scan(&position, &item.Name, &item.Type, &item.NotNull, &defaultValue, &item.Primary); err != nil {
			return fmt.Errorf("读取 app_metadata 列失败：%w", err)
		}
		actual = append(actual, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历 app_metadata 列失败：%w", err)
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("app_metadata 列数量不匹配")
	}
	for index, want := range expected {
		if actual[index] != want {
			return fmt.Errorf("app_metadata 指纹不匹配")
		}
	}
	return nil
}
