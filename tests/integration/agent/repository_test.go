package agent_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"
	agentrepository "meet-sieve/internal/repository/agent"
	serviceagent "meet-sieve/internal/service/agent"
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

// TestRecoveryCommandService_FallsBackToFilesWithoutSession 验证历史 thread 缺失不阻断会议文件接续。
func TestRecoveryCommandService_FallsBackToFilesWithoutSession(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	rawRecord := &turnRawRecord{}
	service := serviceagent.NewRecoveryCommandService(repository, rawRecord, t.TempDir())

	commands, err := service.Get(context.Background(), meetingID)
	if err != nil {
		t.Fatalf("缺失 session 时读取文件接续信息失败: %v", err)
	}
	if rawRecord.flushes != 1 {
		t.Fatalf("打开接续信息前必须刷新原始记录: %d", rawRecord.flushes)
	}
	if commands.ThreadAvailable || commands.ThreadCommand != "" {
		t.Fatalf("缺失 session 不应伪造恢复命令: %+v", commands)
	}
	if commands.DirectoryCommand == "" || commands.RecoveryPrompt == "" {
		t.Fatalf("缺失 session 仍必须提供文件接续信息: %+v", commands)
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

// TestRepository_FailInitializationIsIdempotent 验证重复失败收敛不会用状态冲突覆盖原始错误。
func TestRepository_FailInitializationIsIdempotent(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	if err := db.Model(&models.Meeting{}).Where("id = ?", meetingID).Update("agent_state", "initializing").Error; err != nil {
		t.Fatalf("准备会议初始化状态失败：%v", err)
	}
	createSession(t, repository, sessionID, "starting")

	for attempt := 1; attempt <= 2; attempt++ {
		if err := repository.FailInitialization(context.Background(), sessionID, "AGENT_OUTPUT_INVALID", 2_000_000_000_000+int64(attempt)); err != nil {
			t.Fatalf("第 %d 次收敛初始化失败：%v", attempt, err)
		}
	}

	var session models.AgentSession
	if err := db.Where("id = ?", sessionID).Take(&session).Error; err != nil {
		t.Fatalf("读取失败 session 失败：%v", err)
	}
	if session.State != "failed" || session.LastErrorCode == nil || *session.LastErrorCode != "AGENT_OUTPUT_INVALID" {
		t.Fatalf("失败 session 状态错误：%#v", session)
	}
}

// TestRepository_PersistsProbeAndInvalidatesOnlyExecutableChange 验证页面重建可恢复检测结果，修改唤醒词不会误清空。
func TestRepository_PersistsProbeAndInvalidatesOnlyExecutableChange(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	ctx := context.Background()
	const (
		createdAt           int64 = 1_785_754_364_571
		probedAt                  = createdAt + 1_234
		wakeWordUpdatedAt         = createdAt + 2_000
		executableUpdatedAt       = createdAt + 3_000
	)
	if err := db.Create(&models.Settings{
		ID: "99999999-9999-4999-8999-999999999999", SingletonKey: 1,
		WakeWord: "AI 助手", CreatedAt: createdAt, UpdatedAt: createdAt,
	}).Error; err != nil {
		t.Fatalf("创建测试设置失败：%v", err)
	}
	if err := repository.UpdateProbeSnapshot(ctx, "available", "1.2.3", "logged_in", "compatible", "Codex 可用", probedAt); err != nil {
		t.Fatalf("保存检测结果失败：%v", err)
	}
	settings, err := repository.GetSettings(ctx)
	if err != nil || settings.UpdatedAt != probedAt {
		t.Fatalf("检测结果必须使用毫秒时间更新审计字段：settings=%+v err=%v", settings, err)
	}
	if err := repository.UpdateSettings(ctx, "会议助手", nil, wakeWordUpdatedAt); err != nil {
		t.Fatalf("只修改唤醒词失败：%v", err)
	}
	settings, err = repository.GetSettings(ctx)
	if err != nil || settings.CodexAvailabilityState != "available" || settings.CodexProbedAt == nil || *settings.CodexProbedAt != probedAt {
		t.Fatalf("只修改唤醒词不应清空检测结果：settings=%+v err=%v", settings, err)
	}
	path := "/opt/homebrew/bin/codex"
	if err := repository.UpdateSettings(ctx, "会议助手", &path, executableUpdatedAt); err != nil {
		t.Fatalf("修改 executable 失败：%v", err)
	}
	settings, err = repository.GetSettings(ctx)
	if err != nil || settings.CodexAvailabilityState != "unchecked" || settings.CodexProbedAt != nil {
		t.Fatalf("修改 executable 后必须让检测结果失效：settings=%+v err=%v", settings, err)
	}
}

// TestRepository_ReleasesOrphanVoiceCommandCandidates 验证重启恢复只释放未绑定 turn 的候选 final。
func TestRepository_ReleasesOrphanVoiceCommandCandidates(t *testing.T) {
	db := openAgentDatabase(t)
	repository := agentrepository.NewRepository(db, database.NewTransactionManager(db))
	ctx := context.Background()
	const (
		asrSessionID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		eventID      = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		utteranceID  = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
		relationID   = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	)
	if err := db.Exec(`INSERT INTO asr_sessions (
		id, meeting_id, provider, state, started_at, reconnect_count, transport_mode,
		input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at
	) VALUES (?, ?, 'volcano', 'stopped', 0, 0, 'seed_v1', 0, 10, 10, 0, 0)`, asrSessionID, meetingID).Error; err != nil {
		t.Fatalf("创建测试 ASR session 失败：%v", err)
	}
	if err := db.Create(&models.MeetingEvent{
		ID: eventID, MeetingID: meetingID, Seq: 1, Kind: "utterance.final", OccurredAt: 0,
		Source: "asr", EntityType: stringPointer("utterance"), EntityID: stringPointer(utteranceID), CreatedAt: 0, UpdatedAt: 0,
	}).Error; err != nil {
		t.Fatalf("创建测试 meeting event 失败：%v", err)
	}
	if err := db.Create(&models.Utterance{
		ID: utteranceID, MeetingID: meetingID, EventID: eventID, ASRSessionID: asrSessionID,
		ProviderResultID: "orphan-final", OriginalText: "会议助手", CurrentText: "会议助手",
		StartSample: 0, EndSample: 10, SpeakerAssignmentSource: "unassigned", TextRevision: 1, SpeakerRevision: 1,
	}).Error; err != nil {
		t.Fatalf("创建测试 final 失败：%v", err)
	}
	if err := db.Create(&models.AgentVoiceCommandUtterance{
		ID: relationID, MeetingID: meetingID, CommandID: "orphan-command", UtteranceID: utteranceID,
		Position: 0, State: "candidate", CreatedAt: 0, UpdatedAt: 0,
	}).Error; err != nil {
		t.Fatalf("创建遗留候选关系失败：%v", err)
	}
	if err := repository.ReleaseOrphanVoiceCommandCandidates(ctx); err != nil {
		t.Fatalf("释放遗留候选关系失败：%v", err)
	}
	var relation models.AgentVoiceCommandUtterance
	if err := db.Where("id = ?", relationID).Take(&relation).Error; err != nil || relation.State != "released" {
		t.Fatalf("遗留候选关系未恢复为普通内容：relation=%+v err=%v", relation, err)
	}
}

// stringPointer 返回测试模型需要的字符串指针。
func stringPointer(value string) *string { return &value }

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
