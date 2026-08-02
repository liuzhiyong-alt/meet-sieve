package speaker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	speakerrepository "meet-sieve/internal/repository/speaker"
	voiceservice "meet-sieve/internal/service/voice"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RunnerDependencies 描述一次说话人后台处理所需的计算与持久化边界。
type RunnerDependencies struct {
	Repository   *speakerrepository.Repository
	Transactions *database.TransactionManager
	Evidence     *EvidenceBuilder
	Encoder      port.VoiceEncoder
	Profile      domain.MatchingProfile
	Unknown      *UnknownAssigner
	IDs          identity.Generator
	Clock        clock.Clock
	OnChanged    func(meetingID string, trackID string)
}

// Runner 在事务外完成音频与模型计算，在短事务内提交当前说话人投影。
type Runner struct {
	repository   *speakerrepository.Repository
	transactions *database.TransactionManager
	evidence     *EvidenceBuilder
	encoder      port.VoiceEncoder
	profile      domain.MatchingProfile
	unknown      *UnknownAssigner
	ids          identity.Generator
	clock        clock.Clock
	onChanged    func(meetingID string, trackID string)
}

// NewRunner 创建后台处理器；依赖在 Process 前统一校验。
func NewRunner(dependencies RunnerDependencies) *Runner {
	return &Runner{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		evidence: dependencies.Evidence, encoder: dependencies.Encoder, profile: dependencies.Profile,
		unknown: dependencies.Unknown, ids: dependencies.IDs, clock: dependencies.Clock,
		onChanged: dependencies.OnChanged,
	}
}

// Process 处理一个持久 track；finalizing 表示 session/会议已结束，不再等待更多证据。
func (runner *Runner) Process(ctx context.Context, trackID string, finalizing bool) error {
	if err := validateRunner(runner, trackID); err != nil {
		return err
	}
	snapshot, err := runner.repository.LoadProcessingSnapshot(ctx, trackID)
	if err != nil {
		return err
	}
	if isTerminalTrack(snapshot.Track) {
		return nil
	}
	evidence, err := runner.buildEvidence(ctx, snapshot, finalizing)
	if err != nil {
		return fmt.Errorf("构造 speaker evidence 失败：%w", err)
	}
	if evidence.State != EvidenceReady {
		return runner.commitEvidenceState(ctx, snapshot.Track, evidence)
	}
	return runner.processReadyEvidence(ctx, snapshot.Track, evidence)
}

// buildEvidence 将 repository 行转换为不携带文本的音频范围输入。
func (runner *Runner) buildEvidence(ctx context.Context, snapshot speakerrepository.ProcessingSnapshot, finalizing bool) (EvidenceResult, error) {
	utterances := make([]EvidenceUtterance, 0, len(snapshot.Utterances))
	for _, utterance := range snapshot.Utterances {
		utterances = append(utterances, EvidenceUtterance{
			ID: utterance.ID, ASRSessionID: utterance.ASRSessionID, SpeakerLabel: utterance.ASRSpeakerLabel,
			FinalSeq: utterance.FinalSeq, StartSample: utterance.StartSample, EndSample: utterance.EndSample,
		})
	}
	return runner.evidence.Build(
		ctx, snapshot.Track.MeetingID, snapshot.Track.ASRSessionID, snapshot.Track.ASRSpeakerLabel,
		utterances, runner.profile.Evidence.MinEvidenceMS, runner.profile.Evidence.TargetEvidenceMS, finalizing,
	)
}

// commitEvidenceState 持久化 pending/collecting/insufficient，不启动模型推理。
func (runner *Runner) commitEvidenceState(ctx context.Context, track models.SpeakerTrack, evidence EvidenceResult) error {
	state := string(evidence.State)
	if evidence.State == EvidenceReady {
		return fmt.Errorf("ready evidence 不能作为等待状态提交")
	}
	now := runner.clock.Now().UnixMilli()
	return runner.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return runner.repository.UpdateEvidenceState(ctx, tx, track, mapEvidenceItems(evidence.Items), state, evidence.DurationMS, now)
	})
}

// processReadyEvidence 在事务外执行 encoder、候选查询和 matcher。
func (runner *Runner) processReadyEvidence(ctx context.Context, track models.SpeakerTrack, evidence EvidenceResult) error {
	embedding, err := EncodeTrack(ctx, runner.encoder, runner.profile.Model, evidence.Samples)
	if err != nil {
		state, code := "failed", apperr.CodeSpeakerEmbeddingFailed.ErrorCode
		if errors.Is(err, domain.ErrProfileMismatch) {
			state, code = "unavailable", apperr.CodeSpeakerProfileMismatch.ErrorCode
		}
		return runner.failReadyTrack(ctx, track, evidence, state, code, fmt.Errorf("执行 speaker encoder 失败：%w", err))
	}
	candidates, err := runner.repository.ListCandidateEmbeddings(ctx, track.MeetingID, runner.profile.Model)
	if err != nil {
		return runner.failReadyTrack(ctx, track, evidence, "failed", apperr.CodeSpeakerProcessingFailed.ErrorCode, err)
	}
	decision, err := MatchMember(embedding, candidates, runner.profile.Identity, runner.profile.Model.Dimension)
	if err != nil {
		return runner.failReadyTrack(ctx, track, evidence, "failed", apperr.CodeSpeakerProcessingFailed.ErrorCode, err)
	}
	blob, err := voiceservice.EncodeEmbeddingBlob(embedding, runner.profile.Model.Dimension)
	if err != nil {
		return fmt.Errorf("编码 speaker track embedding 失败：%w", err)
	}
	update := runner.buildEmbeddingUpdate(decision, evidence.DurationMS, blob)
	if decision.State == MemberMatched {
		return runner.commitMatched(ctx, track, evidence.Items, update)
	}
	if _, err := runner.unknown.AssignPrepared(ctx, track, runner.profile, mapEvidenceItems(evidence.Items), update); err != nil {
		return err
	}
	runner.notifyChanged(track)
	return nil
}

