// Package gap 编排会后缺口补转写，不把外部 I/O 放入数据库事务。
package gap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	domaingap "meet-sieve/internal/domain/gap"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	gaprepository "meet-sieve/internal/repository/gap"
	"meet-sieve/models"
)

// RawRecordFlusher 是补偿事实提交后强制刷新原始记录的边界。
type RawRecordFlusher interface {
	Flush(context.Context, string) error
}

// CompensationRepository 是补偿提交器消费的最小事务能力。
type CompensationRepository interface {
	ListAttemptGapIDs(context.Context, string) ([]string, error)
	ListOverlaps(context.Context, string, []models.Utterance) ([]gaprepository.OverlapRow, error)
	CommitCompensation(context.Context, gaprepository.CompensationInput) error
	CommitNoSpeechCompensation(context.Context, gaprepository.NoSpeechInput) error
	CommitGapConflict(context.Context, gaprepository.ConflictInput) error
}

// CommitterDependencies 描述补偿提交所需的真实边界。
type CommitterDependencies struct {
	Repository CompensationRepository
	RawRecord  RawRecordFlusher
	IDs        identity.Generator
	Clock      clock.Clock
}

// CompensationCommitter 把规范化 provider 结果提交为 SQLite 事实并刷新原始记录。
type CompensationCommitter struct {
	repository CompensationRepository
	rawRecord  RawRecordFlusher
	ids        identity.Generator
	clock      clock.Clock
}

// NewCompensationCommitter 创建补偿提交器；构造阶段不执行文件或数据库 I/O。
func NewCompensationCommitter(dependencies CommitterDependencies) *CompensationCommitter {
	return &CompensationCommitter{
		repository: dependencies.Repository, rawRecord: dependencies.RawRecord,
		ids: dependencies.IDs, clock: dependencies.Clock,
	}
}

// Commit 原子提交补偿、无语音或冲突分支，并在事务后强制刷新原始记录。
func (committer *CompensationCommitter) Commit(ctx context.Context, attempt models.GapTranscriptionAttempt, result port.FileTranscriptionResult) error {
	if committer == nil || committer.repository == nil || committer.rawRecord == nil || committer.ids == nil || committer.clock == nil || attempt.ID == "" {
		return fmt.Errorf("补偿提交器未初始化")
	}
	candidates := domaingap.NormalizeToCore(result.Segments, attempt.AudioStartSample, attempt.CoreStartSample, attempt.CoreEndSample)
	responseJSON, err := encodeNormalizedResponse(result.NoSpeech, candidates)
	if err != nil {
		return apperr.Dependency(apperr.CodeGapTranscriptionRejected, err)
	}
	if result.NoSpeech {
		err = committer.commitNoSpeech(ctx, attempt, responseJSON, result.ProviderLogIDSuffix)
	} else {
		err = committer.commitCandidates(ctx, attempt, candidates, responseJSON, result.ProviderLogIDSuffix)
	}
	if err != nil {
		return err
	}
	if err := committer.rawRecord.Flush(ctx, attempt.MeetingID); err != nil {
		return apperr.Dependency(apperr.CodeRawRecordRefreshFailed, err)
	}
	return nil
}

// commitNoSpeech 创建 synthetic session 和唯一无语音补偿事件。
func (committer *CompensationCommitter) commitNoSpeech(ctx context.Context, attempt models.GapTranscriptionAttempt, responseJSON string, logSuffix string) error {
	gapIDs, err := committer.repository.ListAttemptGapIDs(ctx, attempt.ID)
	if err != nil || len(gapIDs) == 0 {
		return fmt.Errorf("读取无语音 gap 失败：%w", err)
	}
	now := committer.clock.Now().UnixMilli()
	session, err := committer.buildSession(attempt, now)
	if err != nil {
		return err
	}
	eventID := committer.ids.New()
	entityType, entityID := "asr_gap", gapIDs[0]
	payload := `{"v":1,"resolution":"no_speech"}`
	return committer.repository.CommitNoSpeechCompensation(ctx, gaprepository.NoSpeechInput{
		AttemptID: attempt.ID, Session: session,
		Event: models.MeetingEvent{
			ID: eventID, MeetingID: attempt.MeetingID, Kind: "asr.compensated", OccurredAt: now,
			Source: "asr", EntityType: &entityType, EntityID: &entityID, PayloadJSON: &payload,
			CreatedAt: now, UpdatedAt: now,
		},
		ResponseJSON: responseJSON, ProviderLogIDSuffix: logSuffix, UpdatedAt: now,
	})
}

