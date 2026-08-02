package speaker

import (
	"context"
	"fmt"

	domain "meet-sieve/internal/domain/speaker"
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

// UnknownAssignerDependencies 描述 unknown cluster 短事务所需边界。
type UnknownAssignerDependencies struct {
	Repository   *speakerrepository.Repository
	Transactions *database.TransactionManager
	IDs          identity.Generator
	Clock        clock.Clock
}

// UnknownAssignmentResult 返回最终 cluster 和是否复用已有 track 归属。
type UnknownAssignmentResult struct {
	Cluster   models.SpeakerCluster
	Duplicate bool
}

// UnknownAssigner 在单 writer 事务中选择或创建本场 unknown cluster。
type UnknownAssigner struct {
	repository   *speakerrepository.Repository
	transactions *database.TransactionManager
	ids          identity.Generator
	clock        clock.Clock
}

// NewUnknownAssigner 创建 unknown 归属服务；执行时统一校验依赖。
func NewUnknownAssigner(dependencies UnknownAssignerDependencies) *UnknownAssigner {
	return &UnknownAssigner{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		ids: dependencies.IDs, clock: dependencies.Clock,
	}
}

// Assign 幂等归属一个已生成当前模型 embedding 的 rejected track。
func (assigner *UnknownAssigner) Assign(ctx context.Context, trackID string, profile domain.MatchingProfile) (UnknownAssignmentResult, error) {
	if assigner == nil || assigner.repository == nil || assigner.transactions == nil || assigner.ids == nil || assigner.clock == nil {
		return UnknownAssignmentResult{}, fmt.Errorf("unknown assigner 依赖不完整")
	}
	if _, err := uuid.Parse(trackID); err != nil {
		return UnknownAssignmentResult{}, fmt.Errorf("unknown track ID 无效")
	}
	var result UnknownAssignmentResult
	err := assigner.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		track, err := assigner.repository.GetTrack(ctx, tx, trackID)
		if err != nil {
			return err
		}
		return assigner.assignTrackInTransaction(ctx, tx, track, profile, &result)
	})
	if err != nil {
		return UnknownAssignmentResult{}, fmt.Errorf("提交 unknown cluster 失败：%w", err)
	}
	return result, nil
}

// AssignPrepared 把事务外生成的 embedding 与 unknown 决策、投影和事件原子提交。
func (assigner *UnknownAssigner) AssignPrepared(
	ctx context.Context,
	track models.SpeakerTrack,
	profile domain.MatchingProfile,
	items []speakerrepository.EvidenceItemUpdate,
	update speakerrepository.TrackEmbeddingUpdate,
) (UnknownAssignmentResult, error) {
	if assigner == nil || assigner.repository == nil || assigner.transactions == nil || assigner.ids == nil || assigner.clock == nil {
		return UnknownAssignmentResult{}, fmt.Errorf("unknown assigner 依赖不完整")
	}
	var result UnknownAssignmentResult
	err := assigner.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if err := assigner.repository.CommitEmbeddingDecision(ctx, tx, track, items, update, nil); err != nil {
			return err
		}
		applyPreparedTrackUpdate(&track, update)
		return assigner.assignTrackInTransaction(ctx, tx, track, profile, &result)
	})
	if err != nil {
		return UnknownAssignmentResult{}, fmt.Errorf("提交 prepared unknown cluster 失败：%w", err)
	}
	return result, nil
}

// assignTrackInTransaction 复用已有归属，否则计算候选并原子更新 cluster/track。
func (assigner *UnknownAssigner) assignTrackInTransaction(
	ctx context.Context,
	tx *gorm.DB,
	track models.SpeakerTrack,
	profile domain.MatchingProfile,
	result *UnknownAssignmentResult,
) error {
	if track.SpeakerClusterID != nil {
		cluster, err := assigner.repository.GetCluster(ctx, tx, *track.SpeakerClusterID)
		if err != nil {
			return err
		}
		*result = UnknownAssignmentResult{Cluster: cluster, Duplicate: true}
		return nil
	}
	embedding, err := validateTrackForCluster(track, profile)
	if err != nil {
		return err
	}
	candidates, err := assigner.repository.ListUnknownClusters(ctx, tx, track.MeetingID, profile.Model, profile.ProfileID)
	if err != nil {
		return err
	}
	decision, err := SelectUnknownCluster(embedding, mapClusterCandidates(candidates), profile.UnknownCluster, profile.Model.Dimension)
	if err != nil {
		return err
	}
	if decision.State == ClusterJoined {
		return assigner.joinCluster(ctx, tx, track, embedding, candidates, decision, profile, result)
	}
	return assigner.createCluster(ctx, tx, track, embedding, decision, profile, result)
}