// failReadyTrack 尽力持久化稳定失败状态，并保留原始计算错误供宿主诊断。
func (runner *Runner) failReadyTrack(
	ctx context.Context,
	track models.SpeakerTrack,
	evidence EvidenceResult,
	state string,
	errorCode string,
	cause error,
) error {
	now := runner.clock.Now().UnixMilli()
	err := runner.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return runner.repository.MarkTrackFailure(ctx, tx, track, mapEvidenceItems(evidence.Items), state, evidence.DurationMS, errorCode, now)
	})
	if err != nil {
		return fmt.Errorf("%v；且持久化 speaker 失败状态失败：%w", cause, err)
	}
	return cause
}

// buildEmbeddingUpdate 把 matcher 输出转换为完整、可复现的持久决策。
func (runner *Runner) buildEmbeddingUpdate(decision MemberDecision, durationMS int64, embedding []byte) speakerrepository.TrackEmbeddingUpdate {
	update := speakerrepository.TrackEmbeddingUpdate{
		State: "ambiguous", EvidenceDurationMS: durationMS,
		ModelID: runner.profile.Model.ID, ModelVersion: runner.profile.Model.Version,
		ModelSHA256: runner.profile.Model.SHA256, Dimension: runner.profile.Model.Dimension,
		Embedding: embedding, ProfileID: runner.profile.ProfileID, UpdatedAt: runner.clock.Now().UnixMilli(),
	}
	if decision.CandidateCount > 0 {
		update.TopScore = floatPointer(decision.TopScore)
		if !decision.SingleCandidate {
			update.RunnerUpScore = floatPointer(decision.RunnerUpScore)
		}
	}
	if decision.State == MemberMatched {
		update.State = "matched"
		update.ParticipantID = stringPointer(decision.ParticipantID)
	}
	return update
}

// commitMatched 原子提交成员决定、当前投影和一条自动 attribution event。
func (runner *Runner) commitMatched(
	ctx context.Context,
	track models.SpeakerTrack,
	items []EvidenceItem,
	update speakerrepository.TrackEmbeddingUpdate,
) error {
	eventID, err := runner.newUUID()
	if err != nil {
		return err
	}
	err = runner.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		seq, err := runner.repository.NextMeetingEventSeq(ctx, tx, track.MeetingID)
		if err != nil {
			return err
		}
		entityType, payload := "speaker_track", `{"assignment":"meeting_member"}`
		event := models.MeetingEvent{
			ID: eventID, MeetingID: track.MeetingID, Seq: seq, Kind: "speaker.attributed",
			OccurredAt: update.UpdatedAt, Source: "system", EntityType: &entityType, EntityID: &track.ID,
			PayloadJSON: &payload, CreatedAt: update.UpdatedAt, UpdatedAt: update.UpdatedAt,
		}
		return runner.repository.CommitEmbeddingDecision(ctx, tx, track, mapEvidenceItems(items), update, &event)
	})
	if err != nil {
		return fmt.Errorf("提交 speaker 成员归属失败：%w", err)
	}
	runner.notifyChanged(track)
	return nil
}

// notifyChanged 只在事务成功后通知 Timeline/Markdown 投影刷新。
func (runner *Runner) notifyChanged(track models.SpeakerTrack) {
	if runner.onChanged != nil {
		runner.onChanged(track.MeetingID, track.ID)
	}
}

// newUUID 只接受 UUID v4，避免无效统一事件主键。
func (runner *Runner) newUUID() (string, error) {
	value := runner.ids.New()
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 4 {
		return "", fmt.Errorf("生成 speaker attribution UUID v4 失败")
	}
	return value, nil
}

// mapEvidenceItems 转换 evidence 追溯结果并保留空原因的 NULL 语义。
func mapEvidenceItems(items []EvidenceItem) []speakerrepository.EvidenceItemUpdate {
	result := make([]speakerrepository.EvidenceItemUpdate, 0, len(items))
	for _, item := range items {
		var reason *string
		if item.ExcludedReason != "" {
			reason = stringPointer(item.ExcludedReason)
		}
		result = append(result, speakerrepository.EvidenceItemUpdate{
			UtteranceID: item.UtteranceID, EvidenceOrder: item.EvidenceOrder,
			OverlapRisk: item.OverlapRisk, Included: item.Included, ExcludedReason: reason,
		})
	}
	return result
}

// isTerminalTrack 避免重复处理已提交自动决定的 track。
func isTerminalTrack(track models.SpeakerTrack) bool {
	return track.State == "matched" || track.State == "clustered" || track.State == "insufficient" || track.State == "unavailable"
}

// validateRunner 在访问数据库前拒绝缺失依赖和明显无效 profile。
func validateRunner(runner *Runner, trackID string) error {
	if runner == nil || runner.repository == nil || runner.transactions == nil || runner.evidence == nil ||
		runner.encoder == nil || runner.unknown == nil || runner.ids == nil || runner.clock == nil {
		return fmt.Errorf("speaker Runner 依赖不完整")
	}
	if _, err := uuid.Parse(trackID); err != nil {
		return fmt.Errorf("speaker Runner track ID 无效")
	}
	if strings.TrimSpace(runner.profile.ProfileID) == "" || runner.profile.Model.Dimension <= 0 {
		return fmt.Errorf("speaker Runner profile 无效")
	}
	return nil
}

// stringPointer 返回独立字符串指针。
func stringPointer(value string) *string { return &value }

// floatPointer 返回独立浮点指针。
func floatPointer(value float64) *float64 { return &value }
