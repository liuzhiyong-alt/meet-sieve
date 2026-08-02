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
	voiceservice "meet-sieve/internal/service/voice"

	"gorm.io/gorm"
)

type rangeAudioReader struct{}

type runnerVoiceEncoder struct {
	info      port.ModelInfo
	embedding port.Embedding
	err       error
}

// ModelInfo 返回测试约定的模型身份。
func (encoder *runnerVoiceEncoder) ModelInfo() port.ModelInfo { return encoder.info }

// Encode 返回固定 embedding，使测试只覆盖 Runner 编排与事务投影。
func (encoder *runnerVoiceEncoder) Encode(context.Context, port.AudioPCM) (port.Embedding, error) {
	if encoder.err != nil {
		return nil, encoder.err
	}
	return append(port.Embedding(nil), encoder.embedding...), nil
}

type pendingAudioReader struct{}

// Read 模拟 rolling/安全录音均尚不可读。
func (pendingAudioReader) Read(context.Context, string, int64, int64) ([]int16, error) {
	return nil, speakerservice.ErrAudioEvidencePending
}

// Read 返回固定正向 PCM，范围只用于满足 evidence 时长。
func (rangeAudioReader) Read(_ context.Context, _ string, start int64, end int64) ([]int16, error) {
	result := make([]int16, end-start)
	for index := range result {
		result[index] = int16(index + 1)
	}
	return result, nil
}

// TestRunner_CommitsMatchedProjectionAndAttributionEvent 验证成员决策、utterance 投影和自动事件同事务提交。
func TestRunner_CommitsMatchedProjectionAndAttributionEvent(t *testing.T) {
	db := prepareObserveDatabase(t)
	prepareRunnerCandidate(t, db)
	trackID := observeRunnerUtterance(t, db)
	changed := make(chan string, 1)
	runner := newTestRunner(db, changed)

	if err := runner.Process(context.Background(), trackID, false); err != nil {
		t.Fatalf("处理 matched track 失败：%v", err)
	}
	var state, participantID, source string
	if err := db.Raw("SELECT state, automatic_participant_id FROM speaker_tracks WHERE id=?", trackID).Row().Scan(&state, &participantID); err != nil {
		t.Fatalf("读取 matched track 失败：%v", err)
	}
	if err := db.Raw("SELECT current_participant_id, speaker_assignment_source FROM utterances WHERE id='77777777-7777-4777-8777-777777777777'").Row().Scan(&participantID, &source); err != nil {
		t.Fatalf("读取 matched utterance 失败：%v", err)
	}
	if state != "matched" || participantID != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" || source != "automatic_member" {
		t.Fatalf("matched 投影错误：state=%s participant=%s source=%s", state, participantID, source)
	}
	assertAttributedEventAndCallback(t, db, changed, trackID)
}

// TestRunner_ClustersRejectedTrackWithoutStoppingPipeline 验证无成员候选时仍生成 unknown 且发布同类刷新通知。
func TestRunner_ClustersRejectedTrackWithoutStoppingPipeline(t *testing.T) {
	db := prepareObserveDatabase(t)
	trackID := observeRunnerUtterance(t, db)
	changed := make(chan string, 1)
	runner := newTestRunner(db, changed)

	if err := runner.Process(context.Background(), trackID, false); err != nil {
		t.Fatalf("处理 unknown track 失败：%v", err)
	}
	var state, clusterID string
	if err := db.Raw("SELECT state, speaker_cluster_id FROM speaker_tracks WHERE id=?", trackID).Row().Scan(&state, &clusterID); err != nil {
		t.Fatalf("读取 clustered track 失败：%v", err)
	}
	if state != "clustered" || clusterID == "" {
		t.Fatalf("unknown 投影错误：state=%s cluster=%s", state, clusterID)
	}
	assertAttributedEventAndCallback(t, db, changed, trackID)
}

// TestRunner_PersistsPendingWithoutCallingEncoder 验证音频未就绪时只更新可恢复状态。
func TestRunner_PersistsPendingWithoutCallingEncoder(t *testing.T) {
	db := prepareObserveDatabase(t)
	trackID := observeRunnerUtterance(t, db)
	profile := runnerTestProfile()
	runner := newTestRunnerWithComponents(db, make(chan string, 1), pendingAudioReader{}, &runnerVoiceEncoder{
		info: port.ModelInfo{ID: profile.Model.ID, Version: profile.Model.Version, SHA256: profile.Model.SHA256, Dimension: 2},
		err:  errors.New("不得调用 encoder"),
	}, profile)
	if err := runner.Process(context.Background(), trackID, false); err != nil {
		t.Fatalf("pending track 不应失败：%v", err)
	}
	var state string
	if err := db.Raw("SELECT state FROM speaker_tracks WHERE id=?", trackID).Scan(&state).Error; err != nil || state != "pending" {
		t.Fatalf("pending 状态错误：state=%s err=%v", state, err)
	}
}

// TestRunner_PersistsEncoderFailureCode 验证模型错误不会把 track 留在伪 collecting 状态。
func TestRunner_PersistsEncoderFailureCode(t *testing.T) {
	db := prepareObserveDatabase(t)
	trackID := observeRunnerUtterance(t, db)
	profile := runnerTestProfile()
	runner := newTestRunnerWithComponents(db, make(chan string, 1), rangeAudioReader{}, &runnerVoiceEncoder{
		info: port.ModelInfo{ID: profile.Model.ID, Version: profile.Model.Version, SHA256: profile.Model.SHA256, Dimension: 2},
		err:  errors.New("encoder failed"),
	}, profile)
	if err := runner.Process(context.Background(), trackID, false); err == nil {
		t.Fatal("encoder 失败必须返回错误")
	}
	var state, code string
	if err := db.Raw("SELECT state, last_error_code FROM speaker_tracks WHERE id=?", trackID).Row().Scan(&state, &code); err != nil {
		t.Fatalf("读取失败状态错误：%v", err)
	}
	if state != "failed" || code != "SPEAKER_EMBEDDING_FAILED" {
		t.Fatalf("encoder 失败状态错误：state=%s code=%s", state, code)
	}
}

