package database_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"meet-sieve/internal/infra/database"

	"gorm.io/gorm"
)

// TestMigrate_CreatesStep1FoundationSchema 验证首次 migration 升级到 typed metadata 与 settings 基础结构。
func TestMigrate_CreatesStep1FoundationSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}

	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, table := range []string{"app_metadata_legacy", "app_metadata", "settings"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("%s 未创建", table)
		}
	}
	var migration struct {
		Version uint
		Dirty   bool
	}
	if err := db.Raw("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&migration).Error; err != nil {
		t.Fatalf("读取 migration 版本失败：%v", err)
	}
	if migration.Version != database.CurrentSchemaVersion || migration.Dirty {
		t.Fatalf("migration 版本不正确：%#v", migration)
	}
}

// TestMigrate_ConstrainsMetadataDeviceCode 验证数据库也拒绝领域字符集之外的设备码。
func TestMigrate_ConstrainsMetadataDeviceCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata-device-code.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	err = db.Exec(`INSERT INTO app_metadata (
		id, singleton_key, product, database_id, device_code, created_with_app_version, created_at, updated_at
	) VALUES (
		'11111111-1111-4111-8111-111111111111', 1, 'meet-sieve',
		'22222222-2222-4222-8222-222222222222', 'A0CD', '0.1.0', 0, 0
	)`).Error
	if err == nil {
		t.Fatal("含 0 的设备码必须被数据库约束拒绝")
	}
}

// TestMigrate_CreatesPeopleAndGroupSchema 验证成员、小组和关系表的活动名称唯一约束。
func TestMigrate_CreatesPeopleAndGroupSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "people.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, table := range []string{"members", "groups", "group_members"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("%s 未创建", table)
		}
	}
	if err := db.Exec("INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('33333333-3333-4333-8333-333333333333', '张三', 'zhang-san', 0, 0)").Error; err != nil {
		t.Fatalf("写入活动成员失败：%v", err)
	}
	if err := db.Exec("INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('44444444-4444-4444-8444-444444444444', '张三旧', 'zhang-san', 0, 0)").Error; err == nil {
		t.Fatal("活动成员名称规范化后必须唯一")
	}
}

// TestMigrate_AddsStep2VoiceSampleColumns 验证 000003 以增量 migration 补齐声纹资料处理字段。
func TestMigrate_AddsStep2VoiceSampleColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "step2-voice.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, column := range []string{
		"source_kind", "source_name", "environment_kind", "channels", "bit_depth",
		"processing_state", "quality_code", "quality_metrics_json", "last_error_code",
	} {
		if !db.Migrator().HasColumn("voice_samples", column) {
			t.Fatalf("voice_samples 缺少 Step 2 字段：%s", column)
		}
	}

	var migration struct {
		Version uint
		Dirty   bool
	}
	if err := db.Raw("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&migration).Error; err != nil {
		t.Fatalf("读取 migration 版本失败：%v", err)
	}
	if migration.Version != database.CurrentSchemaVersion || migration.Dirty {
		t.Fatalf("migration 版本不正确：%#v", migration)
	}
}

// TestMigrate_CreatesMeetingSchemaWithOrthogonalStates 验证会议状态轴和临时参与者约束。
func TestMigrate_CreatesMeetingSchemaWithOrthogonalStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meetings.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, table := range []string{"meeting_number_sequences", "meetings", "meeting_participants"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("%s 未创建", table)
		}
	}
	if err := db.Exec(`INSERT INTO meetings (
        id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at,
        lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state,
        created_at, updated_at
    ) VALUES (
        '55555555-5555-4555-8555-555555555555', 'MS-20260731-0001', '测试会议', 'meetings/test', 'Asia/Shanghai', NULL, NULL,
        'preparing', 'pending', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0
    )`).Error; err != nil {
		t.Fatalf("写入合法会议失败：%v", err)
	}
	if err := db.Exec("INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES ('66666666-6666-4666-8666-666666666666', '55555555-5555-4555-8555-555555555555', NULL, 'member', '错误参与者', 0, 0, 0)").Error; err == nil {
		t.Fatal("member 类型参与者必须引用成员")
	}
}

