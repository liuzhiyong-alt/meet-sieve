package migration_test

import (
	"strings"
	"testing"

	"meet-sieve/internal/service/migration"
)

// TestMigrationJournal_StrictRoundTrip 验证切换 journal 只接受完整、可恢复的最小字段，且拒绝未知字段。
func TestMigrationJournal_StrictRoundTrip(t *testing.T) {
	journal := migration.MigrationJournal{
		SchemaVersion: 1,
		OperationID:   "11111111-1111-4111-8111-111111111111",
		WorkspacePath: "/tmp/meetings",
		SourceVersion: 1,
		TargetVersion: 2,
		StagingFile:   ".meetings-staging-operation.db",
		PreSwitchFile: ".meetings-pre-switch-operation.db",
		Phase:         migration.MigrationPhasePrepared,
		CreatedAtUTC:  "2026-07-31T12:00:00Z",
	}

	data, err := migration.MarshalMigrationJournal(journal)
	if err != nil {
		t.Fatalf("编码 journal 失败：%v", err)
	}
	parsed, err := migration.ParseMigrationJournal(data)
	if err != nil {
		t.Fatalf("解析 journal 失败：%v", err)
	}
	if parsed != journal {
		t.Fatalf("journal 往返不一致：got=%+v want=%+v", parsed, journal)
	}
	if _, err := migration.ParseMigrationJournal(append(data[:len(data)-1], []byte(",\"unknown\":true}")...)); err == nil || !strings.Contains(err.Error(), "未知") {
		t.Fatalf("未知字段必须被拒绝：%v", err)
	}
}
