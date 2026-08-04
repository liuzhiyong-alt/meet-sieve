package database_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"meet-sieve/internal/infra/database"
	"meet-sieve/migrations"
	"meet-sieve/models"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

const (
	testMeetingID = "55555555-5555-4555-8555-555555555555"
	testEventID   = "66666666-6666-4666-8666-666666666666"
)

// TestSchema_LeavesDynamicSingletonsForFinalizer 验证 migration 只创建结构，不写动态身份或设置。
func TestSchema_LeavesDynamicSingletonsForFinalizer(t *testing.T) {
	db := openMigratedDatabase(t)
	for _, table := range []string{"app_metadata", "settings"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("统计 %s 失败：%v", table, err)
		}
		if count != 0 {
			t.Fatalf("migration 不应写入动态 singleton：table=%s count=%d", table, count)
		}
	}
}

// TestSchema_RejectsInvalidPersistenceValues 验证关键枚举、JSON、路径、哈希和时间约束由 SQLite 兜底。
func TestSchema_RejectsInvalidPersistenceValues(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)
	insertMeetingEvent(t, db, testEventID, 1)

	assertStatementFails(t, db, `INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (
		'77777777-7777-4777-8777-777777777777', 'MS-invalid', '错误会议', 'meetings/invalid', 'Asia/Shanghai',
		'unknown', 'pending', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0
	)`)
	assertStatementFails(t, db, `INSERT INTO resources (
		id, meeting_id, event_id, kind, relative_path, state, created_at, updated_at
	) VALUES (
		'88888888-8888-4888-8888-888888888888', ?, ?, 'attachment', '../escape', 'ready', 0, 0
	)`, testMeetingID, testEventID)
	assertStatementFails(t, db, `INSERT INTO corrections (
		id, meeting_id, event_id, target_kind, target_id, correction_kind, before_json, after_json,
		operator_kind, operator_id, created_at, updated_at
	) VALUES (
		'99999999-9999-4999-8999-999999999999', ?, 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',
		'utterance', 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', 'text', '{', '{}', 'host', 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', 0, 0
	)`, testMeetingID)
	assertStatementFails(t, db, `INSERT INTO audio_assets (
		id, meeting_id, kind, sequence_no, relative_path, start_sample, end_sample, sample_rate, bit_depth, channels,
		size_bytes, sha256, state, created_at, updated_at
	) VALUES (
		'dddddddd-dddd-4ddd-8ddd-dddddddddddd', ?, 'mixed', 1, 'audio/1.wav', 0, 1, 16000, 16, 1,
		0, 'not-a-sha256', 'ready', 0, 0
	)`, testMeetingID)
	assertStatementFails(t, db, `INSERT INTO guest_sessions (
		id, meeting_id, display_name, session_token_hash, state, expires_at, created_at, updated_at
	) VALUES (
		'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', ?, '访客',
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'active', -1, 0, 0
	)`, testMeetingID)
}

// TestSchema_RejectsSecondActiveMeeting 验证 SQLite 约束保证同一数据库最多一场活动会议。
func TestSchema_RejectsSecondActiveMeeting(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)

	err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (
		'12121212-1212-4121-8121-121212121212', 'MS-20260731-0002', '第二场会议', 'meetings/second', 'Asia/Shanghai',
		'recording', 'saving', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0
	)`).Error
	if err == nil {
		t.Fatal("第二场活动会议必须被数据库约束拒绝")
	}
}

// TestSchema_AllowsTerminalMeetingAlongsideActiveMeeting 验证终态历史会议不占用单活动会议配额。
func TestSchema_AllowsTerminalMeetingAlongsideActiveMeeting(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)

	if err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (
		'13131313-1313-4131-8131-131313131313', 'MS-20260731-0003', '历史会议', 'meetings/history', 'Asia/Shanghai', 0, 1,
		'ended', 'saved', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 1
	)`).Error; err != nil {
		t.Fatalf("终态会议应可与活动会议并存：%v", err)
	}
}

// TestSchema_ForeignKeysAreEnabledAndDeclared 验证引用关系在 SQLite 中真实执行，而不是仅存在于 models。
func TestSchema_ForeignKeysAreEnabledAndDeclared(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)
	if err := db.Exec(`INSERT INTO meeting_participants (
		id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at
	) VALUES (
		'ffffffff-ffff-4fff-8fff-ffffffffffff', ?, '00000000-0000-4000-8000-000000000000', 'member', '不存在成员', 0, 0
	)`, testMeetingID).Error; err == nil {
		t.Fatal("不存在的成员外键必须被拒绝")
	}
}