// applyPreparedTrackUpdate 同步内存 track，使同一事务后续乐观锁使用新 revision。
func applyPreparedTrackUpdate(track *models.SpeakerTrack, update speakerrepository.TrackEmbeddingUpdate) {
	track.State = update.State
	track.AutomaticParticipantID = update.ParticipantID
	track.TopScore = update.TopScore
	track.RunnerUpScore = update.RunnerUpScore
	track.EvidenceDurationMS = update.EvidenceDurationMS
	track.ModelID = stringPointer(update.ModelID)
	track.ModelVersion = stringPointer(update.ModelVersion)
	track.ModelSHA256 = stringPointer(update.ModelSHA256)
	track.Dimension = &update.Dimension
	track.Embedding = update.Embedding
	track.ProfileID = stringPointer(update.ProfileID)
	track.Revision++
	track.UpdatedAt = update.UpdatedAt
}

// joinCluster 重算确定性 centroid，再关联当前 track。
func (assigner *UnknownAssigner) joinCluster(
	ctx context.Context,
	tx *gorm.DB,
	track models.SpeakerTrack,
	embedding port.Embedding,
	candidates []models.SpeakerCluster,
	decision ClusterDecision,
	profile domain.MatchingProfile,
	result *UnknownAssignmentResult,
) error {
	cluster, err := findSelectedCluster(candidates, decision.ClusterID)
	if err != nil {
		return err
	}
	vectors, err := assigner.loadClusterVectors(ctx, tx, cluster.ID, profile.Model.Dimension)
	if err != nil {
		return err
	}
	seq, err := assigner.repository.FirstFinalSeq(ctx, tx, track.ID)
	if err != nil {
		return err
	}
	vectors = append(vectors, TrackVector{TrackID: track.ID, FirstFinalSeq: seq, Embedding: embedding})
	centroid, err := RecomputeCentroid(vectors, profile.Model.Dimension)
	if err != nil {
		return err
	}
	blob, err := voiceservice.EncodeEmbeddingBlob(centroid, profile.Model.Dimension)
	if err != nil {
		return err
	}
	now := assigner.clock.Now().UnixMilli()
	if err := assigner.repository.UpdateClusterCentroid(ctx, tx, cluster, blob, len(vectors), decision.TopScore, now); err != nil {
		return err
	}
	confidence := decision.TopScore
	if err := assigner.repository.AssignTrackToCluster(ctx, tx, track, cluster.ID, &confidence, now); err != nil {
		return err
	}
	if err := assigner.createAttributionEvent(ctx, tx, track, now); err != nil {
		return err
	}
	cluster.Centroid, cluster.TrackCount, cluster.Confidence = blob, len(vectors), &confidence
	cluster.Revision++
	cluster.UpdatedAt = now
	*result = UnknownAssignmentResult{Cluster: cluster}
	return nil
}

// createCluster 分配稳定 display_no，以当前 track 建立首个 centroid。
func (assigner *UnknownAssigner) createCluster(
	ctx context.Context,
	tx *gorm.DB,
	track models.SpeakerTrack,
	embedding port.Embedding,
	decision ClusterDecision,
	profile domain.MatchingProfile,
	result *UnknownAssignmentResult,
) error {
	displayNo, err := assigner.repository.NextClusterDisplayNo(ctx, tx, track.MeetingID)
	if err != nil {
		return err
	}
	clusterID, err := assigner.newUUID()
	if err != nil {
		return err
	}
	centroid, err := RecomputeCentroid([]TrackVector{{TrackID: track.ID, Embedding: embedding}}, profile.Model.Dimension)
	if err != nil {
		return err
	}
	blob, err := voiceservice.EncodeEmbeddingBlob(centroid, profile.Model.Dimension)
	if err != nil {
		return err
	}
	now := assigner.clock.Now().UnixMilli()
	cluster := buildUnknownCluster(clusterID, track.MeetingID, displayNo, blob, profile, decision, now)
	if err := assigner.repository.CreateCluster(ctx, tx, cluster); err != nil {
		return err
	}
	if err := assigner.repository.AssignTrackToCluster(ctx, tx, track, cluster.ID, cluster.Confidence, now); err != nil {
		return err
	}
	if err := assigner.createAttributionEvent(ctx, tx, track, now); err != nil {
		return err
	}
	*result = UnknownAssignmentResult{Cluster: cluster}
	return nil
}

