package speaker_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	speakerrepository "meet-sieve/internal/repository/speaker"
	speakerservice "meet-sieve/internal/service/speaker"

	"gorm.io/gorm"
)

// TestRepository_ListsOnlyMeetingCurrentModelCandidates 验证候选限本场正式成员、accepted 当前 embedding 和最多十人。
func TestRepository_ListsOnlyMeetingCurrentModelCandidates(t *testing.T) {
	db := prepareObserveDatabase(t)
	model := speakerdomain.ModelIdentity{ID: "model", Version: "v1", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Dimension: 2}
	for index := 0; index < 11; index++ {
		memberID := fmt.Sprintf("1%07d-0000-4000-8000-%012d", index, index)
		participantID := fmt.Sprintf("2%07d-0000-4000-8000-%012d", index, index)
		sampleID := fmt.Sprintf("3%07d-0000-4000-8000-%012d", index, index)
		embeddingID := fmt.Sprintf("4%07d-0000-4000-8000-%012d", index, index)
		if err := db.Exec("INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES (?, ?, ?, 0, 0)", memberID, fmt.Sprintf("成员%d", index), fmt.Sprintf("member-%d", index)).Error; err != nil {
			t.Fatalf("准备候选成员失败：%v", err)
		}
		if err := db.Exec("INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'member', ?, ?, 0, 0)", participantID, "11111111-1111-4111-8111-111111111111", memberID, fmt.Sprintf("成员%d", index), index).Error; err != nil {
			t.Fatalf("准备候选 participant 失败：%v", err)
		}
		if err := db.Exec(`INSERT INTO voice_samples(id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256, source_kind, environment_kind, processing_state, quality_state, created_at, updated_at) VALUES (?, ?, ?, 1000, 16000, 1, 16, 32044, 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'recorded', 'quiet', 'ready', 'accepted', 0, 0)`, sampleID, memberID, fmt.Sprintf("voice/%d.wav", index)).Error; err != nil {
			t.Fatalf("准备候选样本失败：%v", err)
		}
		if err := db.Exec("INSERT INTO voice_embeddings(id, voice_sample_id, model_id, model_version, model_sha256, dimension, embedding, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0)", embeddingID, sampleID, model.ID, model.Version, model.SHA256, model.Dimension, make([]byte, 8)).Error; err != nil {
			t.Fatalf("准备候选 embedding 失败：%v", err)
		}
	}
	candidates, err := speakerrepository.NewRepository(db).ListCandidateEmbeddings(
		context.Background(), "11111111-1111-4111-8111-111111111111", model,
	)
	if err != nil {
		t.Fatalf("查询 speaker 候选失败：%v", err)
	}
	if len(candidates) != 10 {
		t.Fatalf("候选人数必须限制为 10：got=%d", len(candidates))
	}
	if candidates[0].ParticipantID != "20000000-0000-4000-8000-000000000000" || candidates[9].ParticipantID != "20000009-0000-4000-8000-000000000009" {
		t.Fatalf("候选顺序或范围错误：first=%+v last=%+v", candidates[0], candidates[9])
	}
}