// TestSchema_DevelopmentDownRestoresStep1Foundation 验证从最新版本回退至 Step 1 后恢复原始声纹表结构。
func TestSchema_DevelopmentDownRestoresStep1Foundation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "down.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行正向 migration 失败：%v", err)
	}
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)
	rollbackLatestMigration(t, path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开回退后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var meetingTableCount int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'meetings'").Scan(&meetingTableCount); err != nil {
		t.Fatalf("检查 meetings 表失败：%v", err)
	}
	if meetingTableCount != 1 {
		t.Fatal("回退至 Step 1 后必须保留 Step 1 业务表")
	}
	var step2ColumnCount int
	if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('voice_samples') WHERE name = 'processing_state'").Scan(&step2ColumnCount); err != nil {
		t.Fatalf("检查 Step 2 声纹字段失败：%v", err)
	}
	if step2ColumnCount != 0 {
		t.Fatal("回退至 Step 1 后不得保留 Step 2 声纹字段")
	}
	var columns int
	if err := db.QueryRow("SELECT count(*) FROM pragma_table_info('voice_samples')").Scan(&columns); err != nil {
		t.Fatalf("检查 Step 1 voice_samples 表失败：%v", err)
	}
	if columns != 10 {
		t.Fatalf("回退后 voice_samples 列数错误：got=%d want=10", columns)
	}
}

// TestSchema_IndexesEveryForeignKeyColumn 验证外键列均有以该列开头的索引，避免后续关联查询退化为全表扫描。
func TestSchema_IndexesEveryForeignKeyColumn(t *testing.T) {
	db := openMigratedDatabase(t)
	requirements := map[string][]string{
		"group_members":                   {"group_id", "member_id"},
		"meeting_participants":            {"meeting_id", "member_id"},
		"meeting_events":                  {"meeting_id"},
		"utterances":                      {"meeting_id", "event_id", "asr_session_id", "current_participant_id", "speaker_track_id", "speaker_cluster_id"},
		"guest_sessions":                  {"meeting_id"},
		"messages":                        {"meeting_id", "event_id", "member_id", "guest_session_id"},
		"resources":                       {"meeting_id", "event_id", "guest_session_id"},
		"corrections":                     {"meeting_id", "event_id"},
		"correction_items":                {"correction_id"},
		"audio_assets":                    {"meeting_id"},
		"asr_sessions":                    {"meeting_id"},
		"asr_gaps":                        {"meeting_id", "event_id", "asr_session_id", "audio_asset_id"},
		"gap_transcription_attempts":      {"meeting_id", "audio_asset_id"},
		"gap_transcription_attempt_items": {"attempt_id", "gap_id"},
		"voice_samples":                   {"member_id", "source_meeting_id", "source_utterance_id"},
		"voice_embeddings":                {"voice_sample_id"},
		"speaker_clusters":                {"meeting_id", "assigned_participant_id"},
		"speaker_tracks":                  {"meeting_id", "asr_session_id", "automatic_participant_id", "speaker_cluster_id"},
		"speaker_track_evidence":          {"speaker_track_id", "utterance_id"},
		"agent_sessions":                  {"meeting_id", "resumed_from_session_id"},
		"agent_turns":                     {"meeting_id", "agent_session_id", "question_event_id", "answer_event_id"},
		"sync_batches":                    {"meeting_id", "agent_session_id"},
		"context_snapshots":               {"meeting_id", "agent_session_id", "agent_turn_id"},
		"minute_versions":                 {"meeting_id", "agent_turn_id", "parent_version_id"},
		"deletion_jobs":                   {"meeting_id"},
	}
	for table, columns := range requirements {
		for _, column := range columns {
			if !hasLeadingIndexColumn(t, db, table, column) {
				t.Fatalf("外键列必须有索引：table=%s column=%s", table, column)
			}
		}
	}
}

// TestSchema_ModelColumnsMatchMigration 验证 models 的显式列映射与 migration 表结构一一对应。
func TestSchema_ModelColumnsMatchMigration(t *testing.T) {
	db := openMigratedDatabase(t)
	for _, model := range []interface{ TableName() string }{
		models.AppMetadata{}, models.Settings{}, models.Member{}, models.Group{}, models.GroupMember{},
		models.MeetingNumberSequence{}, models.Meeting{}, models.MeetingParticipant{},
		models.MeetingEvent{}, models.Utterance{}, models.GuestSession{}, models.Message{}, models.Resource{}, models.Correction{}, models.CorrectionItem{},
		models.AudioAsset{}, models.ASRSession{}, models.ASRGap{}, models.GapTranscriptionAttempt{}, models.GapTranscriptionAttemptItem{},
		models.VoiceSample{}, models.VoiceEmbedding{}, models.SpeakerCluster{},
		models.SpeakerTrack{}, models.SpeakerTrackEvidence{},
		models.AgentSession{}, models.AgentTurn{}, models.SyncBatch{}, models.ContextSnapshot{}, models.MinuteVersion{}, models.DeletionJob{},
	} {
		assertModelColumnsMatchTable(t, db, model)
	}
}

