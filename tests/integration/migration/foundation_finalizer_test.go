package migration_test

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"meet-sieve/internal/domain/metadata"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
	"meet-sieve/migrations"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

const (
	metadataID = "11111111-1111-4111-8111-111111111111"
	databaseID = "22222222-2222-4222-8222-222222222222"
	settingsID = "33333333-3333-4333-8333-333333333333"
)

// TestFoundationFinalizer_CreatesSingletonsAndRemovesLegacy 验证最终化忽略 legacy key/value，并原子建立完整身份。
func TestFoundationFinalizer_CreatesSingletonsAndRemovesLegacy(t *testing.T) {
	db := createMigratedV2Database(t)
	if _, err := db.Exec("INSERT INTO app_metadata_legacy(key, value, created_at, updated_at) VALUES ('database_id', 'legacy-value', 'old', 'old')"); err != nil {
		t.Fatalf("准备 legacy 数据失败：%v", err)
	}
	finalizer := newFinalizer(metadataID, databaseID, settingsID)

	identityValue, err := finalizer.Finalize(db, migration.FinalizationTargetNewDatabase)
	if err != nil {
		t.Fatalf("执行 finalizer 失败：%v", err)
	}
	if identityValue.DatabaseID != databaseID || identityValue.DeviceCode != "AB28" {
		t.Fatalf("finalizer 生成身份不正确：%+v", identityValue)
	}
	verified, err := database.ReadTypedIdentity(db)
	if err != nil {
		t.Fatalf("finalizer 后 identity 必须完整：%v", err)
	}
	if verified.DatabaseID != databaseID {
		t.Fatalf("finalizer 不得读取 legacy value：%+v", verified)
	}
	var legacyCount, settingCount int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'app_metadata_legacy'").Scan(&legacyCount); err != nil {
		t.Fatalf("检查 legacy 表失败：%v", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM settings WHERE wake_word = 'AI 助手'").Scan(&settingCount); err != nil {
		t.Fatalf("检查默认 settings 失败：%v", err)
	}
	if legacyCount != 0 || settingCount != 1 {
		t.Fatalf("finalizer 后数据不完整：legacy=%d settings=%d", legacyCount, settingCount)
	}
}

// TestFoundationFinalizer_RollsBackPartialSingletons 验证事务中断后不留下可安装的部分 singleton。
func TestFoundationFinalizer_RollsBackPartialSingletons(t *testing.T) {
	db := createMigratedV2Database(t)
	finalizer := newFinalizer(metadataID, databaseID)

	if _, err := finalizer.Finalize(db, migration.FinalizationTargetNewDatabase); err == nil {
		t.Fatal("缺少 settings ID 时 finalizer 必须失败")
	}
	var metadataCount, settingsCount, legacyCount int
	if err := db.QueryRow("SELECT count(*) FROM app_metadata").Scan(&metadataCount); err != nil {
		t.Fatalf("统计 metadata 失败：%v", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM settings").Scan(&settingsCount); err != nil {
		t.Fatalf("统计 settings 失败：%v", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'app_metadata_legacy'").Scan(&legacyCount); err != nil {
		t.Fatalf("统计 legacy 失败：%v", err)
	}
	if metadataCount != 0 || settingsCount != 0 || legacyCount != 1 {
		t.Fatalf("finalizer 回滚不完整：metadata=%d settings=%d legacy=%d", metadataCount, settingsCount, legacyCount)
	}
}

// TestFoundationFinalizer_IsIdempotentAndRejectsInstalledTarget 验证成功重试不改身份，正式库目标永远被拒绝。
func TestFoundationFinalizer_IsIdempotentAndRejectsInstalledTarget(t *testing.T) {
	db := createMigratedV2Database(t)
	finalizer := newFinalizer(metadataID, databaseID, settingsID)
	first, err := finalizer.Finalize(db, migration.FinalizationTargetStaging)
	if err != nil {
		t.Fatalf("首次 finalizer 失败：%v", err)
	}
	second, err := finalizer.Finalize(db, migration.FinalizationTargetStaging)
	if err != nil {
		t.Fatalf("幂等 finalizer 失败：%v", err)
	}
	if first != second {
		t.Fatalf("重复 finalizer 改变了身份：first=%+v second=%+v", first, second)
	}
	if _, err := finalizer.Finalize(db, migration.FinalizationTargetInstalled); !errors.Is(err, migration.ErrFinalizationTargetForbidden) {
		t.Fatalf("正式数据库目标必须拒绝：%v", err)
	}
}

// TestFoundationFinalizer_ProductionGeneratorsCreateV4Identity 验证生产生成器写入 UUID v4 与合法设备码。
func TestFoundationFinalizer_ProductionGeneratorsCreateV4Identity(t *testing.T) {
	db := createMigratedV2Database(t)
	finalizer := migration.NewFoundationFinalizer(
		identity.NewUUIDGenerator(),
		metadata.NewSecureDeviceCodeGenerator(),
		clock.NewFixed(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)),
		"0.1.0-test",
	)
	identityValue, err := finalizer.Finalize(db, migration.FinalizationTargetNewDatabase)
	if err != nil {
		t.Fatalf("生产生成器 finalizer 失败：%v", err)
	}
	for _, value := range []string{identityValue.MetadataID, identityValue.DatabaseID} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 4 {
			t.Fatalf("必须生成 UUID v4：value=%q err=%v", value, err)
		}
	}
	if _, err := metadata.ParseDeviceCode(identityValue.DeviceCode); err != nil {
		t.Fatalf("必须生成合法设备码：%q err=%v", identityValue.DeviceCode, err)
	}
}

