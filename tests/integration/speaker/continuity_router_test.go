package speaker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	speakerrepository "meet-sieve/internal/repository/speaker"
	speakerservice "meet-sieve/internal/service/speaker"

	"gorm.io/gorm"
)

type routingAudioReader struct{}

// Read 用样本范围稳定区分 A/B/A，避免单元编排测试依赖真实模型。
func (routingAudioReader) Read(_ context.Context, _ string, start int64, end int64) ([]int16, error) {
	value := int16(1000)
	if start == 32 {
		value = -1000
	}
	result := make([]int16, end-start)
	for index := range result {
		result[index] = value
	}
	return result, nil
}

type routingFailingAudioReader struct{}

// Read 模拟音频证据读取失败，用于验证持久化失败与恢复链路。
func (routingFailingAudioReader) Read(context.Context, string, int64, int64) ([]int16, error) {
	return nil, errors.New("audio unavailable")
}

type routingEncoder struct{ info port.ModelInfo }

// ModelInfo 返回测试 continuity profile 绑定的模型身份。
func (encoder routingEncoder) ModelInfo() port.ModelInfo { return encoder.info }

// Encode 把正负 PCM 映射成两个正交声学身份。
func (encoder routingEncoder) Encode(_ context.Context, audio port.AudioPCM) (port.Embedding, error) {
	if audio.Samples[0] < 0 {
		return port.Embedding{0, 1}, nil
	}
	return port.Embedding{1, 0}, nil
}

type routedEvent struct {
	trackID           string
	projectionChanged bool
}

// TestContinuityRouter_SplitsAndReturnsToMatchedSegment 验证同 label 的 A→B→A 形成两段并回到 A。
func TestContinuityRouter_SplitsAndReturnsToMatchedSegment(t *testing.T) {
	db := prepareObserveDatabase(t)
	if err := db.Exec(`UPDATE utterances SET asr_session_id='22222222-2222-4222-8222-222222222222'
WHERE id='99999999-9999-4999-8999-999999999999'`).Error; err != nil {
		t.Fatalf("准备 A-B-A 同通道失败：%v", err)
	}
	queue := make(chan string, 3)
	observer := newObserver(db, queue,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc", "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
	)
	results := make([]speakerservice.ObserveResult, 0, 3)
	for _, utteranceID := range []string{
		"77777777-7777-4777-8777-777777777777",
		"88888888-8888-4888-8888-888888888888",
		"99999999-9999-4999-8999-999999999999",
	} {
		result, err := observer.Observe(context.Background(), utteranceID)
		if err != nil {
			t.Fatalf("Observe A-B-A 失败：%v", err)
		}
		results = append(results, result)
	}
	events := make([]routedEvent, 0, 3)
	router := newTestContinuityRouter(db, &events)
	if err := router.Process(context.Background(), results[0].EvidenceID, false); err != nil {
		t.Fatalf("路由首个 A 失败：%v", err)
	}
	prepareMatchedSegment(t, db, results[0].TrackID)
	if err := router.Process(context.Background(), results[1].EvidenceID, false); err != nil {
		t.Fatalf("路由 B 失败：%v", err)
	}
	if err := router.Process(context.Background(), results[2].EvidenceID, false); err != nil {
		t.Fatalf("路由返回 A 失败：%v", err)
	}

	var trackIDs []string
	if err := db.Raw(`SELECT speaker_track_id FROM utterances
WHERE id IN ('77777777-7777-4777-8777-777777777777','88888888-8888-4888-8888-888888888888','99999999-9999-4999-8999-999999999999')
ORDER BY start_sample`).Scan(&trackIDs).Error; err != nil {
		t.Fatalf("读取 A-B-A 路由结果失败：%v", err)
	}
	if len(trackIDs) != 3 || trackIDs[0] == trackIDs[1] || trackIDs[0] != trackIDs[2] {
		t.Fatalf("A-B-A segment 错误：%v", trackIDs)
	}
	assertRowCount(t, db, "speaker_tracks", 2)
	if len(events) != 3 || !events[2].projectionChanged {
		t.Fatalf("返回 matched A 后必须报告同 seq 投影变化：%+v", events)
	}
	var participantID, source string
	if err := db.Raw(`SELECT current_participant_id, speaker_assignment_source FROM utterances
WHERE id='99999999-9999-4999-8999-999999999999'`).Row().Scan(&participantID, &source); err != nil {
		t.Fatalf("读取返回 A 的成员投影失败：%v", err)
	}
	if participantID != "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee" || source != "automatic_member" {
		t.Fatalf("返回 A 未继承成员：participant=%s source=%s", participantID, source)
	}
}