// openMigratedDatabase 为单个 schema 契约测试准备隔离的最新 SQLite 数据库。
func openMigratedDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开迁移后的数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}

// insertValidMeeting 写入供约束测试复用的最小合法会议。
func insertValidMeeting(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (
		?, 'MS-20260731-0001', '测试会议', 'meetings/test', 'Asia/Shanghai',
		'preparing', 'pending', 'idle', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0
	)`, testMeetingID).Error; err != nil {
		t.Fatalf("写入合法会议失败：%v", err)
	}
}

// insertMeetingEvent 写入满足统一序列约束的最小事件。
func insertMeetingEvent(t *testing.T, db *gorm.DB, eventID string, sequence int64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO meeting_events (
		id, meeting_id, seq, kind, occurred_at, source, created_at, updated_at
	) VALUES (?, ?, ?, 'message.created', 0, 'host', 0, 0)`, eventID, testMeetingID, sequence).Error; err != nil {
		t.Fatalf("写入会议事件失败：%v", err)
	}
}

// assertStatementFails 验证数据库约束实际拒绝非法持久化值。
func assertStatementFails(t *testing.T, db *gorm.DB, statement string, values ...any) {
	t.Helper()
	if err := db.Exec(statement, values...).Error; err == nil {
		t.Fatalf("非法数据必须被约束拒绝：%s", statement)
	}
}

// hasLeadingIndexColumn 判断表是否存在以目标列为首列的索引，SQLite 自动唯一索引同样有效。
func hasLeadingIndexColumn(t *testing.T, db *gorm.DB, table string, column string) bool {
	t.Helper()
	var indexes []struct {
		Name string
	}
	if err := db.Raw(fmt.Sprintf("PRAGMA index_list(%s)", table)).Scan(&indexes).Error; err != nil {
		t.Fatalf("读取 %s 索引失败：%v", table, err)
	}
	for _, index := range indexes {
		var columns []struct {
			Sequence int
			Name     string
		}
		if err := db.Raw(fmt.Sprintf("PRAGMA index_info(%s)", index.Name)).Scan(&columns).Error; err != nil {
			t.Fatalf("读取索引 %s 列失败：%v", index.Name, err)
		}
		for _, indexedColumn := range columns {
			if indexedColumn.Sequence == 0 && indexedColumn.Name == column {
				return true
			}
		}
	}
	return false
}

// rollbackLatestMigration 仅在开发测试中调用 golang-migrate 的单步回退能力。
func rollbackLatestMigration(t *testing.T, path string) {
	t.Helper()
	source, err := iofs.New(migrations.SQLiteFiles, "sqlite")
	if err != nil {
		t.Fatalf("加载 migration 文件失败：%v", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("打开回退连接失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{})
	if err != nil {
		t.Fatalf("创建回退 driver 失败：%v", err)
	}
	runner, err := migrate.NewWithInstance("iofs", source, "sqlite3", driver)
	if err != nil {
		t.Fatalf("创建回退 runner 失败：%v", err)
	}
	t.Cleanup(func() { _, _ = runner.Close() })
	if err := runner.Steps(-1); err != nil {
		t.Fatalf("回退最新 migration 失败：%v", err)
	}
}

// assertModelColumnsMatchTable 比对单个模型的显式列标签与 SQLite table_info 输出。
func assertModelColumnsMatchTable(t *testing.T, db *gorm.DB, model interface{ TableName() string }) {
	t.Helper()
	var tableColumns []struct {
		Name string
	}
	if err := db.Raw(fmt.Sprintf("PRAGMA table_info(%s)", model.TableName())).Scan(&tableColumns).Error; err != nil {
		t.Fatalf("读取 %s 列失败：%v", model.TableName(), err)
	}
	actualColumns := make(map[string]struct{}, len(tableColumns))
	for _, column := range tableColumns {
		actualColumns[column.Name] = struct{}{}
	}
	modelType := reflect.TypeOf(model)
	for index := 0; index < modelType.NumField(); index++ {
		field := modelType.Field(index)
		columnName, found := strings.CutPrefix(field.Tag.Get("gorm"), "column:")
		if !found {
			t.Fatalf("%s.%s 未声明列映射", modelType.Name(), field.Name)
		}
		if _, found := actualColumns[columnName]; !found {
			t.Fatalf("%s.%s 映射到不存在的列 %s", modelType.Name(), field.Name, columnName)
		}
		delete(actualColumns, columnName)
	}
	if len(actualColumns) != 0 {
		t.Fatalf("%s 存在未映射列：%v", model.TableName(), actualColumns)
	}
}