// createAttributionEvent 为一个首次自动归属写入恰好一个统一事件。
func (assigner *UnknownAssigner) createAttributionEvent(ctx context.Context, tx *gorm.DB, track models.SpeakerTrack, now int64) error {
	eventID, err := assigner.newUUID()
	if err != nil {
		return err
	}
	seq, err := assigner.repository.NextMeetingEventSeq(ctx, tx, track.MeetingID)
	if err != nil {
		return err
	}
	entityType, payload := "speaker_track", `{"assignment":"unknown_cluster"}`
	return assigner.repository.CreateAttributionEvent(ctx, tx, models.MeetingEvent{
		ID: eventID, MeetingID: track.MeetingID, Seq: seq, Kind: "speaker.attributed", OccurredAt: now,
		Source: "system", EntityType: &entityType, EntityID: &track.ID, PayloadJSON: &payload,
		CreatedAt: now, UpdatedAt: now,
	})
}

// loadClusterVectors 解码现有 cluster track embeddings。
func (assigner *UnknownAssigner) loadClusterVectors(ctx context.Context, tx *gorm.DB, clusterID string, dimension int) ([]TrackVector, error) {
	rows, err := assigner.repository.ListClusterTrackVectors(ctx, tx, clusterID)
	if err != nil {
		return nil, err
	}
	result := make([]TrackVector, 0, len(rows))
	for _, row := range rows {
		embedding, err := voiceservice.DecodeEmbeddingBlob(row.Embedding, dimension)
		if err != nil {
			return nil, fmt.Errorf("解码 cluster track embedding 失败：%w", err)
		}
		result = append(result, TrackVector{TrackID: row.TrackID, FirstFinalSeq: row.FirstFinalSeq, Embedding: embedding})
	}
	return result, nil
}

// validateTrackForCluster 核对 track embedding 与 profile 精确身份。
func validateTrackForCluster(track models.SpeakerTrack, profile domain.MatchingProfile) (port.Embedding, error) {
	if track.ModelID == nil || track.ModelVersion == nil || track.ModelSHA256 == nil || track.Dimension == nil ||
		track.ProfileID == nil || *track.ModelID != profile.Model.ID || *track.ModelVersion != profile.Model.Version ||
		*track.ModelSHA256 != profile.Model.SHA256 || *track.Dimension != profile.Model.Dimension || *track.ProfileID != profile.ProfileID {
		return nil, domain.ErrProfileMismatch
	}
	return voiceservice.DecodeEmbeddingBlob(track.Embedding, profile.Model.Dimension)
}

// mapClusterCandidates 转换 repository 模型，不暴露 assigned/manual cluster。
func mapClusterCandidates(clusters []models.SpeakerCluster) []UnknownClusterCandidate {
	result := make([]UnknownClusterCandidate, 0, len(clusters))
	for _, cluster := range clusters {
		result = append(result, UnknownClusterCandidate{ID: cluster.ID, DisplayNo: cluster.DisplayNo, Centroid: cluster.Centroid})
	}
	return result
}

// findSelectedCluster 返回决策指向的当前事务候选。
func findSelectedCluster(clusters []models.SpeakerCluster, id string) (models.SpeakerCluster, error) {
	for _, cluster := range clusters {
		if cluster.ID == id {
			return cluster, nil
		}
	}
	return models.SpeakerCluster{}, fmt.Errorf("unknown cluster 决策目标不存在")
}

// buildUnknownCluster 构造首个 track 的完整模型/profile 事实。
func buildUnknownCluster(
	id string,
	meetingID string,
	displayNo int,
	centroid []byte,
	profile domain.MatchingProfile,
	decision ClusterDecision,
	now int64,
) models.SpeakerCluster {
	modelID, modelVersion, modelSHA, dimension, profileID := profile.Model.ID, profile.Model.Version, profile.Model.SHA256, profile.Model.Dimension, profile.ProfileID
	cluster := models.SpeakerCluster{
		ID: id, MeetingID: meetingID, DisplayNo: displayNo, AssignmentSource: "unassigned", Centroid: centroid,
		ModelID: &modelID, ModelVersion: &modelVersion, ModelSHA256: &modelSHA, Dimension: &dimension,
		ProfileID: &profileID, TrackCount: 1, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if decision.CandidateCount > 0 {
		confidence := decision.TopScore
		cluster.Confidence = &confidence
	}
	return cluster
}

// newUUID 生成 cluster UUID v4。
func (assigner *UnknownAssigner) newUUID() (string, error) {
	value := assigner.ids.New()
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 4 {
		return "", fmt.Errorf("生成 unknown cluster UUID v4 失败")
	}
	return value, nil
}
