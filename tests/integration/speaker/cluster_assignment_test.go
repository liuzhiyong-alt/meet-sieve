package speaker_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	speakerrepository "meet-sieve/internal/repository/speaker"
	speakerservice "meet-sieve/internal/service/speaker"
	voiceservice "meet-sieve/internal/service/voice"

	"gorm.io/gorm"
)

// TestUnknownAssigner_CreatesJoinsAndKeepsStableDisplayNumber 验证单 writer 内创建、合并和幂等归属。
func TestUnknownAssigner_CreatesJoinsAndKeepsStableDisplayNumber(t *testing.T) {
	db := prepareClusterDatabase(t)
	profile := clusterTestProfile()
	firstTrack := insertClusterTrack(t, db, 1, []float32{1, 0})
	secondTrack := insertClusterTrack(t, db, 2, []float32{0.99, 0.1})
	assigner := newUnknownAssigner(db,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"abababab-abab-4bab-8bab-abababababab",
		"acacacac-acac-4cac-8cac-acacacacacac",
	)

	first, err := assigner.Assign(context.Background(), firstTrack, profile)
	if err != nil || first.Cluster.DisplayNo != 1 || first.Duplicate {
		t.Fatalf("首次 unknown cluster 创建失败：result=%+v err=%v", first, err)
	}
	second, err := assigner.Assign(context.Background(), secondTrack, profile)
	if err != nil || second.Cluster.ID != first.Cluster.ID || second.Cluster.TrackCount != 2 {
		t.Fatalf("相似 track 应加入已有 cluster：first=%+v second=%+v err=%v", first, second, err)
	}
	repeated, err := assigner.Assign(context.Background(), firstTrack, profile)
	if err != nil || !repeated.Duplicate || repeated.Cluster.TrackCount != 2 {
		t.Fatalf("重复归属必须幂等：result=%+v err=%v", repeated, err)
	}
	var attributedEvents int64
	if err := db.Table("meeting_events").Where("kind = 'speaker.attributed'").Count(&attributedEvents).Error; err != nil || attributedEvents != 2 {
		t.Fatalf("每个新归属必须恰好一个自动事件：count=%d err=%v", attributedEvents, err)
	}
}

// TestUnknownAssigner_ExcludesManualClusterWithoutRenumbering 验证人工 cluster 不再自动合并且新编号取历史最大值加一。
func TestUnknownAssigner_ExcludesManualClusterWithoutRenumbering(t *testing.T) {
	db := prepareClusterDatabase(t)
	profile := clusterTestProfile()
	firstTrack := insertClusterTrack(t, db, 1, []float32{1, 0})
	first, err := newUnknownAssigner(db,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "abababab-abab-4bab-8bab-abababababab",
	).Assign(context.Background(), firstTrack, profile)
	if err != nil {
		t.Fatalf("准备人工 cluster 失败：%v", err)
	}
	if err := db.Exec("UPDATE speaker_clusters SET assignment_source='manual' WHERE id=?", first.Cluster.ID).Error; err != nil {
		t.Fatalf("标记人工 cluster 失败：%v", err)
	}
	secondTrack := insertClusterTrack(t, db, 2, []float32{1, 0})
	second, err := newUnknownAssigner(db,
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "bcbcbcbc-bcbc-4bcb-8bcb-bcbcbcbcbcbc",
	).Assign(context.Background(), secondTrack, profile)
	if err != nil || second.Cluster.ID == first.Cluster.ID || second.Cluster.DisplayNo != 2 {
		t.Fatalf("人工 cluster 排除或编号错误：first=%+v second=%+v err=%v", first, second, err)
	}
}

// newUnknownAssigner 创建使用确定性 cluster ID 的单 writer 测试服务。
func newUnknownAssigner(db *gorm.DB, ids ...string) *speakerservice.UnknownAssigner {
	return speakerservice.NewUnknownAssigner(speakerservice.UnknownAssignerDependencies{
		Repository: speakerrepository.NewRepository(db), Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(ids...), Clock: clock.NewFixed(time.UnixMilli(2000)),
	})
}

