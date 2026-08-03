package gap_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"meet-sieve/internal/infra/database"
	gaprepository "meet-sieve/internal/repository/gap"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	testMeetingID = "91919191-9191-4919-8919-919191919191"
	testEventID   = "92929292-9292-4929-8929-929292929292"
	testGapID     = "93939393-9393-4939-8939-939393939393"
	testAudioID   = "94949494-9494-4949-8949-949494949494"
)

// TestClaimGapAttempt_ConcurrentClaimsHaveSingleWinner 验证同一缺口并发 claim 只有一个成功。
func TestClaimGapAttempt_ConcurrentClaimsHaveSingleWinner(t *testing.T) {
	db := openGapDatabase(t)
	repository := gaprepository.NewRepository(db, database.NewTransactionManager(db))
	inputs := []gaprepository.ClaimAttemptInput{
		newClaimInput("95959595-9595-4959-8959-959595959595", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		newClaimInput("96969696-9696-4969-8969-969696969696", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	}

	start := make(chan struct{})
	results := make(chan error, len(inputs))
	var wait sync.WaitGroup
	for _, input := range inputs {
		input := input
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- repository.ClaimGapAttempt(context.Background(), input)
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var successes int
	var conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, gaprepository.ErrConflict):
			conflicts++
		default:
			t.Fatalf("claim 返回非预期错误：%v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("并发 claim 结果错误：successes=%d conflicts=%d", successes, conflicts)
	}

	var gap models.ASRGap
	if err := db.Where("id = ?", testGapID).Take(&gap).Error; err != nil || gap.State != "processing" || gap.AttemptCount != 1 {
		t.Fatalf("gap claim 投影错误：gap=%#v err=%v", gap, err)
	}
	var attempts int64
	if err := db.Model(&models.GapTranscriptionAttempt{}).Count(&attempts).Error; err != nil || attempts != 1 {
		t.Fatalf("attempt 行数错误：count=%d err=%v", attempts, err)
	}
	var items int64
	if err := db.Model(&models.GapTranscriptionAttemptItem{}).Count(&items).Error; err != nil || items != 1 {
		t.Fatalf("attempt item 行数错误：count=%d err=%v", items, err)
	}
}

// newClaimInput 构造一份合法、彼此仅身份不同的 claim 输入。
func newClaimInput(id string, requestHash string) gaprepository.ClaimAttemptInput {
	startedAt := int64(10)
	return gaprepository.ClaimAttemptInput{
		Attempt: models.GapTranscriptionAttempt{
			ID: id, MeetingID: testMeetingID, AudioAssetID: testAudioID, Provider: "volcano",
			ProviderRequestID: id, CoreStartSample: 0, CoreEndSample: 16000,
			AudioStartSample: 0, AudioEndSample: 16000, State: "running", AttemptNo: 1,
			RequestSHA256: requestHash, StartedAt: &startedAt, CreatedAt: 10, UpdatedAt: 10,
		},
		GapIDs: []string{testGapID},
	}
}

// openGapDatabase 创建并填充一个可被会后处理器 claim 的隔离数据库。
func openGapDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gap.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移 gap 测试数据库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 gap 测试数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	statements := []string{
		`INSERT INTO meetings (
			id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at,
			lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state,
			minute_state, lan_state, created_at, updated_at
		) VALUES ('91919191-9191-4919-8919-919191919191', 'MS-20260803-0001', 'Gap 测试',
			'meetings/gap', 'Asia/Shanghai', 0, 1, 'ended', 'saved', 'stopped', 'pending',
			'unchecked', 'not_generated', 'stopped', 0, 1)`,
		`INSERT INTO meeting_events (
			id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at
		) VALUES ('92929292-9292-4929-8929-929292929292', '91919191-9191-4919-8919-919191919191',
			1, 'asr.gap', 0, 'system', 'asr_gap', '93939393-9393-4939-8939-939393939393', 0, 0)`,
		`INSERT INTO audio_assets (
			id, meeting_id, kind, sequence_no, relative_path, start_sample, end_sample, sample_rate,
			bit_depth, channels, size_bytes, sha256, state, created_at, updated_at
		) VALUES ('94949494-9494-4949-8949-949494949494', '91919191-9191-4919-8919-919191919191',
			'gap', 1, 'audio/gaps/test.wav', 0, 16000, 16000, 16, 1, 32044,
			'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'ready', 0, 0)`,
		`INSERT INTO asr_gaps (
			id, meeting_id, event_id, audio_asset_id, start_sample, end_sample, reason, origin_key,
			state, attempt_count, created_at, updated_at
		) VALUES ('93939393-9393-4939-8939-939393939393', '91919191-9191-4919-8919-919191919191',
			'92929292-9292-4929-8929-929292929292', '94949494-9494-4949-8949-949494949494',
			0, 16000, 'record_only', 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
			'pending', 0, 0, 0)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备 gap 测试事实失败：%v", err)
		}
	}
	return db
}
