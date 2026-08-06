package speaker

import (
	"context"
	"errors"
	"fmt"
	"sort"

	speakerdomain "meet-sieve/internal/domain/speaker"
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

// ContinuityRouterDependencies 描述 provider 通道内短窗分轨所需边界。
type ContinuityRouterDependencies struct {
	Repository   *speakerrepository.Repository
	Transactions *database.TransactionManager
	Audio        EvidenceAudioReader
	Encoder      port.VoiceEncoder
	Profile      speakerdomain.MatchingProfile
	IDs          identity.Generator
	Clock        clock.Clock
	OnRouted     func(meetingID string, trackID string, projectionChanged bool)
}

// ContinuityRouter 在事务外编码短窗，在短事务内路由到既有或新 segment。
type ContinuityRouter struct {
	repository   *speakerrepository.Repository
	transactions *database.TransactionManager
	audio        EvidenceAudioReader
	encoder      port.VoiceEncoder
	profile      speakerdomain.MatchingProfile
	ids          identity.Generator
	clock        clock.Clock
	onRouted     func(string, string, bool)
}

type continuityCandidate struct {
	track models.SpeakerTrack
	score float64
}

const (
	routingAudioReadFailed = "continuity_audio_read_failed"
	routingEmbeddingFailed = "continuity_embedding_failed"
	routingCandidateFailed = "continuity_candidate_failed"
)

// NewContinuityRouter 创建短窗路由器；依赖在 Process 前统一校验。
func NewContinuityRouter(dependencies ContinuityRouterDependencies) *ContinuityRouter {
	return &ContinuityRouter{
		repository: dependencies.Repository, transactions: dependencies.Transactions, audio: dependencies.Audio,
		encoder: dependencies.Encoder, profile: dependencies.Profile, ids: dependencies.IDs, clock: dependencies.Clock,
		onRouted: dependencies.OnRouted,
	}
}

// Process 处理一条 pending evidence；finalizing 不改变 continuity 判定。
func (router *ContinuityRouter) Process(ctx context.Context, evidenceID string, _ bool) error {
	if err := validateContinuityRouter(router, evidenceID); err != nil {
		return err
	}
	snapshot, err := router.repository.LoadRoutingSnapshot(ctx, evidenceID)
	if err != nil {
		return err
	}
	if snapshot.Evidence.RoutingState != "pending" && snapshot.Evidence.RoutingState != "failed" {
		return nil
	}
	if snapshot.Utterance.SpeakerAssignmentSource == "manual_single" || snapshot.Utterance.SpeakerAssignmentSource == "manual_cluster" {
		return router.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
			return router.repository.SkipManualRouting(ctx, tx, evidenceID, router.clock.Now().UnixMilli())
		})
	}
	windowSamples := int64(router.profile.Continuity.WindowMS) * speakerSampleRate / 1000
	endSample := minInt64(snapshot.Utterance.EndSample, snapshot.Utterance.StartSample+windowSamples)
	samples, err := router.audio.Read(ctx, snapshot.Utterance.MeetingID, snapshot.Utterance.StartSample, endSample)
	if errors.Is(err, ErrAudioEvidencePending) {
		return nil
	}
	if err != nil {
		return router.fail(ctx, evidenceID, routingAudioReadFailed, err)
	}
	embedding, err := EncodeTrack(ctx, router.encoder, router.profile.Model, samples)
	if err != nil {
		return router.fail(ctx, evidenceID, routingEmbeddingFailed, fmt.Errorf("生成 continuity embedding 失败：%w", err))
	}
	blob, err := voiceservice.EncodeEmbeddingBlob(embedding, router.profile.Model.Dimension)
	if err != nil {
		return router.fail(ctx, evidenceID, routingEmbeddingFailed, err)
	}
	target, createTarget, score, margin, err := router.selectTarget(ctx, snapshot, embedding)
	if err != nil {
		return router.fail(ctx, evidenceID, routingCandidateFailed, err)
	}
	return router.commit(ctx, snapshot, target, createTarget, score, margin, blob, int64(len(samples))*1000/speakerSampleRate)
}

// fail 持久化稳定错误码，同时保留原始错误供日志与调用方诊断。
func (router *ContinuityRouter) fail(ctx context.Context, evidenceID string, errorCode string, cause error) error {
	markErr := router.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return router.repository.MarkRoutingFailure(ctx, tx, evidenceID, errorCode, router.clock.Now().UnixMilli())
	})
	if markErr != nil {
		return errors.Join(cause, markErr)
	}
	return cause
}

// selectTarget 以 score 与多候选 margin 双门禁选择 segment；首条 evidence 固定建立 segment 1。
func (router *ContinuityRouter) selectTarget(
	ctx context.Context,
	snapshot speakerrepository.RoutingSnapshot,
	embedding port.Embedding,
) (models.SpeakerTrack, bool, *float64, *float64, error) {
	if snapshot.Track.Source == "local_utterance" {
		return snapshot.Track, false, nil, nil, nil
	}
	candidates, err := router.loadCandidates(ctx, snapshot.Track, embedding)
	if err != nil {
		return models.SpeakerTrack{}, false, nil, nil, err
	}
	if len(candidates) == 0 {
		return snapshot.Track, false, nil, nil, nil
	}
	top := candidates[0]
	marginPassed := true
	var margin *float64
	if len(candidates) > 1 {
		value := top.score - candidates[1].score
		margin, marginPassed = &value, value >= router.profile.Continuity.MinMargin
	}
	if top.score >= router.profile.Continuity.MinScore && marginPassed {
		value := top.score
		return top.track, false, &value, margin, nil
	}
	value := top.score
	return router.newSegment(snapshot.Track), true, &value, margin, nil
}

