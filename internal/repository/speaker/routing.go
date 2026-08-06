package speaker

import (
	"context"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// RoutingSnapshot 是 continuity router 在事务外计算所需的稳定事实。
type RoutingSnapshot struct {
	Evidence  models.SpeakerTrackEvidence
	Track     models.SpeakerTrack
	Utterance models.Utterance
}

// RoutingCandidate 保存同 provider 通道内一个 segment 及其已路由短窗 embedding。
type RoutingCandidate struct {
	Track      models.SpeakerTrack
	Embeddings [][]byte
}

// RoutingCommit 描述一次 continuity 决策的完整持久化输入。
type RoutingCommit struct {
	EvidenceID    string
	UtteranceID   string
	SourceTrackID string
	TargetTrack   models.SpeakerTrack
	CreateTarget  bool
	DurationMS    int64
	Score         *float64
	Margin        *float64
	ModelID       string
	ModelVersion  string
	ModelSHA256   string
	Dimension     int
	ProfileID     string
	Embedding     []byte
	UpdatedAt     int64
}

// LoadRoutingSnapshot 读取一条可重试 evidence、当前 provisional track 和 utterance。
func (repository *Repository) LoadRoutingSnapshot(ctx context.Context, evidenceID string) (RoutingSnapshot, error) {
	if repository == nil || repository.reader == nil || evidenceID == "" {
		return RoutingSnapshot{}, fmt.Errorf("读取 continuity routing 快照：参数无效")
	}
	var evidence models.SpeakerTrackEvidence
	if err := repository.reader.WithContext(ctx).Where("id = ?", evidenceID).Take(&evidence).Error; err != nil {
		return RoutingSnapshot{}, fmt.Errorf("读取 routing evidence 失败：%w", err)
	}
	var track models.SpeakerTrack
	if err := repository.reader.WithContext(ctx).Where("id = ?", evidence.SpeakerTrackID).Take(&track).Error; err != nil {
		return RoutingSnapshot{}, fmt.Errorf("读取 routing track 失败：%w", err)
	}
	var utterance models.Utterance
	if err := repository.reader.WithContext(ctx).Where("id = ?", evidence.UtteranceID).Take(&utterance).Error; err != nil {
		return RoutingSnapshot{}, fmt.Errorf("读取 routing utterance 失败：%w", err)
	}
	return RoutingSnapshot{Evidence: evidence, Track: track, Utterance: utterance}, nil
}

// ListRoutingCandidates 返回同 session/label 下已有 segment 的已路由短窗向量。
func (repository *Repository) ListRoutingCandidates(ctx context.Context, track models.SpeakerTrack) ([]RoutingCandidate, error) {
	if track.Source != "provider_label" || track.ASRSpeakerLabel == nil {
		return nil, nil
	}
	var tracks []models.SpeakerTrack
	if err := repository.reader.WithContext(ctx).
		Where("source = 'provider_label' AND asr_session_id = ? AND asr_speaker_label = ?", track.ASRSessionID, *track.ASRSpeakerLabel).
		Order("provider_segment_no ASC").Find(&tracks).Error; err != nil {
		return nil, fmt.Errorf("读取 continuity segment 候选失败：%w", err)
	}
	result := make([]RoutingCandidate, 0, len(tracks))
	for _, candidateTrack := range tracks {
		var embeddings [][]byte
		if err := repository.reader.WithContext(ctx).Model(&models.SpeakerTrackEvidence{}).
			Where("speaker_track_id = ? AND routing_state = 'routed' AND routing_embedding IS NOT NULL", candidateTrack.ID).
			Order("evidence_order ASC").Pluck("routing_embedding", &embeddings).Error; err != nil {
			return nil, fmt.Errorf("读取 continuity segment embedding 失败：%w", err)
		}
		result = append(result, RoutingCandidate{Track: candidateTrack, Embeddings: embeddings})
	}
	return result, nil
}

// NextProviderSegmentNo 在单 writer 事务内分配 provider 通道内稳定 segment 编号。
func (repository *Repository) NextProviderSegmentNo(ctx context.Context, tx *gorm.DB, sessionID string, label string) (int, error) {
	var number int
	if err := tx.WithContext(ctx).Raw(`SELECT COALESCE(MAX(provider_segment_no), 0) + 1
FROM speaker_tracks WHERE asr_session_id = ? AND asr_speaker_label = ?`, sessionID, label).Scan(&number).Error; err != nil {
		return 0, fmt.Errorf("分配 provider segment 编号失败：%w", err)
	}
	return number, nil
}

// CommitRouting 原子路由 evidence，必要时创建新 segment，并继承既有终态投影。
func (repository *Repository) CommitRouting(ctx context.Context, tx *gorm.DB, input RoutingCommit) (bool, bool, error) {
	var recoverableCount int64
	if err := tx.WithContext(ctx).Model(&models.SpeakerTrackEvidence{}).
		Where("id = ? AND routing_state IN ('pending', 'failed')", input.EvidenceID).Count(&recoverableCount).Error; err != nil {
		return false, false, fmt.Errorf("确认 continuity evidence 状态失败：%w", err)
	}
	if recoverableCount == 0 {
		return false, false, nil
	}
	if input.CreateTarget {
		if err := repository.CreateTrack(ctx, tx, input.TargetTrack); err != nil {
			return false, false, err
		}
	}
	order, err := repository.NextEvidenceOrder(ctx, tx, input.TargetTrack.ID)
	if err != nil {
		return false, false, err
	}
	updates := map[string]any{
		"speaker_track_id": input.TargetTrack.ID, "evidence_order": order, "routing_state": "routed", "routing_error_code": nil,
		"routing_duration_ms": input.DurationMS, "routing_score": input.Score, "routing_margin": input.Margin,
		"routing_model_id": input.ModelID, "routing_model_version": input.ModelVersion,
		"routing_model_sha256": input.ModelSHA256, "routing_dimension": input.Dimension,
		"routing_profile_id": input.ProfileID, "routing_embedding": input.Embedding, "updated_at": input.UpdatedAt,
	}
	result := tx.WithContext(ctx).Model(&models.SpeakerTrackEvidence{}).
		Where("id = ? AND routing_state IN ('pending', 'failed')", input.EvidenceID).Updates(updates)
	if result.Error != nil {
		return false, false, fmt.Errorf("提交 continuity evidence 失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return false, false, nil
	}
	if err := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("id = ?", input.UtteranceID).
		Updates(map[string]any{"speaker_track_id": input.TargetTrack.ID, "updated_at": input.UpdatedAt}).Error; err != nil {
		return false, false, fmt.Errorf("更新 continuity utterance track 失败：%w", err)
	}
	if err := bumpRoutingRevision(ctx, tx, input.TargetTrack.ID, input.UpdatedAt); err != nil {
		return false, false, err
	}
	if input.SourceTrackID != "" && input.SourceTrackID != input.TargetTrack.ID {
		if err := bumpRoutingRevision(ctx, tx, input.SourceTrackID, input.UpdatedAt); err != nil {
			return false, false, err
		}
	}
	evidence := models.SpeakerTrackEvidence{ID: input.EvidenceID, SpeakerTrackID: input.TargetTrack.ID, UtteranceID: input.UtteranceID}
	changed, err := repository.inheritTrackProjection(ctx, tx, evidence, input.UpdatedAt)
	return true, changed, err
}

// SkipManualRouting 把已人工分配的 utterance 排除出自动 continuity 与身份累计。
func (repository *Repository) SkipManualRouting(ctx context.Context, tx *gorm.DB, evidenceID string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.SpeakerTrackEvidence{}).
		Where("id = ? AND routing_state IN ('pending', 'failed')", evidenceID).
		Updates(map[string]any{"routing_state": "insufficient", "routing_error_code": nil, "excluded_reason": "manual_assignment", "updated_at": updatedAt})
	if result.Error != nil {
		return fmt.Errorf("排除人工 continuity evidence 失败：%w", result.Error)
	}
	return nil
}

// MarkRoutingFailure 记录可重试的 continuity 失败；恢复任务会再次处理该 evidence。
func (repository *Repository) MarkRoutingFailure(ctx context.Context, tx *gorm.DB, evidenceID string, errorCode string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.SpeakerTrackEvidence{}).
		Where("id = ? AND routing_state IN ('pending', 'failed')", evidenceID).
		Updates(map[string]any{"routing_state": "failed", "routing_error_code": errorCode, "updated_at": updatedAt})
	if result.Error != nil {
		return fmt.Errorf("记录 continuity routing 失败状态：%w", result.Error)
	}
	return nil
}