// TestContinuityRouter_PersistsAndRetriesFailure 验证稳定错误码不会吞掉任务，恢复后可正常完成路由。
func TestContinuityRouter_PersistsAndRetriesFailure(t *testing.T) {
	db := prepareObserveDatabase(t)
	queue := make(chan string, 1)
	observer := newObserver(db, queue,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc", "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
	)
	result, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil {
		t.Fatalf("准备 continuity evidence 失败：%v", err)
	}
	events := make([]routedEvent, 0, 1)
	failingRouter := newTestContinuityRouterWithAudio(db, &events, routingFailingAudioReader{})
	if err := failingRouter.Process(context.Background(), result.EvidenceID, false); err == nil {
		t.Fatal("音频失败必须返回原始错误")
	}
	var state string
	var errorCode *string
	if err := db.Raw("SELECT routing_state, routing_error_code FROM speaker_track_evidence WHERE id=?", result.EvidenceID).
		Row().Scan(&state, &errorCode); err != nil {
		t.Fatalf("读取 continuity 失败状态失败：%v", err)
	}
	if state != "failed" || errorCode == nil || *errorCode != "continuity_audio_read_failed" {
		t.Fatalf("continuity 失败事实错误：state=%s error=%v", state, errorCode)
	}
	ids, err := speakerrepository.NewRepository(db).ListRecoverableEvidenceIDs(context.Background(), 10)
	if err != nil || len(ids) != 1 || ids[0] != result.EvidenceID {
		t.Fatalf("失败 evidence 必须可恢复：ids=%v err=%v", ids, err)
	}
	if err := newTestContinuityRouter(db, &events).Process(context.Background(), result.EvidenceID, false); err != nil {
		t.Fatalf("重试 continuity evidence 失败：%v", err)
	}
	if err := db.Raw("SELECT routing_state, routing_error_code FROM speaker_track_evidence WHERE id=?", result.EvidenceID).
		Row().Scan(&state, &errorCode); err != nil {
		t.Fatalf("读取 continuity 恢复状态失败：%v", err)
	}
	if state != "routed" || errorCode != nil {
		t.Fatalf("continuity 恢复后状态错误：state=%s error=%v", state, errorCode)
	}
}

// newTestContinuityRouter 构造固定二维 embedding 的 continuity router。
func newTestContinuityRouter(db *gorm.DB, events *[]routedEvent) *speakerservice.ContinuityRouter {
	return newTestContinuityRouterWithAudio(db, events, routingAudioReader{})
}

// newTestContinuityRouterWithAudio 使用指定音频读取器构造测试 router。
func newTestContinuityRouterWithAudio(db *gorm.DB, events *[]routedEvent, audio speakerservice.EvidenceAudioReader) *speakerservice.ContinuityRouter {
	profile := speakerdomain.MatchingProfile{
		SchemaVersion: 2, ProfileID: "continuity-test",
		Model:          speakerdomain.ModelIdentity{ID: "model", Version: "v1", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Dimension: 2},
		Evidence:       speakerdomain.EvidenceProfile{MinEvidenceMS: 1, TargetEvidenceMS: 2},
		Identity:       speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1},
		UnknownCluster: speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1},
		Continuity:     &speakerdomain.ContinuityProfile{WindowMS: 1000, HopMS: 1000, MinScore: 0.8, MinMargin: 0.2},
	}
	return speakerservice.NewContinuityRouter(speakerservice.ContinuityRouterDependencies{
		Repository: speakerrepository.NewRepository(db), Transactions: database.NewTransactionManager(db),
		Audio: audio, Encoder: routingEncoder{info: port.ModelInfo{ID: "model", Version: "v1", SHA256: profile.Model.SHA256, Dimension: 2}},
		Profile: profile, IDs: identity.NewFixedGenerator("f1f1f1f1-f1f1-41f1-81f1-f1f1f1f1f1f1"), Clock: clock.NewFixed(time.UnixMilli(2000)),
		OnRouted: func(_ string, trackID string, changed bool) {
			*events = append(*events, routedEvent{trackID: trackID, projectionChanged: changed})
		},
	})
}

// prepareMatchedSegment 把首个 A segment 设置成已匹配成员，用于验证新 final 的延迟继承。
func prepareMatchedSegment(t *testing.T, db *gorm.DB, trackID string) {
	t.Helper()
	statements := []string{
		`INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('dddddddd-dddd-4ddd-8ddd-dddddddddddd', '成员A', 'member-a', 0, 0)`,
		`INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES ('eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', '11111111-1111-4111-8111-111111111111', 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', 'member', '成员A', 0, 0, 0)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备 matched A 失败：%v", err)
		}
	}
	if err := db.Exec("UPDATE speaker_tracks SET state='matched', automatic_participant_id=?, top_score=0.92 WHERE id=?", "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", trackID).Error; err != nil {
		t.Fatalf("设置 matched A 失败：%v", err)
	}
}