// observeRunnerUtterance 为首条 final 建立 collecting track/evidence。
func observeRunnerUtterance(t *testing.T, db *gorm.DB) string {
	t.Helper()
	queue := make(chan string, 1)
	observer := newObserver(db, queue,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "acacacac-acac-4cac-8cac-acacacacacac",
	)
	result, err := observer.Observe(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil {
		t.Fatalf("准备 runner track 失败：%v", err)
	}
	return result.TrackID
}

// prepareRunnerCandidate 写入本场唯一正式成员的当前模型 accepted embedding。
func prepareRunnerCandidate(t *testing.T, db *gorm.DB) {
	t.Helper()
	blob, err := voiceservice.EncodeEmbeddingBlob([]float32{1, 0}, 2)
	if err != nil {
		t.Fatalf("编码 runner 候选失败：%v", err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{query: "INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('cccccccc-cccc-4ccc-8ccc-cccccccccccc', '张三', 'zhang-san', 0, 0)"},
		{query: "INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES ('bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb', '11111111-1111-4111-8111-111111111111', 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', 'member', '张三', 0, 0, 0)"},
		{query: `INSERT INTO voice_samples(id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256, source_kind, environment_kind, processing_state, quality_state, created_at, updated_at) VALUES ('dddddddd-dddd-4ddd-8ddd-dddddddddddd', 'cccccccc-cccc-4ccc-8ccc-cccccccccccc', 'voice/runner.wav', 1000, 16000, 1, 16, 32044, 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', 'recorded', 'quiet', 'ready', 'accepted', 0, 0)`},
		{query: `INSERT INTO voice_embeddings(id, voice_sample_id, model_id, model_version, model_sha256, dimension, embedding, created_at, updated_at) VALUES ('eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee', 'dddddddd-dddd-4ddd-8ddd-dddddddddddd', 'model', 'v1', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 2, ?, 0, 0)`, args: []any{blob}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("准备 runner 候选失败：%v", err)
		}
	}
}

// newTestRunner 装配真实 repository/transaction/matcher/clusterer 与受控 encoder/audio。
func newTestRunner(db *gorm.DB, changed chan string) *speakerservice.Runner {
	profile := runnerTestProfile()
	encoder := &runnerVoiceEncoder{
		info:      port.ModelInfo{ID: profile.Model.ID, Version: profile.Model.Version, SHA256: profile.Model.SHA256, Dimension: 2},
		embedding: port.Embedding{1, 0},
	}
	return newTestRunnerWithComponents(db, changed, rangeAudioReader{}, encoder, profile)
}

// newTestRunnerWithComponents 允许状态机测试替换音频、encoder 和 profile。
func newTestRunnerWithComponents(db *gorm.DB, changed chan string, reader speakerservice.EvidenceAudioReader, encoder port.VoiceEncoder, profile speakerdomain.MatchingProfile) *speakerservice.Runner {
	repository := speakerrepository.NewRepository(db)
	transactions := database.NewTransactionManager(db)
	unknownIDs := []string{"f1f1f1f1-f1f1-41f1-81f1-f1f1f1f1f1f1", "f2f2f2f2-f2f2-42f2-82f2-f2f2f2f2f2f2"}
	unknown := speakerservice.NewUnknownAssigner(speakerservice.UnknownAssignerDependencies{
		Repository: repository, Transactions: transactions, IDs: identity.NewFixedGenerator(unknownIDs...),
		Clock: clock.NewFixed(time.UnixMilli(3000)),
	})
	return speakerservice.NewRunner(speakerservice.RunnerDependencies{
		Repository: repository, Transactions: transactions,
		Evidence: speakerservice.NewEvidenceBuilder(reader), Encoder: encoder, Profile: profile,
		Unknown: unknown, IDs: identity.NewFixedGenerator("f3f3f3f3-f3f3-43f3-83f3-f3f3f3f3f3f3"),
		Clock: clock.NewFixed(time.UnixMilli(3000)), OnChanged: func(_ string, changedTrackID string) { changed <- changedTrackID },
	})
}

// runnerTestProfile 返回 runner 状态机的小向量临时档案。
func runnerTestProfile() speakerdomain.MatchingProfile {
	return speakerdomain.MatchingProfile{
		ProfileID: "runner-test", Model: speakerdomain.ModelIdentity{ID: "model", Version: "v1", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Dimension: 2},
		Evidence:       speakerdomain.EvidenceProfile{MinEvidenceMS: 1, TargetEvidenceMS: 2},
		Identity:       speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1},
		UnknownCluster: speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1},
	}
}

// assertAttributedEventAndCallback 验证成功提交后才发布刷新通知，且事件不生成空正文类型。
func assertAttributedEventAndCallback(t *testing.T, db *gorm.DB, changed chan string, trackID string) {
	t.Helper()
	var count int64
	if err := db.Table("meeting_events").Where("kind='speaker.attributed' AND entity_id=?", trackID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("自动 attribution 事件错误：count=%d err=%v", count, err)
	}
	select {
	case got := <-changed:
		if got != trackID {
			t.Fatalf("刷新通知 track 错误：%s", got)
		}
	default:
		t.Fatal("提交后必须发布 speaker changed")
	}
}