// TestFoundationFinalizer_ConvertsStep4SpeakerAndCorrectionFacts 验证历史身份、unknown 和校对事实确定性升级。
func TestFoundationFinalizer_ConvertsStep4SpeakerAndCorrectionFacts(t *testing.T) {
	path := createStep4Database(t)
	prepareStep4SpeakerFacts(t, path, true)
	if err := database.Migrate(path); err != nil {
		t.Fatalf("升级到 Step 5 schema 失败：%v", err)
	}
	db := openSQLDatabase(t, path)

	if _, err := newFinalizer(metadataID, databaseID, settingsID).Finalize(db, migration.FinalizationTargetStaging); err != nil {
		t.Fatalf("执行 Step 5 finalizer 失败：%v", err)
	}
	assertStep5ConvertedFacts(t, db)
}

// TestFoundationFinalizer_RejectsMissingMeetingParticipant 验证无法唯一映射成员快照时整笔回滚。
func TestFoundationFinalizer_RejectsMissingMeetingParticipant(t *testing.T) {
	path := createStep4Database(t)
	prepareStep4SpeakerFacts(t, path, false)
	if err := database.Migrate(path); err != nil {
		t.Fatalf("升级到 Step 5 schema 失败：%v", err)
	}
	db := openSQLDatabase(t, path)

	if _, err := newFinalizer(metadataID, databaseID, settingsID).Finalize(db, migration.FinalizationTargetStaging); err == nil {
		t.Fatal("成员不在本场 participant 快照时 finalizer 必须失败")
	}
	var legacyCount, utteranceCount int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='utterances_step4_legacy'").Scan(&legacyCount); err != nil {
		t.Fatalf("检查 legacy 转写表失败：%v", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM utterances").Scan(&utteranceCount); err != nil {
		t.Fatalf("统计新转写表失败：%v", err)
	}
	if legacyCount != 1 || utteranceCount != 0 {
		t.Fatalf("失败 finalizer 必须回滚：legacy=%d utterances=%d", legacyCount, utteranceCount)
	}
}

