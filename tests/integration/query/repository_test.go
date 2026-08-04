package query_test

import (
	"context"
	"path/filepath"
	"testing"

	querydomain "meet-sieve/internal/domain/query"
	"meet-sieve/internal/infra/database"
	queryrepository "meet-sieve/internal/repository/query"

	"gorm.io/gorm"
)

// TestRepository_ListMeetingsUsesStableCursorAndEscapedSearch 验证稳定排序、前后游标与 LIKE 转义。
func TestRepository_ListMeetingsUsesStableCursorAndEscapedSearch(t *testing.T) {
	db := openQueryDatabase(t)
	insertMeeting(t, db, "11111111-1111-4111-8111-111111111111", "M-03", "100% 周会", 3000, "saved")
	insertMeeting(t, db, "22222222-2222-4222-8222-222222222222", "M-02", "普通周会", 2000, "saved")
	insertMeeting(t, db, "33333333-3333-4333-8333-333333333333", "M-01", "历史会议", 1000, "saved")
	insertParticipant(t, db, "44444444-4444-4444-8444-444444444444", "33333333-3333-4333-8333-333333333333", "研发_一组")

	repository := queryrepository.NewRepository(db)
	first, err := repository.ListMeetings(context.Background(), queryrepository.ListInput{Limit: 2})
	if err != nil {
		t.Fatalf("读取第一页失败：%v", err)
	}
	if len(first.Items) != 2 || !first.HasMore || first.Items[0].MeetingNo != "M-03" || first.Items[1].MeetingNo != "M-02" {
		t.Fatalf("第一页排序或边界错误：%+v", first)
	}

	next, err := repository.ListMeetings(context.Background(), queryrepository.ListInput{
		Limit:  2,
		Cursor: &querydomain.Cursor{Version: 1, Direction: querydomain.DirectionNext, StartedAt: 2000, MeetingNo: "M-02"},
	})
	if err != nil || len(next.Items) != 1 || next.Items[0].MeetingNo != "M-01" {
		t.Fatalf("下一页错误：page=%+v err=%v", next, err)
	}

	percent, err := repository.ListMeetings(context.Background(), queryrepository.ListInput{Limit: 50, Filter: querydomain.MeetingFilter{Search: "%"}})
	if err != nil || len(percent.Items) != 1 || percent.Items[0].MeetingNo != "M-03" {
		t.Fatalf("百分号必须按文字搜索：page=%+v err=%v", percent, err)
	}
	underscore, err := repository.ListMeetings(context.Background(), queryrepository.ListInput{Limit: 50, Filter: querydomain.MeetingFilter{Search: "_"}})
	if err != nil || len(underscore.Items) != 1 || underscore.Items[0].MeetingNo != "M-01" {
		t.Fatalf("下划线必须按文字搜索参与人：page=%+v err=%v", underscore, err)
	}
}

// TestRepository_ListMeetingsProjectsPriorityWithoutNPlusOne 验证删除和正交状态被批量投影。
func TestRepository_ListMeetingsProjectsPriorityWithoutNPlusOne(t *testing.T) {
	db := openQueryDatabase(t)
	insertMeeting(t, db, "11111111-1111-4111-8111-111111111111", "M-01", "待删除", 1000, "saved")
	if err := db.Exec(`INSERT INTO deletion_jobs (
		id, meeting_id, kind, state, target_manifest_json, attempt_count, created_at, updated_at
	) VALUES ('99999999-9999-4999-8999-999999999999', '11111111-1111-4111-8111-111111111111',
		'meeting', 'failed', '{}', 1, 0, 0)`).Error; err != nil {
		t.Fatalf("写入删除任务失败：%v", err)
	}
	page, err := queryrepository.NewRepository(db).ListMeetings(context.Background(), queryrepository.ListInput{Limit: 50})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("读取删除会议失败：page=%+v err=%v", page, err)
	}
	if page.Items[0].HighestStatus != querydomain.StatusDeleting {
		t.Fatalf("删除状态优先级错误：%+v", page.Items[0])
	}
}

// TestRepository_ListMeetingsLoadsRealPendingGapTarget 验证列表批量投影真实未解决缺口 ID。
func TestRepository_ListMeetingsLoadsRealPendingGapTarget(t *testing.T) {
	db := openQueryDatabase(t)
	const meetingID = "55555555-5555-4555-8555-555555555555"
	const eventID = "66666666-6666-4666-8666-666666666666"
	const gapID = "77777777-7777-4777-8777-777777777777"
	insertMeeting(t, db, meetingID, "M-GAP", "缺口会议", 1000, "saved")
	if err := db.Exec("UPDATE meetings SET gap_state = 'conflict', minute_state = 'not_generated' WHERE id = ?", meetingID).Error; err != nil {
		t.Fatalf("准备缺口会议失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO meeting_events (
		id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at
	) VALUES (?, ?, 1, 'asr.gap', 1001, 'system', 'asr_gap', ?, 1001, 1001)`, eventID, meetingID, gapID).Error; err != nil {
		t.Fatalf("写入缺口事件失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO asr_gaps (
		id, meeting_id, event_id, start_sample, end_sample, reason, origin_key, state, attempt_count, created_at, updated_at
	) VALUES (?, ?, ?, 0, 16000, 'disconnected', ?, 'conflict', 1, 1001, 1001)`,
		gapID, meetingID, eventID, "0000000000000000000000000000000000000000000000000000000000000000").Error; err != nil {
		t.Fatalf("写入缺口失败：%v", err)
	}

	page, err := queryrepository.NewRepository(db).ListMeetings(context.Background(), queryrepository.ListInput{Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("读取缺口会议失败：page=%+v err=%v", page, err)
	}
	if page.Items[0].HighestStatus != querydomain.StatusGapConflict || page.Items[0].PendingGapID != gapID {
		t.Fatalf("缺口状态或目标错误：%+v", page.Items[0])
	}
}

// openQueryDatabase 创建隔离的最新 SQLite 查询测试库。
func openQueryDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "query.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移测试库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}

// insertMeeting 写入可进入历史查询的最小会议。
func insertMeeting(t *testing.T, db *gorm.DB, id string, number string, subject string, startedAt int64, localSave string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, 'Asia/Shanghai', ?, ?, 'ended', ?, 'stopped', 'none', 'available', 'confirmed', 'stopped', ?, ?)`,
		id, number, subject, "meetings/"+number, startedAt, startedAt+1, localSave, startedAt, startedAt+1).Error; err != nil {
		t.Fatalf("写入会议失败：%v", err)
	}
}

// insertParticipant 写入临时参会者快照。
func insertParticipant(t *testing.T, db *gorm.DB, id string, meetingID string, name string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO meeting_participants (
		id, meeting_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at
	) VALUES (?, ?, 'temporary', ?, 0, 0, 0)`, id, meetingID, name).Error; err != nil {
		t.Fatalf("写入参会者失败：%v", err)
	}
}