// TestMigrate_CreatesEventAndContentSchema 验证统一事件序号、JSON 校正和资源路径约束。
func TestMigrate_CreatesEventAndContentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, table := range []string{"meeting_events", "utterances", "guest_sessions", "messages", "resources", "corrections"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("%s 未创建", table)
		}
	}
}

// TestMigrate_CreatesAudioAndAgentSchema 验证音频、ASR、声纹、Agent、纪要和删除任务表全部由 000002 创建。
func TestMigrate_CreatesAudioAndAgentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, table := range []string{
		"audio_assets", "asr_sessions", "asr_gaps", "voice_samples", "voice_embeddings", "speaker_clusters",
		"agent_sessions", "agent_turns", "sync_batches", "context_snapshots", "minute_versions", "deletion_jobs",
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("%s 未创建", table)
		}
	}
}

// TestMigrate_AddsRealtimeASRFoundation 验证 Step 4 以增量 migration 补齐实时转写的传输、样本与缺口事实字段。
func TestMigrate_AddsRealtimeASRFoundation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "realtime-asr.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, column := range []string{"transport_mode", "input_start_sample", "last_sent_sample", "last_final_sample"} {
		if !db.Migrator().HasColumn("asr_sessions", column) {
			t.Fatalf("asr_sessions 缺少 Step 4 字段：%s", column)
		}
	}
	for _, column := range []string{"origin_key", "reason"} {
		if !db.Migrator().HasColumn("asr_gaps", column) {
			t.Fatalf("asr_gaps 缺少 Step 4 字段：%s", column)
		}
	}
	if err := db.Exec(`INSERT INTO asr_gaps (
		id, meeting_id, event_id, start_sample, end_sample, state, attempt_count,
		origin_key, reason, created_at, updated_at
	) VALUES (
		'11111111-1111-4111-8111-111111111111', 'meeting-id', 'event-id', 0, 16000, 'pending', 0,
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'record_only', 0, 0
	)`).Error; err == nil {
		t.Fatal("asr_gaps 必须保留 meeting/event 外键约束，不能接受不存在的会议")
	}
}

// TestMigrate_AddsSpeakerCorrectionFoundation 验证 Step 5 空库具备说话人处理与人工校对结构。
func TestMigrate_AddsSpeakerCorrectionFoundation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "speaker-correction.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	for _, table := range []string{"speaker_tracks", "speaker_track_evidence", "correction_items"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("Step 5 缺少表：%s", table)
		}
	}
	assertTableHasColumns(t, db, "utterances", []string{
		"current_participant_id", "speaker_track_id", "speaker_cluster_id",
		"speaker_assignment_source", "speaker_confidence", "text_revision", "speaker_revision",
	})
	if db.Migrator().HasColumn("utterances", "current_member_id") {
		t.Fatal("utterances 不得继续保留语义不完整的 current_member_id")
	}
	assertTableHasColumns(t, db, "corrections", []string{
		"request_id", "target_revision", "result_revision", "batch_scope",
	})
	assertTableHasColumns(t, db, "resources", []string{
		"original_description", "current_description", "description_revision",
	})
	assertTableHasColumns(t, db, "voice_samples", []string{"source_meeting_id", "source_utterance_id"})
	assertTableHasColumns(t, db, "speaker_clusters", []string{
		"display_no", "assigned_participant_id", "centroid", "track_count", "revision",
	})
}

