package gap

import (
	"context"
	"errors"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	gaprepository "meet-sieve/internal/repository/gap"
	"meet-sieve/models"
)

// TestCompensationCommitter_WholeAttemptConflictsAndFlushes 验证任一重叠使整次 attempt 只保存冲突。
func TestCompensationCommitter_WholeAttemptConflictsAndFlushes(t *testing.T) {
	repository := &fakeCompensationRepository{overlaps: []gaprepository.OverlapRow{{ID: "existing", StartSample: 100, EndSample: 200}}}
	flusher := &fakeRawRecordFlusher{}
	committer := newTestCommitter(repository, flusher)
	err := committer.Commit(context.Background(), testAttempt(), port.FileTranscriptionResult{
		Segments: []port.FileTranscriptionSegment{{Text: "候选", StartSample: 0, EndSample: 1000}},
	})
	if err != nil {
		t.Fatalf("提交冲突结果失败：%v", err)
	}
	if repository.conflictCalls != 1 || repository.compensationCalls != 0 || flusher.calls != 1 {
		t.Fatalf("冲突分支调用错误：repository=%+v flusher=%+v", repository, flusher)
	}
}

// TestCompensationCommitter_NoSpeechPersistsBeforeProjectionFailure 验证投影失败不回滚静音事实。
func TestCompensationCommitter_NoSpeechPersistsBeforeProjectionFailure(t *testing.T) {
	repository := &fakeCompensationRepository{gapIDs: []string{"gap-id"}}
	flusher := &fakeRawRecordFlusher{err: errors.New("disk unavailable")}
	committer := newTestCommitter(repository, flusher)
	err := committer.Commit(context.Background(), testAttempt(), port.FileTranscriptionResult{NoSpeech: true})
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeRawRecordRefreshFailed.ErrorCode {
		t.Fatalf("投影失败错误语义不正确：%v", err)
	}
	if repository.noSpeechCalls != 1 || flusher.calls != 1 {
		t.Fatalf("静音事实必须先提交：repository=%+v flusher=%+v", repository, flusher)
	}
}

// newTestCommitter 创建使用固定身份和时钟的补偿提交器。
func newTestCommitter(repository CompensationRepository, flusher RawRecordFlusher) *CompensationCommitter {
	return NewCompensationCommitter(CommitterDependencies{
		Repository: repository, RawRecord: flusher,
		IDs: identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		),
		Clock: clock.NewFixed(time.UnixMilli(10)),
	})
}

// testAttempt 返回 service 测试所需的活动 attempt。
func testAttempt() models.GapTranscriptionAttempt {
	return models.GapTranscriptionAttempt{
		ID: "attempt", MeetingID: "meeting", ProviderRequestID: "request",
		CoreStartSample: 0, CoreEndSample: 1000, AudioStartSample: 0, AudioEndSample: 1000,
	}
}

type fakeCompensationRepository struct {
	gapIDs            []string
	overlaps          []gaprepository.OverlapRow
	compensationCalls int
	noSpeechCalls     int
	conflictCalls     int
}

// ListAttemptGapIDs 返回静音事件关联的 gap。
func (repository *fakeCompensationRepository) ListAttemptGapIDs(context.Context, string) ([]string, error) {
	return repository.gapIDs, nil
}

// ListOverlaps 返回预设的当前重叠事实。
func (repository *fakeCompensationRepository) ListOverlaps(context.Context, string, []models.Utterance) ([]gaprepository.OverlapRow, error) {
	return repository.overlaps, nil
}

// CommitCompensation 记录无冲突提交。
func (repository *fakeCompensationRepository) CommitCompensation(context.Context, gaprepository.CompensationInput) error {
	repository.compensationCalls++
	return nil
}

// CommitNoSpeechCompensation 记录静音提交。
func (repository *fakeCompensationRepository) CommitNoSpeechCompensation(context.Context, gaprepository.NoSpeechInput) error {
	repository.noSpeechCalls++
	return nil
}

// CommitGapConflict 记录冲突提交。
func (repository *fakeCompensationRepository) CommitGapConflict(context.Context, gaprepository.ConflictInput) error {
	repository.conflictCalls++
	return nil
}

type fakeRawRecordFlusher struct {
	calls int
	err   error
}

// Flush 记录事务完成后的投影刷新。
func (flusher *fakeRawRecordFlusher) Flush(context.Context, string) error {
	flusher.calls++
	return flusher.err
}
