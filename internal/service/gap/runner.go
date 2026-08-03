package gap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	domaingap "meet-sieve/internal/domain/gap"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	gaprepository "meet-sieve/internal/repository/gap"
	"meet-sieve/models"
)

var errAttemptTerminal = errors.New("补转写单次请求已终结")

// RunnerRepository 描述一次完整补转写流水线需要的持久能力。
type RunnerRepository interface {
	ListEligibleGaps(context.Context, string) ([]models.ASRGap, error)
	ClaimGapAttempt(context.Context, gaprepository.ClaimAttemptInput) error
	FailGapAttempt(context.Context, string, string, int64) error
	GetAttempt(context.Context, string) (models.GapTranscriptionAttempt, error)
	MarkGapAssetDeleted(context.Context, string, int64) error
}

// RunnerDependencies 描述串行补转写流水线依赖。
type RunnerDependencies struct {
	Repository  RunnerRepository
	Extractor   *Extractor
	Transcriber port.FileTranscriber
	Committer   *CompensationCommitter
	IDs         identity.Generator
	Clock       clock.Clock
	Events      EventSink
}

// EventSink 接收 gap 持久状态发生变化后的轻量刷新通知。
type EventSink interface {
	PublishGapChanged(meetingID string)
}

// EventSinkFunc 让装配层以函数实现 gap 事件出口。
type EventSinkFunc func(meetingID string)

// PublishGapChanged 发布会议级 gap 失效通知。
func (publisher EventSinkFunc) PublishGapChanged(meetingID string) {
	if publisher != nil {
		publisher(meetingID)
	}
}

// Runner 逐次执行 plan → extract → claim → transcribe → commit → cleanup。
type Runner struct {
	repository  RunnerRepository
	extractor   *Extractor
	transcriber port.FileTranscriber
	committer   *CompensationCommitter
	ids         identity.Generator
	clock       clock.Clock
	events      EventSink
}

// NewRunner 创建补转写 Runner；构造阶段不读文件、不联网。
func NewRunner(dependencies RunnerDependencies) *Runner {
	return &Runner{repository: dependencies.Repository, extractor: dependencies.Extractor, transcriber: dependencies.Transcriber, committer: dependencies.Committer, ids: dependencies.IDs, clock: dependencies.Clock, events: dependencies.Events}
}

// ProcessNext 处理计划中的第一个切片；返回是否仍有可处理 gap。
func (runner *Runner) ProcessNext(ctx context.Context, meetingID string) (bool, error) {
	return runner.processNext(ctx, meetingID, "", nil)
}

// ProcessNextRequested 使用主持人提供的请求 ID 处理首个指定切片，重放不重复调用 provider。
func (runner *Runner) ProcessNextRequested(ctx context.Context, meetingID string, requestID string, gapIDs []string) (bool, error) {
	if requestID == "" {
		return false, fmt.Errorf("补转写 request ID 为空")
	}
	if lookup, ok := runner.repository.(interface {
		GetAttemptByProviderRequest(context.Context, string, string) (models.GapTranscriptionAttempt, error)
	}); ok {
		if attempt, err := lookup.GetAttemptByProviderRequest(ctx, meetingID, requestID); err == nil {
			return attempt.State == "pending" || attempt.State == "running", nil
		}
	}
	return runner.processNext(ctx, meetingID, requestID, gapIDs)
}

// processNext 读取固定 gap 集合并执行第一个切片。
func (runner *Runner) processNext(ctx context.Context, meetingID string, requestID string, selectedGapIDs []string) (bool, error) {
	if runner == nil || runner.repository == nil || runner.extractor == nil || runner.transcriber == nil || runner.committer == nil || runner.ids == nil || runner.clock == nil || meetingID == "" {
		return false, fmt.Errorf("补转写 Runner 依赖无效")
	}
	gaps, err := runner.repository.ListEligibleGaps(ctx, meetingID)
	if err != nil || len(gaps) == 0 {
		return false, err
	}
	if len(selectedGapIDs) > 0 {
		gaps, err = selectGaps(gaps, selectedGapIDs)
		if err != nil {
			return false, err
		}
	}
	pcm, err := runner.extractor.LoadPlanningPCM(ctx, meetingID)
	if err != nil {
		return false, apperr.Dependency(apperr.CodeGapAudioUnavailable, err, apperr.WithOp("gap.plan.source"))
	}
	plan, err := domaingap.Plan(domaingap.PlanInput{Gaps: toRanges(gaps), RecordingEnd: int64(len(pcm)), SampleRate: domaingap.DefaultSampleRate, PCM: pcm})
	if err != nil || len(plan) == 0 {
		return false, apperr.Dependency(apperr.CodeGapAudioInvalid, err, apperr.WithOp("gap.plan"))
	}
	if err := runner.processSlice(ctx, meetingID, gaps, plan[0], requestID); err != nil {
		if errors.Is(err, errAttemptTerminal) {
			return false, nil
		}
		return false, err
	}
	remaining, err := runner.repository.ListEligibleGaps(ctx, meetingID)
	return len(remaining) > 0, err
}