// TestFoundationFinalizer_RejectsAmbiguousMeetingParticipant 验证同一成员存在重复本场快照时不猜测身份。
func TestFoundationFinalizer_RejectsAmbiguousMeetingParticipant(t *testing.T) {
	path := createStep4Database(t)
	prepareStep4SpeakerFacts(t, path, true)
	db := openSQLDatabase(t, path)
	if _, err := db.Exec(`INSERT INTO meeting_participants(
		id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at
	) VALUES ('eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', '55555555-5555-4555-8555-555555555555',
		'44444444-4444-4444-8444-444444444444', 'member', '张三重复', 1, 0, 0)`); err != nil {
		t.Fatalf("准备重复 participant 失败：%v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭 Step 4 数据库失败：%v", err)
	}
	if err := database.Migrate(path); err != nil {
		t.Fatalf("升级到 Step 5 schema 失败：%v", err)
	}
	db = openSQLDatabase(t, path)
	if _, err := newFinalizer(metadataID, databaseID, settingsID).Finalize(db, migration.FinalizationTargetStaging); err == nil {
		t.Fatal("重复 participant 快照必须阻断升级")
	}
}

// createStep4Database 使用生产 migration 的前五个版本构造真实旧库。
func createStep4Database(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "step4.db")
	if err := database.MigrateFS(path, step4MigrationFiles(t), "sqlite"); err != nil {
		t.Fatalf("创建 Step 4 数据库失败：%v", err)
	}
	return path
}

// step4MigrationFiles 复制嵌入资源的 000001～000005，避免测试维护另一套 schema SQL。
func step4MigrationFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.SQLiteFiles, "sqlite")
	if err != nil {
		t.Fatalf("读取 migration 资源失败：%v", err)
	}
	for _, entry := range entries {
		if entry.Name() >= "000006" {
			continue
		}
		name := "sqlite/" + entry.Name()
		content, readErr := fs.ReadFile(migrations.SQLiteFiles, name)
		if readErr != nil {
			t.Fatalf("读取 migration 文件失败：%v", readErr)
		}
		files[name] = &fstest.MapFile{Data: content}
	}
	return files
}

// prepareStep4SpeakerFacts 写入最小但完整的历史说话人、转写与校对事实。
func prepareStep4SpeakerFacts(t *testing.T, path string, includeParticipant bool) {
	t.Helper()
	db := openSQLDatabase(t, path)
	statements := []string{
		`INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('44444444-4444-4444-8444-444444444444', '张三', 'zhang-san', 0, 0)`,
		`INSERT INTO meetings(id, meeting_no, subject, relative_dir, local_timezone, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES ('55555555-5555-4555-8555-555555555555', 'MS-20260802-0001', '迁移会议', 'meetings/migration', 'Asia/Shanghai', 'ended', 'saved', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0)`,
		`INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES ('66666666-6666-4666-8666-666666666666', '55555555-5555-4555-8555-555555555555', 1, 'utterance.final', 0, 'asr', 'utterance', '99999999-9999-4999-8999-999999999999', 0, 0)`,
		`INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES ('77777777-7777-4777-8777-777777777777', '55555555-5555-4555-8555-555555555555', 2, 'utterance.corrected', 1, 'host', 'correction', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 1, 1)`,
		`INSERT INTO asr_sessions(id, meeting_id, provider, state, started_at, reconnect_count, transport_mode, input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at) VALUES ('88888888-8888-4888-8888-888888888888', '55555555-5555-4555-8555-555555555555', 'volcano', 'stopped', 0, 0, 'seed_v1', 0, 16000, 16000, 0, 1)`,
		`INSERT INTO speaker_clusters(id, meeting_id, asr_session_id, asr_speaker_label, assigned_member_id, confidence, assignment_source, created_at, updated_at) VALUES ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', '55555555-5555-4555-8555-555555555555', '88888888-8888-4888-8888-888888888888', 'speaker-1', '44444444-4444-4444-8444-444444444444', 0.91, 'automatic', 0, 1)`,
		`INSERT INTO speaker_clusters(id, meeting_id, asr_session_id, asr_speaker_label, confidence, assignment_source, created_at, updated_at) VALUES ('cccccccc-cccc-4ccc-8ccc-cccccccccccc', '55555555-5555-4555-8555-555555555555', '88888888-8888-4888-8888-888888888888', 'speaker-2', 0.72, 'unassigned', 1, 1)`,
		`INSERT INTO utterances(id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, asr_speaker_label, current_member_id, created_at, updated_at) VALUES ('99999999-9999-4999-8999-999999999999', '55555555-5555-4555-8555-555555555555', '66666666-6666-4666-8666-666666666666', '88888888-8888-4888-8888-888888888888', 'result-1', '原文', '修正文', 0, 16000, 'speaker-1', '44444444-4444-4444-8444-444444444444', 0, 1)`,
		`INSERT INTO corrections(id, meeting_id, event_id, target_kind, target_id, correction_kind, before_json, after_json, operator_kind, reason, created_at, updated_at) VALUES ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', '55555555-5555-4555-8555-555555555555', '77777777-7777-4777-8777-777777777777', 'utterance', '99999999-9999-4999-8999-999999999999', 'text', '{"text":"原文"}', '{"text":"修正文"}', 'system', 'legacy', 1, 1)`,
	}
	if includeParticipant {
		statements = append(statements, `INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES ('dddddddd-dddd-4ddd-8ddd-dddddddddddd', '55555555-5555-4555-8555-555555555555', '44444444-4444-4444-8444-444444444444', 'member', '张三', 0, 0, 0)`)
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("准备 Step 4 历史事实失败：%v", err)
		}
	}
}