// TestObserver_PersistsIdempotentSessionScopedTracks 验证同 session/label 复用、跨 session 分离且重复 final 不重复 evidence。
func TestObserver_PersistsIdempotentSessionScopedTracks(t *testing.T) {
	db := prepareObserveDatabase(t)
	queue := make(chan string, 4)
	observer := newObserver(db, queue,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc", "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
	)

	first, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil {
		t.Fatalf("首次 Observe 失败：%v", err)
	}
	duplicate, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil || !duplicate.Duplicate || duplicate.TrackID != first.TrackID {
		t.Fatalf("重复 Observe 未复用原事实：first=%+v duplicate=%+v err=%v", first, duplicate, err)
	}
	second, err := observer.Observe(context.Background(), "88888888-8888-4888-8888-888888888888")
	if err != nil || second.TrackID != first.TrackID {
		t.Fatalf("同 session/label 必须复用 track：first=%+v second=%+v err=%v", first, second, err)
	}
	third, err := observer.Observe(context.Background(), "99999999-9999-4999-8999-999999999999")
	if err != nil || third.TrackID == first.TrackID {
		t.Fatalf("不同 session 同 label 必须分离：first=%+v third=%+v err=%v", first, third, err)
	}

	assertRowCount(t, db, "speaker_tracks", 2)
	assertRowCount(t, db, "speaker_track_evidence", 3)
	var firstDisplayNo, thirdDisplayNo int
	if err := db.Raw("SELECT display_no FROM speaker_tracks WHERE id = ?", first.TrackID).Scan(&firstDisplayNo).Error; err != nil {
		t.Fatalf("读取首个匿名编号失败：%v", err)
	}
	if err := db.Raw("SELECT display_no FROM speaker_tracks WHERE id = ?", third.TrackID).Scan(&thirdDisplayNo).Error; err != nil {
		t.Fatalf("读取跨 session 匿名编号失败：%v", err)
	}
	if firstDisplayNo != 1 || thirdDisplayNo != 2 {
		t.Fatalf("匿名 track 编号不稳定：first=%d third=%d", firstDisplayNo, thirdDisplayNo)
	}
}

// TestObserver_CreatesLocalTrackWithoutProviderSpeaker 验证无标签 final 进入本地证据链且可幂等重放。
func TestObserver_CreatesLocalTrackWithoutProviderSpeaker(t *testing.T) {
	db := prepareObserveDatabase(t)
	if err := db.Exec("UPDATE utterances SET asr_speaker_label = NULL WHERE id = ?", "77777777-7777-4777-8777-777777777777").Error; err != nil {
		t.Fatalf("准备无说话人 final 失败：%v", err)
	}
	queue := make(chan string, 1)
	observer := newObserver(db, queue,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
	)
	result, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil || result.TrackID == "" || result.Skipped || !result.Notified {
		t.Fatalf("无标签 final 应创建本地 track：result=%+v err=%v", result, err)
	}
	duplicate, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil || !duplicate.Duplicate || duplicate.TrackID != result.TrackID {
		t.Fatalf("无标签 final 重放必须幂等：result=%+v err=%v", duplicate, err)
	}
	var source, sourceUtteranceID string
	if err := db.Raw("SELECT source, source_utterance_id FROM speaker_tracks WHERE id = ?", result.TrackID).Row().Scan(&source, &sourceUtteranceID); err != nil {
		t.Fatalf("读取本地 track 来源失败：%v", err)
	}
	if source != "local_utterance" || sourceUtteranceID != "77777777-7777-4777-8777-777777777777" {
		t.Fatalf("本地 track 来源错误：source=%s utterance=%s", source, sourceUtteranceID)
	}
	assertRowCount(t, db, "speaker_tracks", 1)
	assertRowCount(t, db, "speaker_track_evidence", 1)
}

// TestObserver_QueueBackpressureKeepsSQLiteRecoveryFact 验证队列满只丢唤醒，恢复查询仍按最早 final seq 找到 track。
func TestObserver_QueueBackpressureKeepsSQLiteRecoveryFact(t *testing.T) {
	db := prepareObserveDatabase(t)
	queue := make(chan string, 1)
	queue <- "occupied"
	observer := newObserver(db, queue,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
	)
	result, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil {
		t.Fatalf("队列满时 Observe 不应失败：%v", err)
	}
	if result.Notified {
		t.Fatal("队列满时不得阻塞或伪称已通知")
	}
	repository := speakerrepository.NewRepository(db)
	evidenceIDs, err := repository.ListRecoverableEvidenceIDs(context.Background(), 10)
	if err != nil || len(evidenceIDs) != 1 || evidenceIDs[0] != result.EvidenceID {
		t.Fatalf("SQLite 恢复事实缺失：evidence=%v result=%+v err=%v", evidenceIDs, result, err)
	}
}

