package agent_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"
	agentrepository "meet-sieve/internal/repository/agent"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	meetingID = "11111111-1111-4111-8111-111111111111"
	sessionID = "22222222-2222-4222-8222-222222222222"
	turnID    = "33333333-3333-4333-8333-333333333333"
)

// TestRepository_MapsNotFoundAndActiveConflict 验证稳定 NotFound/Conflict 语义。
func TestRepository_MapsNotFoundAndActiveConflict(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	if _, err := repository.GetSession(context.Background(), "missing"); !errors.Is(err, agentrepository.ErrNotFound) {
		t.Fatalf("缺失 session 未映射 ErrNotFound：%v", err)
	}
	createSession(t, repository, sessionID, "starting")
	err := repository.CreateSession(context.Background(), models.AgentSession{
		ID: "44444444-4444-4444-8444-444444444444", MeetingID: meetingID, Provider: "codex",
		CWDRelativePath: "meetings/test", State: "available", StartedAt: 0, CreatedAt: 0, UpdatedAt: 0,
	})
	if !errors.Is(err, agentrepository.ErrConflict) {
		t.Fatalf("活动 session 冲突未映射 ErrConflict：%v", err)
	}
}

// TestRepository_UpsertSnapshotKeepsOneRollingRow 验证同一 session 连续成功只更新滚动快照。
func TestRepository_UpsertSnapshotKeepsOneRollingRow(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	createSession(t, repository, sessionID, "available")
	createTurn(t, repository, turnID, "completed")

	first := models.ContextSnapshot{
		ID: "55555555-5555-4555-8555-555555555555", MeetingID: meetingID, AgentSessionID: sessionID,
		AgentTurnID: turnID, ThroughSeq: 0, ContentJSON: `{"current_topics":[]}`,
		ContentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CreatedAt: 1, UpdatedAt: 1,
	}
	if err := repository.UpsertSnapshot(context.Background(), first); err != nil {
		t.Fatalf("写入首个快照失败：%v", err)
	}
	first.AgentTurnID = turnID
	first.ThroughSeq = 7
	first.ContentJSON = `{"current_topics":["更新"]}`
	first.ContentSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	first.UpdatedAt = 2
	if err := repository.UpsertSnapshot(context.Background(), first); err != nil {
		t.Fatalf("覆盖滚动快照失败：%v", err)
	}

	snapshot, err := repository.GetSnapshot(context.Background(), sessionID)
	if err != nil || snapshot.ThroughSeq != 7 || snapshot.ContentSHA256 != first.ContentSHA256 {
		t.Fatalf("读取滚动快照错误：snapshot=%#v err=%v", snapshot, err)
	}
	var count int64
	if err := db.Model(&models.ContextSnapshot{}).Where("agent_session_id = ?", sessionID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("滚动快照行数错误：count=%d err=%v", count, err)
	}
}

// createSession 写入 repository 测试所需 session。
func createSession(t *testing.T, repository *agentrepository.Repository, id string, state string) {
	t.Helper()
	err := repository.CreateSession(context.Background(), models.AgentSession{
		ID: id, MeetingID: meetingID, Provider: "codex", CWDRelativePath: "meetings/test",
		State: state, StartedAt: 0, CreatedAt: 0, UpdatedAt: 0,
	})
	if err != nil {
		t.Fatalf("创建 agent session 失败：%v", err)
	}
}

// createTurn 写入 repository 测试所需 turn。
func createTurn(t *testing.T, repository *agentrepository.Repository, id string, state string) {
	t.Helper()
	err := repository.CreateTurn(context.Background(), models.AgentTurn{
		ID: id, MeetingID: meetingID, AgentSessionID: sessionID, Kind: "answer", State: state,
		IdempotencyKey: id, CreatedAt: 0, UpdatedAt: 0,
	})
	if err != nil {
		t.Fatalf("创建 agent turn 失败：%v", err)
	}
}

// openAgentDatabase 创建迁移到最新版本的隔离 SQLite。
func openAgentDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移 agent 测试数据库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 agent 测试数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	if err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (?, 'MS-20260802-0001', 'Agent 测试', 'meetings/test', 'Asia/Shanghai',
		'recording', 'saving', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0)`, meetingID).Error; err != nil {
		t.Fatalf("写入测试会议失败：%v", err)
	}
	return db
}
