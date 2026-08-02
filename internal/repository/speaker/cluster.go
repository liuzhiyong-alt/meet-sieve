package speaker

import (
	"context"
	"errors"
	"fmt"

	domain "meet-sieve/internal/domain/speaker"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// ClusterTrackVector 是确定性重算 centroid 所需的持久 track 行。
type ClusterTrackVector struct {
	TrackID       string `gorm:"column:track_id"`
	FirstFinalSeq int64  `gorm:"column:first_final_seq"`
	Embedding     []byte `gorm:"column:embedding"`
}

// GetTrack 在写事务内读取单个 speaker track。
func (repository *Repository) GetTrack(ctx context.Context, tx *gorm.DB, trackID string) (models.SpeakerTrack, error) {
	var track models.SpeakerTrack
	if err := tx.WithContext(ctx).Where("id = ?", trackID).Take(&track).Error; err != nil {
		return models.SpeakerTrack{}, fmt.Errorf("读取 speaker track 失败：%w", err)
	}
	return track, nil
}

// GetCluster 在写事务内读取单个 unknown cluster。
func (repository *Repository) GetCluster(ctx context.Context, tx *gorm.DB, clusterID string) (models.SpeakerCluster, error) {
	var cluster models.SpeakerCluster
	if err := tx.WithContext(ctx).Where("id = ?", clusterID).Take(&cluster).Error; err != nil {
		return models.SpeakerCluster{}, fmt.Errorf("读取 speaker cluster 失败：%w", err)
	}
	return cluster, nil
}

// ListUnknownClusters 只返回本场、未人工分配且精确模型/profile 的 centroid。
func (repository *Repository) ListUnknownClusters(
	ctx context.Context,
	tx *gorm.DB,
	meetingID string,
	model domain.ModelIdentity,
	profileID string,
) ([]models.SpeakerCluster, error) {
	var clusters []models.SpeakerCluster
	err := tx.WithContext(ctx).
		Where("meeting_id = ? AND assignment_source = 'unassigned'", meetingID).
		Where("model_id = ? AND model_version = ? AND model_sha256 = ? AND dimension = ? AND profile_id = ?", model.ID, model.Version, model.SHA256, model.Dimension, profileID).
		Where("centroid IS NOT NULL").Order("display_no ASC").Order("id ASC").Find(&clusters).Error
	if err != nil {
		return nil, fmt.Errorf("查询 unknown clusters 失败：%w", err)
	}
	return clusters, nil
}

// NextClusterDisplayNo 在单 writer 事务中分配历史最大编号加一，不重排已有编号。
func (repository *Repository) NextClusterDisplayNo(ctx context.Context, tx *gorm.DB, meetingID string) (int, error) {
	var displayNo int
	if err := tx.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(display_no), 0) + 1 FROM speaker_clusters WHERE meeting_id = ?", meetingID,
	).Scan(&displayNo).Error; err != nil {
		return 0, fmt.Errorf("分配 unknown cluster 编号失败：%w", err)
	}
	return displayNo, nil
}

// CreateCluster 在调用方事务内写入首个 track 的 unknown cluster。
func (repository *Repository) CreateCluster(ctx context.Context, tx *gorm.DB, cluster models.SpeakerCluster) error {
	if err := tx.WithContext(ctx).Create(&cluster).Error; err != nil {
		return fmt.Errorf("创建 unknown cluster 失败：%w", err)
	}
	return nil
}

// ListClusterTrackVectors 按最早 final seq 和 track ID 返回当前 cluster 的 embedding。
func (repository *Repository) ListClusterTrackVectors(ctx context.Context, tx *gorm.DB, clusterID string) ([]ClusterTrackVector, error) {
	const statement = `SELECT track.id AS track_id, MIN(event.seq) AS first_final_seq, track.embedding
FROM speaker_tracks AS track
JOIN speaker_track_evidence AS evidence ON evidence.speaker_track_id = track.id
JOIN utterances AS utterance ON utterance.id = evidence.utterance_id
JOIN meeting_events AS event ON event.id = utterance.event_id
WHERE track.speaker_cluster_id = ? AND track.embedding IS NOT NULL
GROUP BY track.id
ORDER BY first_final_seq ASC, track.id ASC`
	var tracks []ClusterTrackVector
	if err := tx.WithContext(ctx).Raw(statement, clusterID).Scan(&tracks).Error; err != nil {
		return nil, fmt.Errorf("读取 cluster track embeddings 失败：%w", err)
	}
	return tracks, nil
}