// commitCandidates 在事务前检测冲突，并由 Repository 在提交时二次校验。
func (committer *CompensationCommitter) commitCandidates(ctx context.Context, attempt models.GapTranscriptionAttempt, candidates []domaingap.CandidateSegment, responseJSON string, logSuffix string) error {
	if len(candidates) == 0 {
		return apperr.Dependency(apperr.CodeGapTranscriptionRejected, fmt.Errorf("补转写结果没有 core 分句"))
	}
	now := committer.clock.Now().UnixMilli()
	session, events, utterances, err := committer.buildCompensation(attempt, candidates, now)
	if err != nil {
		return err
	}
	overlaps, err := committer.repository.ListOverlaps(ctx, attempt.MeetingID, utterances)
	if err != nil {
		return err
	}
	if len(overlaps) > 0 {
		return committer.commitConflict(ctx, attempt, overlaps, responseJSON, logSuffix, now)
	}
	err = committer.repository.CommitCompensation(ctx, gaprepository.CompensationInput{
		AttemptID: attempt.ID, Session: session, Events: events, Utterances: utterances,
		ResponseJSON: responseJSON, ProviderLogIDSuffix: logSuffix, UpdatedAt: now,
	})
	if !errors.Is(err, gaprepository.ErrConflict) {
		return err
	}
	// 提交窗口内新增 final 时重新读取；确认重叠后整次 attempt 转为 conflict。
	overlaps, queryErr := committer.repository.ListOverlaps(ctx, attempt.MeetingID, utterances)
	if queryErr != nil || len(overlaps) == 0 {
		return err
	}
	return committer.commitConflict(ctx, attempt, overlaps, responseJSON, logSuffix, now)
}

// buildCompensation 为每个候选生成稳定 event/utterance 身份。
func (committer *CompensationCommitter) buildCompensation(attempt models.GapTranscriptionAttempt, candidates []domaingap.CandidateSegment, now int64) (models.ASRSession, []models.MeetingEvent, []models.Utterance, error) {
	session, err := committer.buildSession(attempt, now)
	if err != nil {
		return models.ASRSession{}, nil, nil, err
	}
	events := make([]models.MeetingEvent, 0, len(candidates))
	utterances := make([]models.Utterance, 0, len(candidates))
	for index, candidate := range candidates {
		eventID, utteranceID := committer.ids.New(), committer.ids.New()
		entityType := "utterance"
		events = append(events, models.MeetingEvent{
			ID: eventID, MeetingID: attempt.MeetingID, Kind: "asr.compensated", OccurredAt: now,
			Source: "asr", EntityType: &entityType, EntityID: &utteranceID, CreatedAt: now, UpdatedAt: now,
		})
		label := candidate.SpeakerID
		utterances = append(utterances, models.Utterance{
			ID: utteranceID, MeetingID: attempt.MeetingID, ASRSessionID: session.ID,
			ProviderResultID: fmt.Sprintf("%s:%d", attempt.ProviderRequestID, index),
			OriginalText:     candidate.Text, CurrentText: candidate.Text,
			StartSample: candidate.StartSample, EndSample: candidate.EndSample, ASRSpeakerLabel: optionalString(label),
			SpeakerAssignmentSource: "unassigned", TextRevision: 1, SpeakerRevision: 1, CreatedAt: now, UpdatedAt: now,
		})
	}
	return session, events, utterances, nil
}

// buildSession 创建一次成功 provider 响应对应的 synthetic file session。
func (committer *CompensationCommitter) buildSession(attempt models.GapTranscriptionAttempt, now int64) (models.ASRSession, error) {
	sessionID := committer.ids.New()
	if sessionID == "" {
		return models.ASRSession{}, fmt.Errorf("生成文件 ASR session ID 失败")
	}
	requestID := attempt.ProviderRequestID
	return models.ASRSession{
		ID: sessionID, MeetingID: attempt.MeetingID, Provider: "volcano", ProviderSessionID: &requestID,
		State: "stopped", StartedAt: now, EndedAt: &now, TransportMode: "auc_flash_v3",
		InputStartSample: attempt.AudioStartSample, LastSentSample: attempt.AudioEndSample,
		LastFinalSample: attempt.AudioEndSample, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// commitConflict 保存经过限制的候选和重叠范围摘要。
func (committer *CompensationCommitter) commitConflict(ctx context.Context, attempt models.GapTranscriptionAttempt, overlaps []gaprepository.OverlapRow, responseJSON string, logSuffix string, now int64) error {
	conflictJSON, err := json.Marshal(map[string]any{"attempt_id": attempt.ID, "overlaps": overlaps})
	if err != nil {
		return fmt.Errorf("编码补转写冲突失败：%w", err)
	}
	return committer.repository.CommitGapConflict(ctx, gaprepository.ConflictInput{
		AttemptID: attempt.ID, ResponseJSON: responseJSON, ConflictJSON: string(conflictJSON),
		ProviderLogIDSuffix: logSuffix, UpdatedAt: now,
	})
}

// encodeNormalizedResponse 只保存规范化候选，不保存 provider 原始响应或 Header。
func encodeNormalizedResponse(noSpeech bool, candidates []domaingap.CandidateSegment) (string, error) {
	value, err := json.Marshal(map[string]any{"no_speech": noSpeech, "segments": candidates})
	return string(value), err
}

// optionalString 把空匿名标签映射为 NULL。
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
