package database_test

import (
	"testing"

	"meet-sieve/internal/infra/database"

	"gorm.io/gorm"
)

const (
	step8AudioAssetID = "81818181-8181-4818-8818-818181818181"
	step8SessionID    = "82828282-8282-4828-8828-828282828282"
	step8AttemptID    = "83838383-8383-4838-8838-838383838383"
)

// TestStep8Schema_ReachesVersionNineAndCreatesGapAttempts 验证 Step 8 migration 与核心新表存在。
func TestStep8Schema_ReachesVersionNineAndCreatesGapAttempts(t *testing.T) {
	db := openMigratedDatabase(t)
	if database.CurrentSchemaVersion != 9 {
		t.Fatalf("当前 schema 版本错误：got %d, want 9", database.CurrentSchemaVersion)
	}
	for _, table := range []string{"gap_transcription_attempts", "gap_transcription_attempt_items"} {
		var count int64
		if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
			t.Fatalf("检查 %s 失败：%v", table, err)
		}
		if count != 1 {
			t.Fatalf("缺少 Step 8 表：%s", table)
		}
	}
}

// TestStep8Schema_ConstrainsTransportAttemptsAndMinuteSources 验证传输模式、活动尝试与纪要来源约束。
func TestStep8Schema_ConstrainsTransportAttemptsAndMinuteSources(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)
	insertStep8AudioAsset(t, db)

	if err := insertStep8ASRSession(db, step8SessionID, "auc_flash_v3"); err != nil {
		t.Fatalf("极速文件 session 应可持久化：%v", err)
	}
	if err := insertStep8ASRSession(db, "84848484-8484-4848-8848-848484848484", "unknown"); err == nil {
		t.Fatal("未知 transport_mode 必须被拒绝")
	}

	insertStep8Attempt(t, db, step8AttemptID, "pending", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := insertStep8AttemptError(db, "85858585-8585-4858-8858-858585858585", "running", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Fatal("同一会议的第二个活动补转写尝试必须被拒绝")
	}

	insertAgentSession(t, db, step7SessionID, "available")
	insertAgentTurn(t, db, step7TurnID, step7SessionID, "completed")
	if err := insertMinuteVersionError(db, "86868686-8686-4868-8868-868686868686", "ai", nil); err == nil {
		t.Fatal("AI 纪要必须关联 agent turn")
	}
	if err := insertMinuteVersionError(db, "87878787-8787-4878-8878-878787878787", "human", nil); err != nil {
		t.Fatalf("人工纪要允许没有 agent turn：%v", err)
	}

	var indexCount int64
	if err := db.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_minute_versions_meeting_created'").Scan(&indexCount).Error; err != nil {
		t.Fatalf("检查纪要排序索引失败：%v", err)
	}
	if indexCount != 1 {
		t.Fatal("缺少纪要版本排序索引")
	}
}

// insertStep8AudioAsset 写入补转写约束测试所需的 ready 音频资产。
func insertStep8AudioAsset(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`INSERT INTO audio_assets (
		id, meeting_id, kind, sequence_no, relative_path, start_sample, end_sample,
		sample_rate, bit_depth, channels, size_bytes, sha256, state, created_at, updated_at
	) VALUES (?, ?, 'gap', 1, 'audio/gaps/test.wav', 0, 16000, 16000, 16, 1, 32044,
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'ready', 0, 0)`,
		step8AudioAssetID, testMeetingID).Error; err != nil {
		t.Fatalf("写入测试音频资产失败：%v", err)
	}
}

// insertStep8ASRSession 写入指定传输模式的 ASR session。
func insertStep8ASRSession(db *gorm.DB, id string, transportMode string) error {
	return db.Exec(`INSERT INTO asr_sessions (
		id, meeting_id, provider, provider_session_id, state, started_at, ended_at,
		reconnect_count, transport_mode, input_start_sample, last_sent_sample,
		last_final_sample, created_at, updated_at
	) VALUES (?, ?, 'volcano', ?, 'stopped', 0, 1, 0, ?, 0, 16000, 16000, 0, 1)`,
		id, testMeetingID, id, transportMode).Error
}

// insertStep8Attempt 写入一行合法补转写尝试。
func insertStep8Attempt(t *testing.T, db *gorm.DB, id string, state string, requestHash string) {
	t.Helper()
	if err := insertStep8AttemptError(db, id, state, requestHash); err != nil {
		t.Fatalf("写入补转写尝试失败：%v", err)
	}
}

// insertStep8AttemptError 返回补转写尝试的原始数据库错误。
func insertStep8AttemptError(db *gorm.DB, id string, state string, requestHash string) error {
	return db.Exec(`INSERT INTO gap_transcription_attempts (
		id, meeting_id, audio_asset_id, provider, provider_request_id,
		core_start_sample, core_end_sample, audio_start_sample, audio_end_sample,
		state, attempt_no, request_sha256, created_at, updated_at
	) VALUES (?, ?, ?, 'volcano', ?, 0, 16000, 0, 16000, ?, 1, ?, 0, 0)`,
		id, testMeetingID, step8AudioAssetID, id, state, requestHash).Error
}

// insertMinuteVersionError 返回指定来源纪要版本的原始数据库错误。
func insertMinuteVersionError(db *gorm.DB, id string, source string, turnID *string) error {
	return db.Exec(`INSERT INTO minute_versions (
		id, meeting_id, agent_turn_id, version_no, source, content_markdown,
		state, is_current, created_at, updated_at
	) VALUES (?, ?, ?, 1, ?, '# 纪要', 'draft', 1, 0, 0)`,
		id, testMeetingID, turnID, source).Error
}