// clusterTestProfile 返回仅用于小向量状态机测试的临时档案。
func clusterTestProfile() speakerdomain.MatchingProfile {
	return speakerdomain.MatchingProfile{
		ProfileID:      "test-profile",
		Model:          speakerdomain.ModelIdentity{ID: "model", Version: "v1", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Dimension: 2},
		UnknownCluster: speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1},
	}
}

// prepareClusterDatabase 创建 unknown 聚类所需的会议和 ASR session。
func prepareClusterDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cluster.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 cluster 数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	if err := db.Exec(`INSERT INTO meetings(id, meeting_no, subject, relative_dir, local_timezone, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES ('11111111-1111-4111-8111-111111111111', 'MS-20260802-0020', 'Cluster', 'meetings/cluster', 'Asia/Shanghai', 'recording', 'saving', 'streaming', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0)`).Error; err != nil {
		t.Fatalf("准备 cluster 会议失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO asr_sessions(id, meeting_id, provider, state, started_at, reconnect_count, transport_mode, input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at) VALUES ('22222222-2222-4222-8222-222222222222', '11111111-1111-4111-8111-111111111111', 'volcano', 'streaming', 0, 0, 'seed_v1', 0, 64, 64, 0, 0)`).Error; err != nil {
		t.Fatalf("准备 cluster session 失败：%v", err)
	}
	return db
}

// insertClusterTrack 写入带当前模型 embedding 和可排序 final evidence 的待聚类 track。
func insertClusterTrack(t *testing.T, db *gorm.DB, index int, embedding []float32) string {
	t.Helper()
	trackID := []string{"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"}[index-1]
	eventID := []string{"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666"}[index-1]
	utteranceID := []string{"77777777-7777-4777-8777-777777777777", "88888888-8888-4888-8888-888888888888"}[index-1]
	evidenceID := []string{"99999999-9999-4999-8999-999999999999", "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}[index-1]
	blob, err := voiceservice.EncodeEmbeddingBlob(embedding, 2)
	if err != nil {
		t.Fatalf("编码测试 track embedding 失败：%v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{query: `INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES (?, '11111111-1111-4111-8111-111111111111', (SELECT COALESCE(MAX(seq), 0) + 1 FROM meeting_events WHERE meeting_id='11111111-1111-4111-8111-111111111111'), 'utterance.final', 0, 'asr', 'utterance', ?, 0, 0)`, args: []any{eventID, utteranceID}},
		{query: `INSERT INTO speaker_tracks(id, meeting_id, asr_session_id, source, asr_speaker_label, provider_segment_no, state, evidence_duration_ms, model_id, model_version, model_sha256, dimension, embedding, profile_id, routing_revision, revision, created_at, updated_at) VALUES (?, '11111111-1111-4111-8111-111111111111', '22222222-2222-4222-8222-222222222222', 'provider_label', ?, ?, 'ambiguous', 1000, 'model', 'v1', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 2, ?, 'test-profile', 1, 1, 0, 0)`, args: []any{trackID, "speaker-" + string(rune('0'+index)), index, blob}},
		{query: `INSERT INTO utterances(id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, asr_speaker_label, speaker_track_id, speaker_assignment_source, text_revision, speaker_revision, created_at, updated_at) VALUES (?, '11111111-1111-4111-8111-111111111111', ?, '22222222-2222-4222-8222-222222222222', ?, '文本', '文本', ?, ?, ?, ?, 'unassigned', 1, 1, 0, 0)`, args: []any{utteranceID, eventID, "result-" + trackID, (index - 1) * 32, index * 32, "speaker-" + string(rune('0'+index)), trackID}},
		{query: `INSERT INTO speaker_track_evidence(id, speaker_track_id, utterance_id, evidence_order, overlap_risk, included, created_at, updated_at) VALUES (?, ?, ?, 1, 0, 1, 0, 0)`, args: []any{evidenceID, trackID, utteranceID}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("准备 cluster track 失败：%v", err)
		}
	}
	return trackID
}
