package speaker

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// Repository 负责 speaker 事实读写，不执行音频读取或模型推理。
type Repository struct {
	reader *gorm.DB
}

// ObserveFact 是已持久 final 创建 evidence 所需的最小事实。
type ObserveFact struct {
	Utterance models.Utterance
	FinalSeq  int64
}

// NewRepository 创建 speaker Repository；reader 仅用于事务外恢复查询。
func NewRepository(reader *gorm.DB) *Repository {
	return &Repository{reader: reader}
}

// GetObserveFact 在同一事务内读取 final utterance 和持久事件序号。
func (repository *Repository) GetObserveFact(ctx context.Context, tx *gorm.DB, utteranceID string) (ObserveFact, error) {
	if tx == nil || utteranceID == "" {
		return ObserveFact{}, fmt.Errorf("读取 speaker Observe 事实：参数无效")
	}
	var row struct {
		models.Utterance
		FinalSeq int64 `gorm:"column:final_seq"`
	}
	selection := append(utteranceColumns(), "meeting_events.seq AS final_seq")
	err := tx.WithContext(ctx).Table("utterances").Select(selection).
		Joins("JOIN meeting_events ON meeting_events.id = utterances.event_id").
		Where("utterances.id = ? AND meeting_events.kind = 'utterance.final'", utteranceID).Take(&row).Error
	if err != nil {
		return ObserveFact{}, fmt.Errorf("读取 speaker Observe 事实失败：%w", err)
	}
	return ObserveFact{Utterance: row.Utterance, FinalSeq: row.FinalSeq}, nil
}

// FindEvidenceByUtterance 返回已归属的 evidence，用于 Observe 幂等重放。
func (repository *Repository) FindEvidenceByUtterance(ctx context.Context, tx *gorm.DB, utteranceID string) (*models.SpeakerTrackEvidence, error) {
	var evidence models.SpeakerTrackEvidence
	err := tx.WithContext(ctx).Where("utterance_id = ?", utteranceID).Take(&evidence).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 speaker evidence 失败：%w", err)
	}
	return &evidence, nil
}

// FindTrackBySessionLabel 返回 session 内已有匿名 track。
func (repository *Repository) FindTrackBySessionLabel(ctx context.Context, tx *gorm.DB, sessionID string, label string) (*models.SpeakerTrack, error) {
	var track models.SpeakerTrack
	err := tx.WithContext(ctx).Where("asr_session_id = ? AND asr_speaker_label = ?", sessionID, label).
		Order("provider_segment_no ASC").Take(&track).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 speaker track 失败：%w", err)
	}
	return &track, nil
}

// FindTrackBySourceUtterance 返回同一个无标签 final 已创建的本地候选 track。
func (repository *Repository) FindTrackBySourceUtterance(ctx context.Context, tx *gorm.DB, utteranceID string) (*models.SpeakerTrack, error) {
	var track models.SpeakerTrack
	err := tx.WithContext(ctx).Where("source = 'local_utterance' AND source_utterance_id = ?", utteranceID).Take(&track).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询本地 speaker track 失败：%w", err)
	}
	return &track, nil
}

// CreateTrack 写入 collecting track，唯一约束兜底 session/label 幂等键。
func (repository *Repository) CreateTrack(ctx context.Context, tx *gorm.DB, track models.SpeakerTrack) error {
	if err := tx.WithContext(ctx).Create(&track).Error; err != nil {
		return fmt.Errorf("创建 speaker track 失败：%w", err)
	}
	return nil
}

// NextTrackDisplayNo 在单 writer 事务内分配会议级匿名 track 展示编号。
func (repository *Repository) NextTrackDisplayNo(ctx context.Context, tx *gorm.DB, meetingID string) (int, error) {
	if tx == nil || meetingID == "" {
		return 0, fmt.Errorf("分配 speaker track 展示编号：参数无效")
	}
	var displayNo int
	if err := tx.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(display_no), 0) + 1 FROM speaker_tracks WHERE meeting_id = ?", meetingID,
	).Scan(&displayNo).Error; err != nil {
		return 0, fmt.Errorf("分配 speaker track 展示编号失败：%w", err)
	}
	return displayNo, nil
}

// NextEvidenceOrder 在单 writer 事务中分配 track 内确定性证据序号。
func (repository *Repository) NextEvidenceOrder(ctx context.Context, tx *gorm.DB, trackID string) (int, error) {
	var order int
	if err := tx.WithContext(ctx).Raw(
		"SELECT COALESCE(MAX(evidence_order), 0) + 1 FROM speaker_track_evidence WHERE speaker_track_id = ?", trackID,
	).Scan(&order).Error; err != nil {
		return 0, fmt.Errorf("分配 speaker evidence 顺序失败：%w", err)
	}
	return order, nil
}