// FirstFinalSeq 返回 track 最早 evidence 对应的持久 final seq。
func (repository *Repository) FirstFinalSeq(ctx context.Context, tx *gorm.DB, trackID string) (int64, error) {
	var seq int64
	err := tx.WithContext(ctx).Raw(`SELECT MIN(event.seq)
FROM speaker_track_evidence AS evidence
JOIN utterances AS utterance ON utterance.id = evidence.utterance_id
JOIN meeting_events AS event ON event.id = utterance.event_id
WHERE evidence.speaker_track_id = ?`, trackID).Scan(&seq).Error
	if err != nil || seq <= 0 {
		return 0, fmt.Errorf("读取 track 最早 final seq 失败：%w", err)
	}
	return seq, nil
}

// UpdateClusterCentroid 以 revision 乐观更新 centroid、track_count 和最近分数。
func (repository *Repository) UpdateClusterCentroid(
	ctx context.Context,
	tx *gorm.DB,
	cluster models.SpeakerCluster,
	centroid []byte,
	trackCount int,
	confidence float64,
	updatedAt int64,
) error {
	result := tx.WithContext(ctx).Model(&models.SpeakerCluster{}).
		Where("id = ? AND revision = ? AND assignment_source = 'unassigned'", cluster.ID, cluster.Revision).
		Updates(map[string]any{
			"centroid": centroid, "track_count": trackCount, "confidence": confidence,
			"revision": cluster.Revision + 1, "updated_at": updatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("更新 unknown cluster centroid 失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("unknown cluster 已变化或已人工分配")
	}
	return nil
}

// AssignTrackToCluster 将 track 及其 utterance 当前投影关联到 unknown cluster。
func (repository *Repository) AssignTrackToCluster(
	ctx context.Context,
	tx *gorm.DB,
	track models.SpeakerTrack,
	clusterID string,
	confidence *float64,
	updatedAt int64,
) error {
	result := tx.WithContext(ctx).Model(&models.SpeakerTrack{}).
		Where("id = ? AND revision = ? AND speaker_cluster_id IS NULL", track.ID, track.Revision).
		Updates(map[string]any{
			"state": "clustered", "speaker_cluster_id": clusterID, "top_score": confidence,
			"revision": track.Revision + 1, "updated_at": updatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("归属 unknown track 失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("unknown track 已被其他任务处理")
	}
	updates := map[string]any{
		"speaker_cluster_id": clusterID, "speaker_assignment_source": "automatic_cluster",
		"speaker_confidence": confidence, "updated_at": updatedAt,
	}
	if err := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("speaker_track_id = ? AND speaker_assignment_source = 'unassigned'", track.ID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("更新 unknown utterance 投影失败：%w", err)
	}
	return nil
}

// NextMeetingEventSeq 在单 writer 事务中分配下一统一事件序号。
func (repository *Repository) NextMeetingEventSeq(ctx context.Context, tx *gorm.DB, meetingID string) (int64, error) {
	var seq int64
	if err := tx.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(seq), 0) + 1 FROM meeting_events WHERE meeting_id = ?", meetingID,
	).Scan(&seq).Error; err != nil {
		return 0, fmt.Errorf("分配 speaker attribution 事件序号失败：%w", err)
	}
	return seq, nil
}

// CreateAttributionEvent 写入只用于 Timeline/Markdown 刷新的自动归属事件。
func (repository *Repository) CreateAttributionEvent(ctx context.Context, tx *gorm.DB, event models.MeetingEvent) error {
	if err := tx.WithContext(ctx).Create(&event).Error; err != nil {
		return fmt.Errorf("创建 speaker attribution 事件失败：%w", err)
	}
	return nil
}

// IsNotFound 判断 repository 查询错误是否为不存在。
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