// loadCandidates 计算每个已有 segment 的短窗 centroid 与当前 evidence 相似度。
func (router *ContinuityRouter) loadCandidates(ctx context.Context, track models.SpeakerTrack, embedding port.Embedding) ([]continuityCandidate, error) {
	rows, err := router.repository.ListRoutingCandidates(ctx, track)
	if err != nil {
		return nil, err
	}
	result := make([]continuityCandidate, 0, len(rows))
	for _, row := range rows {
		vectors := make([]TrackVector, 0, len(row.Embeddings))
		for index, blob := range row.Embeddings {
			decoded, err := voiceservice.DecodeEmbeddingBlob(blob, router.profile.Model.Dimension)
			if err != nil {
				return nil, err
			}
			vectors = append(vectors, TrackVector{TrackID: fmt.Sprintf("%s-%d", row.Track.ID, index), FirstFinalSeq: int64(index), Embedding: decoded})
		}
		if len(vectors) == 0 {
			continue
		}
		centroid, err := RecomputeCentroid(vectors, router.profile.Model.Dimension)
		if err != nil {
			return nil, err
		}
		score, err := CosineSimilarity(embedding, centroid, router.profile.Model.Dimension)
		if err != nil {
			return nil, err
		}
		result = append(result, continuityCandidate{track: row.Track, score: score})
	}
	sort.Slice(result, func(left int, right int) bool {
		if result[left].score == result[right].score {
			return *result[left].track.ProviderSegmentNo < *result[right].track.ProviderSegmentNo
		}
		return result[left].score > result[right].score
	})
	return result, nil
}

// newSegment 构造待在 writer 事务内补齐编号的 provider segment。
func (router *ContinuityRouter) newSegment(source models.SpeakerTrack) models.SpeakerTrack {
	now := router.clock.Now().UnixMilli()
	return models.SpeakerTrack{
		ID: router.ids.New(), MeetingID: source.MeetingID, ASRSessionID: source.ASRSessionID,
		Source: "provider_label", ASRSpeakerLabel: source.ASRSpeakerLabel, State: "collecting",
		RoutingRevision: 1, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
}

// commit 在 writer 事务内分配编号并提交路由，提交后再唤醒身份处理。
func (router *ContinuityRouter) commit(
	ctx context.Context,
	snapshot speakerrepository.RoutingSnapshot,
	target models.SpeakerTrack,
	createTarget bool,
	score *float64,
	margin *float64,
	embedding []byte,
	durationMS int64,
) error {
	if createTarget {
		parsed, err := uuid.Parse(target.ID)
		if err != nil || parsed.Version() != 4 {
			return fmt.Errorf("生成 continuity segment UUID 失败")
		}
	}
	projectionChanged := false
	committed := false
	err := router.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if createTarget {
			segmentNo, err := router.repository.NextProviderSegmentNo(ctx, tx, target.ASRSessionID, *target.ASRSpeakerLabel)
			if err != nil {
				return err
			}
			displayNo, err := router.repository.NextTrackDisplayNo(ctx, tx, target.MeetingID)
			if err != nil {
				return err
			}
			target.ProviderSegmentNo, target.DisplayNo = &segmentNo, displayNo
		}
		wasCommitted, changed, err := router.repository.CommitRouting(ctx, tx, speakerrepository.RoutingCommit{
			EvidenceID: snapshot.Evidence.ID, UtteranceID: snapshot.Utterance.ID, SourceTrackID: snapshot.Track.ID,
			TargetTrack: target, CreateTarget: createTarget, DurationMS: durationMS, Score: score, Margin: margin,
			ModelID: router.profile.Model.ID, ModelVersion: router.profile.Model.Version,
			ModelSHA256: router.profile.Model.SHA256, Dimension: router.profile.Model.Dimension,
			ProfileID: router.profile.ProfileID, Embedding: embedding, UpdatedAt: router.clock.Now().UnixMilli(),
		})
		committed, projectionChanged = wasCommitted, changed
		return err
	})
	if err != nil {
		return err
	}
	if committed && router.onRouted != nil {
		router.onRouted(target.MeetingID, target.ID, projectionChanged)
	}
	return nil
}

// validateContinuityRouter 要求 schema v2 profile 和完整模型依赖。
func validateContinuityRouter(router *ContinuityRouter, evidenceID string) error {
	if router == nil || router.repository == nil || router.transactions == nil || router.audio == nil || router.encoder == nil ||
		router.profile.Continuity == nil || router.ids == nil || router.clock == nil {
		return fmt.Errorf("continuity router 依赖不完整")
	}
	if _, err := uuid.Parse(evidenceID); err != nil {
		return fmt.Errorf("continuity evidence ID 无效")
	}
	return nil
}
