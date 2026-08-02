package migration_test

import (
	"testing"
	"time"

	"meet-sieve/internal/service/migration"
)

// TestBackupManifest_ParsesOnlyCompleteMinimalV1 验证备份 manifest 只接受完整、无敏感字段的 v1 格式。
func TestBackupManifest_ParsesOnlyCompleteMinimalV1(t *testing.T) {
	databaseID := "22222222-2222-4222-8222-222222222222"
	manifest := migration.BackupManifest{
		SchemaVersion: 1,
		OperationID:   "11111111-1111-4111-8111-111111111111",
		CreatedAtUTC:  time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		SourceKind:    migration.BackupSourceMeetSieve,
		DatabaseID:    &databaseID,
		FromVersion:   1,
		ToVersion:     2,
		DatabaseFile:  "meetings-v1-to-v2-20260731T120000Z-op.db",
		SizeBytes:     1024,
		SHA256:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	data, err := migration.MarshalBackupManifest(manifest)
	if err != nil {
		t.Fatalf("编码合法 manifest 失败：%v", err)
	}
	parsed, err := migration.ParseBackupManifest(data)
	if err != nil || parsed.DatabaseID == nil || *parsed.DatabaseID != databaseID || parsed.OperationID != manifest.OperationID || parsed.SourceKind != manifest.SourceKind {
		t.Fatalf("manifest 往返不正确：parsed=%+v err=%v", parsed, err)
	}
	if _, err := migration.ParseBackupManifest(append(data, []byte(`,"credential":"secret"}`)...)); err == nil {
		t.Fatal("未知或敏感字段必须拒绝")
	}
}

// TestBackupManifest_RejectsInvalidSourceIdentity 验证 v1 foundation 与已安装数据库的来源身份不可混淆。
func TestBackupManifest_RejectsInvalidSourceIdentity(t *testing.T) {
	base := migration.BackupManifest{
		SchemaVersion: 1, OperationID: "op", CreatedAtUTC: "2026-07-31T12:00:00Z", SourceKind: migration.BackupSourceFoundationV1,
		FromVersion: 1, ToVersion: 2, DatabaseFile: "backup.db", SizeBytes: 1,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if _, err := migration.MarshalBackupManifest(base); err != nil {
		t.Fatalf("合法 foundation manifest 被拒绝：%v", err)
	}
	invalid := base
	databaseID := "22222222-2222-4222-8222-222222222222"
	invalid.DatabaseID = &databaseID
	if _, err := migration.MarshalBackupManifest(invalid); err == nil {
		t.Fatal("foundation manifest 不得携带 database_id")
	}
}
