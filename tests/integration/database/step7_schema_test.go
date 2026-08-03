package database_test

import (
	"testing"

	"gorm.io/gorm"
)

const (
	step7SessionID = "71717171-7171-4717-8717-717171717171"
	step7TurnID    = "72727272-7272-4727-8727-727272727272"
)

// TestStep7Schema_AllowsFailedEventAndInitialSnapshot 验证 Step 7 新事件和零游标快照可持久化。
func TestStep7Schema_AllowsFailedEventAndInitialSnapshot(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)
	insertAgentSession(t, db, step7SessionID, "available")
	insertAgentTurn(t, db, step7TurnID, step7SessionID, "completed")

	if err := db.Exec(`INSERT INTO meeting_events (
		id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at
	) VALUES (
		'73737373-7373-4737-8737-737373737373', ?, 1, 'ai.failed', 0, 'agent', 'agent_turn', ?, 0, 0
	)`, testMeetingID, step7TurnID).Error; err != nil {
		t.Fatalf("ai.failed 事件应可持久化：%v", err)
	}
	if err := db.Exec(`INSERT INTO context_snapshots (
		id, meeting_id, agent_session_id, agent_turn_id, through_seq, content_json, content_sha256, created_at, updated_at
	) VALUES (
		'74747474-7474-4747-8747-747474747474', ?, ?, ?, 0, '{}',
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 0, 0
	)`, testMeetingID, step7SessionID, step7TurnID).Error; err != nil {
		t.Fatalf("through_seq=0 的初始快照应可持久化：%v", err)
	}
}

// TestStep7Schema_EnforcesOneRollingSnapshotPerSession 验证每个会话只保留一行滚动快照。
func TestStep7Schema_EnforcesOneRollingSnapshotPerSession(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)
	insertAgentSession(t, db, step7SessionID, "available")
	insertAgentTurn(t, db, step7TurnID, step7SessionID, "completed")
	insertSnapshot(t, db, "75757575-7575-4757-8757-757575757575", step7SessionID, step7TurnID, 0)

	err := db.Exec(`INSERT INTO context_snapshots (
		id, meeting_id, agent_session_id, agent_turn_id, through_seq, content_json, content_sha256, created_at, updated_at
	) VALUES (
		'76767676-7676-4767-8767-767676767676', ?, ?, ?, 1, '{}',
		'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 1, 1
	)`, testMeetingID, step7SessionID, step7TurnID).Error
	if err == nil {
		t.Fatal("同一 agent session 的第二行快照必须被唯一约束拒绝")
	}
}

// TestStep7Schema_EnforcesActiveSessionAndTurnExclusivity 验证数据库兜底单活动会话和单活动 turn。
func TestStep7Schema_EnforcesActiveSessionAndTurnExclusivity(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)
	insertAgentSession(t, db, step7SessionID, "starting")

	if err := insertAgentSessionError(db, "77777777-7777-4777-8777-777777777777", "available"); err == nil {
		t.Fatal("同一会议的第二个活动 agent session 必须被拒绝")
	}
	insertAgentTurn(t, db, step7TurnID, step7SessionID, "pending")
	if err := insertAgentTurnError(db, "78787878-7878-4787-8787-787878787878", step7SessionID, "running"); err == nil {
		t.Fatal("同一会议的第二个活动 agent turn 必须被拒绝")
	}
}

// TestStep7Schema_RepairsTemporaryParentForeignKeys 验证升级后不存在指向迁移临时表的外键。
func TestStep7Schema_RepairsTemporaryParentForeignKeys(t *testing.T) {
	db := openMigratedDatabase(t)
	for _, table := range []string{"asr_gaps", "voice_embeddings"} {
		var count int64
		if err := db.Raw(`SELECT count(*) FROM pragma_foreign_key_list(?) WHERE "table" LIKE '%step%'`, table).Scan(&count).Error; err != nil {
			t.Fatalf("检查 %s 外键失败：%v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s 仍有指向迁移临时表的外键", table)
		}
	}
}

// insertAgentSession 写入约束测试所需的 agent session。
func insertAgentSession(t *testing.T, db *gorm.DB, id string, state string) {
	t.Helper()
	if err := insertAgentSessionError(db, id, state); err != nil {
		t.Fatalf("写入 agent session 失败：%v", err)
	}
}

// insertAgentSessionError 返回写入 agent session 的原始数据库错误。
func insertAgentSessionError(db *gorm.DB, id string, state string) error {
	return db.Exec(`INSERT INTO agent_sessions (
		id, meeting_id, provider, cwd_relative_path, state, started_at, created_at, updated_at
	) VALUES (?, ?, 'codex', 'meetings/test', ?, 0, 0, 0)`, id, testMeetingID, state).Error
}

// insertAgentTurn 写入约束测试所需的 agent turn。
func insertAgentTurn(t *testing.T, db *gorm.DB, id string, sessionID string, state string) {
	t.Helper()
	if err := insertAgentTurnError(db, id, sessionID, state); err != nil {
		t.Fatalf("写入 agent turn 失败：%v", err)
	}
}

// insertAgentTurnError 返回写入 agent turn 的原始数据库错误。
func insertAgentTurnError(db *gorm.DB, id string, sessionID string, state string) error {
	return db.Exec(`INSERT INTO agent_turns (
		id, meeting_id, agent_session_id, kind, state, idempotency_key, created_at, updated_at
	) VALUES (?, ?, ?, 'answer', ?, ?, 0, 0)`, id, testMeetingID, sessionID, state, id).Error
}

// insertSnapshot 写入一行合法上下文快照。
func insertSnapshot(t *testing.T, db *gorm.DB, id string, sessionID string, turnID string, throughSeq int64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO context_snapshots (
		id, meeting_id, agent_session_id, agent_turn_id, through_seq, content_json, content_sha256, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, '{}',
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 0, 0
	)`, id, testMeetingID, sessionID, turnID, throughSeq).Error; err != nil {
		t.Fatalf("写入上下文快照失败：%v", err)
	}
}