// AttachEvidence 原子写 evidence 并把 utterance 指向同一 track；返回是否同步改变了说话人投影。
func (repository *Repository) AttachEvidence(ctx context.Context, tx *gorm.DB, evidence models.SpeakerTrackEvidence, updatedAt int64) (bool, error) {
	result := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("id = ? AND (speaker_track_id IS NULL OR speaker_track_id = ?)", evidence.UtteranceID, evidence.SpeakerTrackID).
		Updates(map[string]any{"speaker_track_id": evidence.SpeakerTrackID, "updated_at": updatedAt})
	if result.Error != nil {
		return false, fmt.Errorf("关联 utterance speaker track 失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return false, fmt.Errorf("utterance 已归属其他 speaker track")
	}
	if err := tx.WithContext(ctx).Create(&evidence).Error; err != nil {
		return false, fmt.Errorf("创建 speaker evidence 失败：%w", err)
	}
	if evidence.RoutingState == "pending" {
		return false, nil
	}
	return repository.inheritTrackProjection(ctx, tx, evidence, updatedAt)
}

// inheritTrackProjection 让既有成员/cluster track 的新 final 继承当前决定，并返回投影是否发生变化。
func (repository *Repository) inheritTrackProjection(
	ctx context.Context,
	tx *gorm.DB,
	evidence models.SpeakerTrackEvidence,
	updatedAt int64,
) (bool, error) {
	var assignment struct {
		State                  string   `gorm:"column:state"`
		AutomaticParticipantID *string  `gorm:"column:automatic_participant_id"`
		ClusterID              *string  `gorm:"column:cluster_id"`
		AssignedParticipantID  *string  `gorm:"column:assigned_participant_id"`
		ClusterSource          *string  `gorm:"column:cluster_source"`
		Confidence             *float64 `gorm:"column:confidence"`
	}
	err := tx.WithContext(ctx).Raw(`SELECT track.state, track.automatic_participant_id,
       cluster.id AS cluster_id, cluster.assigned_participant_id, cluster.assignment_source AS cluster_source,
       track.top_score AS confidence
FROM speaker_tracks AS track
LEFT JOIN speaker_clusters AS cluster ON cluster.id = track.speaker_cluster_id
WHERE track.id = ?`, evidence.SpeakerTrackID).
		Take(&assignment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取人工 cluster 继承事实失败：%w", err)
	}
	updates := map[string]any{"updated_at": updatedAt}
	switch {
	case assignment.State == "matched" && assignment.AutomaticParticipantID != nil:
		updates["current_participant_id"] = *assignment.AutomaticParticipantID
		updates["speaker_assignment_source"] = "automatic_member"
		updates["speaker_confidence"] = assignment.Confidence
	case assignment.ClusterID != nil && assignment.ClusterSource != nil && *assignment.ClusterSource == "manual" && assignment.AssignedParticipantID != nil:
		updates["current_participant_id"] = *assignment.AssignedParticipantID
		updates["speaker_cluster_id"] = *assignment.ClusterID
		updates["speaker_assignment_source"] = "manual_cluster"
		updates["speaker_confidence"] = nil
	case assignment.ClusterID != nil:
		updates["speaker_cluster_id"] = *assignment.ClusterID
		updates["speaker_assignment_source"] = "automatic_cluster"
		updates["speaker_confidence"] = assignment.Confidence
	default:
		return false, nil
	}
	updates["speaker_revision"] = gorm.Expr("speaker_revision + 1")
	result := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("id = ? AND speaker_assignment_source = 'unassigned'", evidence.UtteranceID).Updates(updates)
	if result.Error != nil {
		return false, fmt.Errorf("继承 speaker track 投影失败：%w", result.Error)
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	if assignment.ClusterID != nil {
		if err := tx.WithContext(ctx).Model(&models.SpeakerCluster{}).Where("id = ?", *assignment.ClusterID).
			Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": updatedAt}).Error; err != nil {
			return false, fmt.Errorf("推进 cluster scope revision 失败：%w", err)
		}
	}
	return true, nil
}

// ListRecoverableTrackIDs 按最早 final seq 返回需要后台补拉的 track。
func (repository *Repository) ListRecoverableTrackIDs(ctx context.Context, limit int) ([]string, error) {
	return repository.listRecoverableTrackIDs(ctx, "", limit)
}

// ListRecoverableMeetingTrackIDs 只返回指定会议仍需恢复的 track，供显式重试使用。
func (repository *Repository) ListRecoverableMeetingTrackIDs(ctx context.Context, meetingID string, limit int) ([]string, error) {
	if meetingID == "" {
		return nil, fmt.Errorf("恢复 speaker track：会议 ID 为空")
	}
	return repository.listRecoverableTrackIDs(ctx, meetingID, limit)
}

// listRecoverableTrackIDs 按最早 final seq 查询全局或单场持久任务。
func (repository *Repository) listRecoverableTrackIDs(ctx context.Context, meetingID string, limit int) ([]string, error) {
	if repository == nil || repository.reader == nil {
		return nil, fmt.Errorf("恢复 speaker track：数据库不可用")
	}
	if limit <= 0 {
		limit = 64
	}
	if limit > 256 {
		limit = 256
	}
	const statement = `SELECT track.id
FROM speaker_tracks AS track
JOIN speaker_track_evidence AS evidence ON evidence.speaker_track_id = track.id
JOIN utterances AS utterance ON utterance.id = evidence.utterance_id
JOIN meeting_events AS event ON event.id = utterance.event_id
WHERE evidence.routing_state = 'routed'
  AND track.state IN ('collecting', 'pending', 'rebuild_required') AND (? = '' OR track.meeting_id = ?)
GROUP BY track.id
ORDER BY MIN(event.seq) ASC, track.id ASC
LIMIT ?`
	var ids []string
	if err := repository.reader.WithContext(ctx).Raw(statement, meetingID, meetingID, limit).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("恢复 speaker track 查询失败：%w", err)
	}
	return ids, nil
}

// utteranceColumns 返回 Observe 读取所需的显式 utterance 字段。
func utteranceColumns() []string {
	return []string{
		"utterances.id", "utterances.meeting_id", "utterances.event_id", "utterances.asr_session_id",
		"utterances.provider_result_id", "utterances.original_text", "utterances.current_text",
		"utterances.start_sample", "utterances.end_sample", "utterances.asr_speaker_label",
		"utterances.current_participant_id", "utterances.speaker_track_id", "utterances.speaker_cluster_id",
		"utterances.speaker_assignment_source", "utterances.speaker_confidence", "utterances.text_revision",
		"utterances.speaker_revision", "utterances.created_at", "utterances.updated_at",
	}
}