// TestObserver_DefersTerminalProjectionUntilRouting 验证同 label 新 final 不会在声学路由前继承终态。
func TestObserver_DefersTerminalProjectionUntilRouting(t *testing.T) {
	db := prepareObserveDatabase(t)
	observer := newObserver(db, make(chan string, 2),
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc",
	)
	first, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil {
		t.Fatalf("准备人工 cluster track 失败：%v", err)
	}
	statements := []string{
		`INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('dddddddd-dddd-4ddd-8ddd-dddddddddddd', '李四', 'li-si', 0, 0)`,
		`INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES ('eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', '11111111-1111-4111-8111-111111111111', 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', 'member', '李四', 0, 0, 0)`,
		`INSERT INTO speaker_clusters(id, meeting_id, display_no, assigned_participant_id, assignment_source, track_count, revision, created_at, updated_at) VALUES ('ffffffff-ffff-4fff-8fff-ffffffffffff', '11111111-1111-4111-8111-111111111111', 1, 'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', 'manual', 1, 1, 0, 0)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备人工 cluster 失败：%v", err)
		}
	}
	if err := db.Exec("UPDATE speaker_tracks SET state='clustered', speaker_cluster_id=? WHERE id=?", "ffffffff-ffff-4fff-8fff-ffffffffffff", first.TrackID).Error; err != nil {
		t.Fatalf("关联人工 cluster 失败：%v", err)
	}
	if _, err := observer.Observe(context.Background(), "88888888-8888-4888-8888-888888888888"); err != nil {
		t.Fatalf("Observe 新 final 失败：%v", err)
	}
	var source string
	var participantID, clusterID *string
	if err := db.Raw("SELECT current_participant_id, speaker_cluster_id, speaker_assignment_source FROM utterances WHERE id=?", "88888888-8888-4888-8888-888888888888").Row().Scan(&participantID, &clusterID, &source); err != nil {
		t.Fatalf("读取新 final 投影失败：%v", err)
	}
	if participantID != nil || clusterID != nil || source != "unassigned" {
		t.Fatalf("路由前不得继承终态：participant=%v cluster=%v source=%s", participantID, clusterID, source)
	}
}

// TestObserver_StagesMatchedTrackEvidence 验证 matched track 的新 final 先进入 pending routing。
func TestObserver_StagesMatchedTrackEvidence(t *testing.T) {
	db := prepareObserveDatabase(t)
	observer := newObserver(db, make(chan string, 2),
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc",
	)
	first, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil {
		t.Fatalf("准备已匹配 track 失败：%v", err)
	}
	statements := []string{
		`INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('dddddddd-dddd-4ddd-8ddd-dddddddddddd', '刘志勇', 'liu-zhi-yong', 0, 0)`,
		`INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES ('eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', '11111111-1111-4111-8111-111111111111', 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', 'member', '刘志勇', 0, 0, 0)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备已匹配成员失败：%v", err)
		}
	}
	if err := db.Exec("UPDATE speaker_tracks SET state='matched', automatic_participant_id=?, top_score=0.92 WHERE id=?", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", first.TrackID).Error; err != nil {
		t.Fatalf("设置 track 匹配结果失败：%v", err)
	}
	result, err := observer.Observe(context.Background(), "88888888-8888-4888-8888-888888888888")
	if err != nil || result.ProjectionChanged {
		t.Fatalf("新 final 在 continuity 路由前不得报告投影变化：result=%+v err=%v", result, err)
	}
	var routingState string
	if err := db.Raw("SELECT routing_state FROM speaker_track_evidence WHERE id=?", result.EvidenceID).Scan(&routingState).Error; err != nil {
		t.Fatalf("读取 continuity pending 失败：%v", err)
	}
	if routingState != "pending" {
		t.Fatalf("新 final 必须等待 continuity 路由：state=%s", routingState)
	}
}