// openSQLDatabase 打开测试 SQLite，并在用例结束时关闭。
func openSQLDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// assertStep5ConvertedFacts 验证 finalizer 的核心确定性投影和 legacy 清理结果。
func assertStep5ConvertedFacts(t *testing.T, db *sql.DB) {
	t.Helper()
	var participantID, trackID, source, requestID string
	var textRevision, targetRevision, resultRevision, clusterCount, legacyCount int
	if err := db.QueryRow("SELECT current_participant_id, speaker_track_id, speaker_assignment_source, text_revision FROM utterances WHERE id='99999999-9999-4999-8999-999999999999'").Scan(&participantID, &trackID, &source, &textRevision); err != nil {
		t.Fatalf("读取升级后转写失败：%v", err)
	}
	if participantID != "dddddddd-dddd-4ddd-8ddd-dddddddddddd" || trackID != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" || source != "automatic_member" || textRevision != 2 {
		t.Fatalf("转写身份投影错误：participant=%s track=%s source=%s revision=%d", participantID, trackID, source, textRevision)
	}
	if err := db.QueryRow("SELECT request_id, target_revision, result_revision FROM corrections WHERE id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'").Scan(&requestID, &targetRevision, &resultRevision); err != nil {
		t.Fatalf("读取升级后 correction 失败：%v", err)
	}
	parsed, err := uuid.Parse(requestID)
	if err != nil || parsed.Version() != 5 || targetRevision != 1 || resultRevision != 2 {
		t.Fatalf("历史 correction 回填不确定：request=%s target=%d result=%d err=%v", requestID, targetRevision, resultRevision, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM speaker_clusters WHERE display_no=1 AND assignment_source='unassigned'").Scan(&clusterCount); err != nil {
		t.Fatalf("统计 unknown cluster 失败：%v", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE '%step4_legacy'").Scan(&legacyCount); err != nil {
		t.Fatalf("统计 legacy staging 失败：%v", err)
	}
	if clusterCount != 1 || legacyCount != 0 {
		t.Fatalf("unknown 或 legacy 清理错误：clusters=%d legacy=%d", clusterCount, legacyCount)
	}
}

// createMigratedV2Database 创建尚未 finalizer 的最新 schema。
func createMigratedV2Database(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "finalizer.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newFinalizer 创建使用确定性时间、ID 和设备码的测试 finalizer。
func newFinalizer(ids ...string) *migration.FoundationFinalizer {
	return migration.NewFoundationFinalizer(
		identity.NewFixedGenerator(ids...),
		metadata.NewDeviceCodeGenerator(metadata.FixedRandomSource{0, 1, 24, 30}),
		clock.NewFixed(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)),
		"0.1.0-test",
	)
}