// bumpRoutingRevision 推进 segment 路由版本，供恢复与并发诊断使用。
func bumpRoutingRevision(ctx context.Context, tx *gorm.DB, trackID string, updatedAt int64) error {
	if err := tx.WithContext(ctx).Model(&models.SpeakerTrack{}).Where("id = ?", trackID).
		Updates(map[string]any{"routing_revision": gorm.Expr("routing_revision + 1"), "updated_at": updatedAt}).Error; err != nil {
		return fmt.Errorf("推进 continuity routing revision 失败：%w", err)
	}
	return nil
}

// ListRecoverableEvidenceIDs 按 final seq 返回待 continuity 路由的 SQLite 任务。
func (repository *Repository) ListRecoverableEvidenceIDs(ctx context.Context, limit int) ([]string, error) {
	return repository.listRecoverableEvidenceIDs(ctx, "", limit)
}

// ListRecoverableMeetingEvidenceIDs 只返回指定会议的待 continuity 路由任务。
func (repository *Repository) ListRecoverableMeetingEvidenceIDs(ctx context.Context, meetingID string, limit int) ([]string, error) {
	if meetingID == "" {
		return nil, fmt.Errorf("恢复 continuity evidence：会议 ID 为空")
	}
	return repository.listRecoverableEvidenceIDs(ctx, meetingID, limit)
}

