package finalization_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	agentrepository "meet-sieve/internal/repository/agent"
	gaprepository "meet-sieve/internal/repository/gap"
	minutesrepository "meet-sieve/internal/repository/minutes"
	finalizationservice "meet-sieve/internal/service/finalization"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	meetingGapID  = "11111111-1111-4111-8111-111111111111"
	meetingSyncID = "22222222-2222-4222-8222-222222222222"
)

// TestRecoveryCoordinatorSettlesLocalStateWithoutNetwork 验证重启只收敛 SQLite 遗留状态。
func TestRecoveryCoordinatorSettlesLocalStateWithoutNetwork(t *testing.T) {
	db := openRecoveryDatabase(t)
	prepareRecoveryFacts(t, db)
	transactions := database.NewTransactionManager(db)
	coordinator := finalizationservice.NewRecoveryCoordinator(
		gaprepository.NewRepository(db, transactions),
		minutesrepository.NewRepository(db, transactions),
		agentrepository.NewRepository(db, transactions),
		clock.NewFixed(time.UnixMilli(100)),
	)
	if err := coordinator.RecoverInterrupted(context.Background()); err != nil {
		t.Fatalf("收敛会后遗留状态失败：%v", err)
	}
	assertState(t, db, &models.GapTranscriptionAttempt{}, "33333333-3333-4333-8333-333333333333", "failed", "GAP_ATTEMPT_INTERRUPTED")
	assertState(t, db, &models.AgentTurn{}, "44444444-4444-4444-8444-444444444444", "failed", "MINUTES_GENERATION_INTERRUPTED")
	assertState(t, db, &models.AgentTurn{}, "55555555-5555-4555-8555-555555555555", "failed", "AGENT_FINAL_SYNC_INTERRUPTED")
	var gap models.ASRGap
	if err := db.Where("id = ?", "66666666-6666-4666-8666-666666666666").Take(&gap).Error; err != nil || gap.State != "failed" {
		t.Fatalf("gap 未收敛：gap=%#v err=%v", gap, err)
	}
	var gapMeeting, syncMeeting models.Meeting
	if err := db.Where("id = ?", meetingGapID).Take(&gapMeeting).Error; err != nil || gapMeeting.MinuteState != "failed" {
		t.Fatalf("纪要聚合未收敛：meeting=%#v err=%v", gapMeeting, err)
	}
	if err := db.Where("id = ?", meetingSyncID).Take(&syncMeeting).Error; err != nil || syncMeeting.AgentState != "unsynced" {
		t.Fatalf("结束同步聚合未收敛：meeting=%#v err=%v", syncMeeting, err)
	}
}

// assertState 校验带稳定错误码的遗留终态。
func assertState(t *testing.T, db *gorm.DB, model any, id string, state string, errorCode string) {
	t.Helper()
	var result struct {
		State         string  `gorm:"column:state"`
		LastErrorCode *string `gorm:"column:last_error_code"`
	}
	if err := db.Model(model).Select("state", "last_error_code").Where("id = ?", id).Scan(&result).Error; err != nil || result.State != state || result.LastErrorCode == nil || *result.LastErrorCode != errorCode {
		t.Fatalf("遗留终态错误：id=%s state=%#v err=%v", id, result, err)
	}
}

// openRecoveryDatabase 创建真实 version 9 SQLite。
func openRecoveryDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recovery.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}

// prepareRecoveryFacts 写入互不违反活动 turn 唯一约束的 gap、minutes 和 ingest 遗留事实。
func prepareRecoveryFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO meetings (id, meeting_no, subject, relative_dir, local_timezone, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES ('11111111-1111-4111-8111-111111111111','M-1','恢复一','meetings/1','Asia/Shanghai','ended','saved','stopped','processing','available','generating','stopped',1,1)`,
		`INSERT INTO meetings (id, meeting_no, subject, relative_dir, local_timezone, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES ('22222222-2222-4222-8222-222222222222','M-2','恢复二','meetings/2','Asia/Shanghai','ended','saved','stopped','none','busy','not_generated','stopped',1,1)`,
		`INSERT INTO meeting_events (id,meeting_id,seq,kind,occurred_at,source,entity_type,entity_id,created_at,updated_at) VALUES ('77777777-7777-4777-8777-777777777777','11111111-1111-4111-8111-111111111111',1,'asr.gap',1,'system','asr_gap','66666666-6666-4666-8666-666666666666',1,1)`,
		`INSERT INTO asr_gaps (id,meeting_id,event_id,start_sample,end_sample,reason,origin_key,state,attempt_count,created_at,updated_at) VALUES ('66666666-6666-4666-8666-666666666666','11111111-1111-4111-8111-111111111111','77777777-7777-4777-8777-777777777777',0,16000,'record_only','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','processing',1,1,1)`,
		`INSERT INTO audio_assets (id,meeting_id,kind,sequence_no,relative_path,start_sample,end_sample,sample_rate,bit_depth,channels,size_bytes,sha256,state,created_at,updated_at) VALUES ('88888888-8888-4888-8888-888888888888','11111111-1111-4111-8111-111111111111','gap',1,'meetings/1/audio/gaps/a.wav',0,16000,16000,16,1,32044,'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','ready',1,1)`,
		`INSERT INTO gap_transcription_attempts (id,meeting_id,audio_asset_id,provider,provider_request_id,core_start_sample,core_end_sample,audio_start_sample,audio_end_sample,state,attempt_no,request_sha256,started_at,created_at,updated_at) VALUES ('33333333-3333-4333-8333-333333333333','11111111-1111-4111-8111-111111111111','88888888-8888-4888-8888-888888888888','volcano','99999999-9999-4999-8999-999999999999',0,16000,0,16000,'running',1,'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',1,1,1)`,
		`INSERT INTO gap_transcription_attempt_items (attempt_id,gap_id,item_order,created_at) VALUES ('33333333-3333-4333-8333-333333333333','66666666-6666-4666-8666-666666666666',0,1)`,
		`INSERT INTO agent_sessions (id,meeting_id,provider,thread_id,cwd_relative_path,state,started_at,created_at,updated_at) VALUES ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','11111111-1111-4111-8111-111111111111','codex','thread-1','meetings/1','ended',1,1,1)`,
		`INSERT INTO agent_sessions (id,meeting_id,provider,thread_id,cwd_relative_path,state,started_at,created_at,updated_at) VALUES ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','22222222-2222-4222-8222-222222222222','codex','thread-2','meetings/2','ended',1,1,1)`,
		`INSERT INTO agent_turns (id,meeting_id,agent_session_id,kind,state,idempotency_key,created_at,updated_at) VALUES ('44444444-4444-4444-8444-444444444444','11111111-1111-4111-8111-111111111111','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','minutes','running','minutes-recovery',1,1)`,
		`INSERT INTO agent_turns (id,meeting_id,agent_session_id,kind,state,idempotency_key,created_at,updated_at) VALUES ('55555555-5555-4555-8555-555555555555','22222222-2222-4222-8222-222222222222','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','ingest','pending','final-sync:2',1,1)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备恢复事实失败：%v\nSQL: %s", err, statement)
		}
	}
}