// newObserver 创建使用确定性 UUID 与单 writer 事务的测试 Observe 服务。
func newObserver(db *gorm.DB, queue chan string, ids ...string) *speakerservice.Observer {
	return speakerservice.NewObserver(speakerservice.ObserverDependencies{
		Repository: speakerrepository.NewRepository(db), Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(ids...), Clock: clock.NewFixed(time.UnixMilli(1000)), Queue: queue,
	})
}

// prepareObserveDatabase 写入三个已提交 final，其中前两个共享 session/label。
func prepareObserveDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "observe.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	if err := db.Exec(`INSERT INTO meetings(id, meeting_no, subject, relative_dir, local_timezone, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES (?, 'MS-20260802-0010', 'Observe', 'meetings/observe', 'Asia/Shanghai', 'recording', 'saving', 'streaming', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0)`, "11111111-1111-4111-8111-111111111111").Error; err != nil {
		t.Fatalf("准备 Observe 会议失败：%v", err)
	}
	for _, session := range []struct {
		id      string
		started int
		start   int
		end     int
	}{
		{id: "22222222-2222-4222-8222-222222222222", started: 0, start: 0, end: 64},
		{id: "33333333-3333-4333-8333-333333333333", started: 1, start: 64, end: 96},
	} {
		if err := db.Exec(`INSERT INTO asr_sessions(id, meeting_id, provider, state, started_at, reconnect_count, transport_mode, input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at) VALUES (?, ?, 'volcano', 'streaming', ?, 0, 'seed_v1', ?, ?, ?, ?, ?)`, session.id, "11111111-1111-4111-8111-111111111111", session.started, session.start, session.end, session.end, session.started, session.started).Error; err != nil {
			t.Fatalf("准备 Observe session 失败：%v", err)
		}
	}
	fixtures := []struct {
		utteranceID string
		eventID     string
		sessionID   string
		seq         int
		start       int
	}{
		{utteranceID: "77777777-7777-4777-8777-777777777777", eventID: "44444444-4444-4444-8444-444444444444", sessionID: "22222222-2222-4222-8222-222222222222", seq: 1, start: 0},
		{utteranceID: "88888888-8888-4888-8888-888888888888", eventID: "55555555-5555-4555-8555-555555555555", sessionID: "22222222-2222-4222-8222-222222222222", seq: 2, start: 32},
		{utteranceID: "99999999-9999-4999-8999-999999999999", eventID: "66666666-6666-4666-8666-666666666666", sessionID: "33333333-3333-4333-8333-333333333333", seq: 3, start: 64},
	}
	for _, fixture := range fixtures {
		if err := db.Exec(`INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES (?, ?, ?, 'utterance.final', 0, 'asr', 'utterance', ?, 0, 0)`, fixture.eventID, "11111111-1111-4111-8111-111111111111", fixture.seq, fixture.utteranceID).Error; err != nil {
			t.Fatalf("准备 Observe event 失败：%v", err)
		}
		if err := db.Exec(`INSERT INTO utterances(id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, asr_speaker_label, speaker_assignment_source, text_revision, speaker_revision, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '文本', '文本', ?, ?, 'speaker-1', 'unassigned', 1, 1, 0, 0)`, fixture.utteranceID, "11111111-1111-4111-8111-111111111111", fixture.eventID, fixture.sessionID, "result-"+fixture.utteranceID, fixture.start, fixture.start+32).Error; err != nil {
			t.Fatalf("准备 Observe utterance 失败：%v", err)
		}
	}
	return db
}

// assertRowCount 验证指定表的持久事实数量。
func assertRowCount(t *testing.T, db *gorm.DB, table string, want int64) {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil || count != want {
		t.Fatalf("表行数错误：table=%s got=%d want=%d err=%v", table, count, want, err)
	}
}