// processSlice 执行单个计费请求，任何失败都不会自动创建第二个 request ID。
func (runner *Runner) processSlice(ctx context.Context, meetingID string, gaps []models.ASRGap, slice domaingap.Slice, requestedID string) error {
	attemptID, requestID := runner.ids.New(), requestedID
	if requestID == "" {
		requestID = runner.ids.New()
	}
	if attemptID == "" || requestID == "" {
		return fmt.Errorf("生成补转写身份失败")
	}
	now := runner.clock.Now().UnixMilli()
	asset, path, err := runner.extractor.Extract(ctx, meetingID, attemptID, slice.AudioStartSample, slice.AudioEndSample, now)
	if err != nil {
		return apperr.Dependency(apperr.CodeGapAudioInvalid, err, apperr.WithOp("gap.extract"))
	}
	attempt := buildAttempt(attemptID, requestID, meetingID, asset, slice, gaps, now)
	if err := runner.repository.ClaimGapAttempt(ctx, gaprepository.ClaimAttemptInput{Attempt: attempt, GapIDs: slice.GapIDs}); err != nil {
		runner.cleanup(ctx, asset, path)
		return err
	}
	runner.publish(meetingID)
	result, transcribeErr := runner.transcriber.Transcribe(ctx, port.FileTranscriptionRequest{
		MeetingID: meetingID, RequestID: requestID, AudioPath: path, AudioSHA256: asset.SHA256,
		CoreStartSample: slice.CoreStartSample, CoreEndSample: slice.CoreEndSample,
		AudioStartSample: slice.AudioStartSample, AudioEndSample: slice.AudioEndSample, SampleRate: domaingap.DefaultSampleRate,
	})
	if transcribeErr != nil {
		code := stableGapFailureCode(transcribeErr)
		if settleErr := runner.repository.FailGapAttempt(context.Background(), attempt.ID, code, runner.clock.Now().UnixMilli()); settleErr != nil {
			return settleErr
		}
		runner.cleanup(context.Background(), asset, path)
		runner.publish(meetingID)
		// 单次计费请求已经终结为 failed；后台首轮不自动重试，但可以继续结束同步。
		return errAttemptTerminal
	}
	commitErr := runner.committer.Commit(ctx, attempt, result)
	settled, queryErr := runner.repository.GetAttempt(context.Background(), attempt.ID)
	if queryErr == nil && settled.State != "conflict" && settled.State != "running" {
		runner.cleanup(context.Background(), asset, path)
	}
	if commitErr != nil && queryErr == nil && settled.State == "running" {
		_ = runner.repository.FailGapAttempt(context.Background(), attempt.ID, stableGapFailureCode(commitErr), runner.clock.Now().UnixMilli())
		runner.cleanup(context.Background(), asset, path)
	}
	runner.publish(meetingID)
	return commitErr
}

// publish 在锁和数据库事务外通知页面重新读取 SQLite 事实。
func (runner *Runner) publish(meetingID string) {
	if runner.events != nil {
		runner.events.PublishGapChanged(meetingID)
	}
}

// selectGaps 要求显式重试的每个 gap 都仍可领取。
func selectGaps(gaps []models.ASRGap, selected []string) ([]models.ASRGap, error) {
	wanted := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		if id == "" {
			return nil, fmt.Errorf("补转写 gap ID 为空")
		}
		wanted[id] = struct{}{}
	}
	result := make([]models.ASRGap, 0, len(wanted))
	for _, gap := range gaps {
		if _, ok := wanted[gap.ID]; ok {
			result = append(result, gap)
			delete(wanted, gap.ID)
		}
	}
	if len(wanted) > 0 {
		return nil, gaprepository.ErrConflict
	}
	return result, nil
}

// buildAttempt 固定请求范围、attempt 序号和请求哈希。
func buildAttempt(attemptID string, requestID string, meetingID string, asset models.AudioAsset, slice domaingap.Slice, gaps []models.ASRGap, now int64) models.GapTranscriptionAttempt {
	attemptNo := 1
	counts := make(map[string]int, len(gaps))
	for _, gap := range gaps {
		counts[gap.ID] = gap.AttemptCount
		if containsID(slice.GapIDs, gap.ID) && gap.AttemptCount+1 > attemptNo {
			attemptNo = gap.AttemptCount + 1
		}
	}
	hashInput, _ := json.Marshal([]any{requestID, asset.SHA256, slice.CoreStartSample, slice.CoreEndSample, slice.AudioStartSample, slice.AudioEndSample, slice.GapIDs})
	digest := sha256.Sum256(hashInput)
	startedAt := now
	return models.GapTranscriptionAttempt{
		ID: attemptID, MeetingID: meetingID, AudioAssetID: asset.ID, Provider: "volcano", ProviderRequestID: requestID,
		CoreStartSample: slice.CoreStartSample, CoreEndSample: slice.CoreEndSample, AudioStartSample: slice.AudioStartSample, AudioEndSample: slice.AudioEndSample,
		State: "running", AttemptNo: attemptNo, RequestSHA256: hex.EncodeToString(digest[:]), StartedAt: &startedAt, CreatedAt: now, UpdatedAt: now,
	}
}

// cleanup 在事务已收敛后删除派生音频，并保留 deleted 元数据。
func (runner *Runner) cleanup(ctx context.Context, asset models.AudioAsset, path string) {
	if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return
	}
	_ = runner.repository.MarkGapAssetDeleted(ctx, asset.ID, runner.clock.Now().UnixMilli())
}

// stableGapFailureCode 只保存稳定错误码，不保存 provider 原文。
func stableGapFailureCode(err error) string {
	var appError *apperr.AppError
	if errors.As(err, &appError) {
		return appError.ErrorCode
	}
	if errors.Is(err, context.Canceled) {
		return apperr.CodeGapTranscriptionCancelled.ErrorCode
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apperr.CodeGapTranscriptionTimeout.ErrorCode
	}
	return apperr.CodeGapTranscriptionRejected.ErrorCode
}

// toRanges 转换计划器所需范围。
func toRanges(gaps []models.ASRGap) []domaingap.Range {
	result := make([]domaingap.Range, 0, len(gaps))
	for _, gap := range gaps {
		result = append(result, domaingap.Range{ID: gap.ID, StartSample: gap.StartSample, EndSample: gap.EndSample})
	}
	return result
}

// containsID 判断计划是否包含指定 gap。
func containsID(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
