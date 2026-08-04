package database_test

import (
	"testing"

	"meet-sieve/internal/infra/database"
)

// TestStep9Schema_ReachesVersionTenAndAddsLifecycleIndexes 验证 Step 9 migration 与列表索引存在。
func TestStep9Schema_ReachesVersionTenAndAddsLifecycleIndexes(t *testing.T) {
	db := openMigratedDatabase(t)
	if database.CurrentSchemaVersion < 10 {
		t.Fatalf("当前 schema 版本不得低于 Step 9：got %d", database.CurrentSchemaVersion)
	}
	for _, index := range []string{"idx_meetings_started_no", "idx_meeting_participants_snapshot_meeting"} {
		var count int64
		if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?", index).Scan(&count).Error; err != nil {
			t.Fatalf("检查索引 %s 失败：%v", index, err)
		}
		if count != 1 {
			t.Fatalf("缺少 Step 9 索引：%s", index)
		}
	}
}

// TestStep9Schema_ConstrainsResourceIntegrity 验证资源完整性字段默认值和 CHECK 约束。
func TestStep9Schema_ConstrainsResourceIntegrity(t *testing.T) {
	db := openMigratedDatabase(t)
	for _, column := range []string{"integrity_state", "last_verified_at", "integrity_error_code"} {
		if !db.Migrator().HasColumn("resources", column) {
			t.Fatalf("resources 缺少 Step 9 字段：%s", column)
		}
	}

	insertValidMeeting(t, db)
	insertMeetingEvent(t, db, testEventID, 1)
	if err := db.Exec(`INSERT INTO resources (
		id, meeting_id, event_id, kind, relative_path, state, created_at, updated_at
	) VALUES (
		'91919191-9191-4919-8919-919191919191', ?, ?, 'attachment', 'attachments/test.txt', 'completed', 0, 0
	)`, testMeetingID, testEventID).Error; err != nil {
		t.Fatalf("写入测试资源失败：%v", err)
	}

	var defaultState string
	if err := db.Raw("SELECT integrity_state FROM resources LIMIT 1").Scan(&defaultState).Error; err != nil {
		t.Fatalf("读取完整性默认值失败：%v", err)
	}
	if defaultState != "unchecked" {
		t.Fatalf("完整性默认值错误：got %q", defaultState)
	}
	if err := db.Exec("UPDATE resources SET integrity_state = 'invalid'").Error; err == nil {
		t.Fatal("未知完整性状态必须被拒绝")
	}
	if err := db.Exec("UPDATE resources SET last_verified_at = -1").Error; err == nil {
		t.Fatal("负数校验时间必须被拒绝")
	}
}