// listRecoverableEvidenceIDs 按 final seq 返回全局或单场可重试 evidence。
func (repository *Repository) listRecoverableEvidenceIDs(ctx context.Context, meetingID string, limit int) ([]string, error) {
	if repository == nil || repository.reader == nil {
		return nil, fmt.Errorf("恢复 continuity evidence：数据库不可用")
	}
	if limit <= 0 || limit > 256 {
		limit = 64
	}
	const statement = `SELECT evidence.id
FROM speaker_track_evidence AS evidence
JOIN utterances AS utterance ON utterance.id = evidence.utterance_id
JOIN meeting_events AS event ON event.id = utterance.event_id
WHERE evidence.routing_state IN ('pending', 'failed') AND (? = '' OR utterance.meeting_id = ?)
ORDER BY event.seq ASC, evidence.id ASC LIMIT ?`
	var ids []string
	if err := repository.reader.WithContext(ctx).Raw(statement, meetingID, meetingID, limit).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("恢复 continuity evidence 查询失败：%w", err)
	}
	return ids, nil
}

// RoutingRecoverySource 把可重试 evidence 适配到现有有界后台池。
type RoutingRecoverySource struct{ Repository *Repository }

// ListRecoverableTrackIDs 返回 evidence ID；方法名仅用于复用后台池的通用恢复接口。
func (source RoutingRecoverySource) ListRecoverableTrackIDs(ctx context.Context, limit int) ([]string, error) {
	if source.Repository == nil {
		return nil, fmt.Errorf("恢复 continuity evidence：Repository 为空")
	}
	return source.Repository.ListRecoverableEvidenceIDs(ctx, limit)
}