// TestMigrate_MeetingClipReferencesBecomeNull 验证删除会议只清空片段来源，不删除成员声纹样本。
func TestMigrate_MeetingClipReferencesBecomeNull(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting-clip.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })

	statements := []string{
		`INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('11111111-1111-4111-8111-111111111111', '张三', 'zhang-san', 0, 0)`,
		`INSERT INTO meetings(id, meeting_no, subject, relative_dir, local_timezone, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES ('22222222-2222-4222-8222-222222222222', 'MS-20260802-0002', '片段会议', 'meetings/clip', 'Asia/Shanghai', 'ended', 'saved', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0)`,
		`INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES ('33333333-3333-4333-8333-333333333333', '22222222-2222-4222-8222-222222222222', 1, 'utterance.final', 0, 'asr', 'utterance', '44444444-4444-4444-8444-444444444444', 0, 0)`,
		`INSERT INTO asr_sessions(id, meeting_id, provider, state, started_at, reconnect_count, transport_mode, input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at) VALUES ('55555555-5555-4555-8555-555555555555', '22222222-2222-4222-8222-222222222222', 'volcano', 'stopped', 0, 0, 'seed_v1', 0, 16000, 16000, 0, 0)`,
		`INSERT INTO utterances(id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, speaker_assignment_source, text_revision, speaker_revision, created_at, updated_at) VALUES ('44444444-4444-4444-8444-444444444444', '22222222-2222-4222-8222-222222222222', '33333333-3333-4333-8333-333333333333', '55555555-5555-4555-8555-555555555555', 'result-clip', '片段', '片段', 0, 16000, 'unassigned', 1, 1, 0, 0)`,
		`INSERT INTO voice_samples(id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256, source_kind, source_meeting_id, source_utterance_id, environment_kind, processing_state, quality_state, created_at, updated_at) VALUES ('66666666-6666-4666-8666-666666666666', '11111111-1111-4111-8111-111111111111', 'voice-samples/clip.wav', 1000, 16000, 1, 16, 32044, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'meeting_clip', '22222222-2222-4222-8222-222222222222', '44444444-4444-4444-8444-444444444444', 'meeting_room', 'ready', 'accepted', 0, 0)`,
		`DELETE FROM meetings WHERE id='22222222-2222-4222-8222-222222222222'`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("执行 meeting clip 约束用例失败：%v", err)
		}
	}
	var sampleCount int64
	var sourceMeetingID, sourceUtteranceID *string
	if err := db.Raw("SELECT count(*), source_meeting_id, source_utterance_id FROM voice_samples WHERE id='66666666-6666-4666-8666-666666666666'").Row().Scan(&sampleCount, &sourceMeetingID, &sourceUtteranceID); err != nil {
		t.Fatalf("读取会议片段样本失败：%v", err)
	}
	if sampleCount != 1 || sourceMeetingID != nil || sourceUtteranceID != nil {
		t.Fatalf("删除会议后样本必须保留且来源置空：count=%d meeting=%v utterance=%v", sampleCount, sourceMeetingID, sourceUtteranceID)
	}
}

// assertTableHasColumns 验证目标表包含当前切片要求的全部字段。
func assertTableHasColumns(t *testing.T, db interface {
	Migrator() gorm.Migrator
}, table string, columns []string) {
	t.Helper()
	for _, column := range columns {
		if !db.Migrator().HasColumn(table, column) {
			t.Fatalf("%s 缺少 Step 5 字段：%s", table, column)
		}
	}
}

// TestMigrate_RejectsDirtyDatabase 验证 dirty migration 会阻止继续升级。
func TestMigrate_RejectsDirtyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开 dirty 数据库失败：%v", err)
	}
	if _, err := db.Exec("CREATE TABLE schema_migrations (version uint64, dirty bool); INSERT INTO schema_migrations(version, dirty) VALUES (7, 1)"); err != nil {
		_ = db.Close()
		t.Fatalf("准备 dirty migration 失败：%v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭 dirty 数据库失败：%v", err)
	}

	err = database.Migrate(path)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "dirty") {
		t.Fatalf("dirty migration 诊断不正确：%v", err)
	}
}

// TestMigrateFS_ReportsInvalidSQL 验证 migration SQL 失败不会被吞掉。
func TestMigrateFS_ReportsInvalidSQL(t *testing.T) {
	files := fstest.MapFS{
		"sqlite/000001_invalid.up.sql":   {Data: []byte("INVALID SQL")},
		"sqlite/000001_invalid.down.sql": {Data: []byte("DROP TABLE invalid")},
	}
	err := database.MigrateFS(filepath.Join(t.TempDir(), "invalid.db"), files, "sqlite")
	if err == nil || !strings.Contains(err.Error(), "执行 migration 失败") {
		t.Fatalf("非法 migration 诊断不正确：%v", err)
	}
}

// TestMigrate_AllowsRepeatedExecution 验证重复执行 migration 不会将 ErrNoChange 视为失败。
func TestMigrate_AllowsRepeatedExecution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repeat.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("首次 migration 失败：%v", err)
	}
	if err := database.Migrate(path); err != nil {
		t.Fatalf("重复 migration 失败：%v", err)
	}
}
