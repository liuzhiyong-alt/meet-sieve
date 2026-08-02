package database_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"
)

const (
	testMetadataID = "11111111-1111-4111-8111-111111111111"
	testDatabaseID = "22222222-2222-4222-8222-222222222222"
	testSettingsID = "33333333-3333-4333-8333-333333333333"
)

// TestReadTypedIdentity_AcceptsCompleteFinalizedDatabase 验证完整 metadata/settings singleton 可被读取。
func TestReadTypedIdentity_AcceptsCompleteFinalizedDatabase(t *testing.T) {
	db := createFinalizedLikeDatabase(t)
	identity, err := database.ReadTypedIdentity(db)
	if err != nil {
		t.Fatalf("读取有效数据库身份失败：%v", err)
	}
	if identity.DatabaseID != testDatabaseID || identity.DeviceCode != "AB29" {
		t.Fatalf("数据库身份不正确：%+v", identity)
	}
}

// TestReadTypedIdentity_RejectsLegacyTable 验证 version 2 与 legacy 表并存被视为半初始化。
func TestReadTypedIdentity_RejectsLegacyTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db := openSQLDatabase(t, path)
	if _, err := database.ReadTypedIdentity(db); !errors.Is(err, database.ErrTypedIdentityInvalid) {
		t.Fatalf("legacy 表必须被识别为半初始化：%v", err)
	}
}

// TestReadTypedIdentity_RejectsIncompleteOrMalformedSingleton 验证缺失、重复或格式非法 identity 不能作为已安装数据库使用。
func TestReadTypedIdentity_RejectsIncompleteOrMalformedSingleton(t *testing.T) {
	for _, test := range []struct {
		name       string
		prepareSQL string
	}{
		{name: "missing", prepareSQL: ""},
		{name: "malformed_uuid", prepareSQL: `INSERT INTO app_metadata (id, singleton_key, product, database_id, device_code, created_with_app_version, created_at, updated_at)
			VALUES ('11111111-1111-4111-8111-111111111111', 1, 'meet-sieve', '11111111-1111-1111-8111-111111111111', 'AB29', '0.1.0', 0, 0)`},
		{name: "duplicated", prepareSQL: `DROP TABLE app_metadata;
			CREATE TABLE app_metadata (id TEXT, singleton_key INTEGER, product TEXT, database_id TEXT, device_code TEXT, created_with_app_version TEXT, created_at INTEGER, updated_at INTEGER);
			INSERT INTO app_metadata VALUES ('11111111-1111-4111-8111-111111111111', 1, 'meet-sieve', '22222222-2222-4222-8222-222222222222', 'AB29', '0.1.0', 0, 0);
			INSERT INTO app_metadata VALUES ('44444444-4444-4444-8444-444444444444', 1, 'meet-sieve', '55555555-5555-4555-8555-555555555555', 'CD34', '0.1.0', 0, 0)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := createTypedSchemaWithoutLegacy(t)
			if test.prepareSQL != "" {
				if _, err := db.Exec(test.prepareSQL); err != nil {
					t.Fatalf("准备 %s 数据库失败：%v", test.name, err)
				}
			}
			if _, err := database.ReadTypedIdentity(db); !errors.Is(err, database.ErrTypedIdentityInvalid) {
				t.Fatalf("%s 必须被视为无效 identity：%v", test.name, err)
			}
		})
	}
}

// createFinalizedLikeDatabase 创建只含合法 singleton 的 Step 1 数据库。
func createFinalizedLikeDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := createTypedSchemaWithoutLegacy(t)
	statement := `INSERT INTO app_metadata (
		id, singleton_key, product, database_id, device_code, created_with_app_version, created_at, updated_at
	) VALUES (?, 1, 'meet-sieve', ?, 'AB29', '0.1.0', 0, 0);
	INSERT INTO settings (id, singleton_key, created_at, updated_at) VALUES (?, 1, 0, 0);`
	if _, err := db.Exec(statement, testMetadataID, testDatabaseID, testSettingsID); err != nil {
		t.Fatalf("写入合法 singleton 失败：%v", err)
	}
	return db
}

// createTypedSchemaWithoutLegacy 创建 migration 完成但删除 legacy 的测试数据库。
func createTypedSchemaWithoutLegacy(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "typed.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db := openSQLDatabase(t, path)
	if _, err := db.Exec("DROP TABLE app_metadata_legacy"); err != nil {
		t.Fatalf("删除测试 legacy 表失败：%v", err)
	}
	return db
}
