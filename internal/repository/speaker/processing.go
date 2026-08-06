package speaker

import (
	"context"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// ProcessingUtterance 是 Runner 构造 evidence 所需的 session 级 final 事实。
type ProcessingUtterance struct {
	ID              string `gorm:"column:id"`
	SpeakerTrackID  string `gorm:"column:speaker_track_id"`
	ASRSessionID    string `gorm:"column:asr_session_id"`
	ASRSpeakerLabel string `gorm:"column:asr_speaker_label"`
	FinalSeq        int64  `gorm:"column:final_seq"`
	StartSample     int64  `gorm:"column:start_sample"`
	EndSample       int64  `gorm:"column:end_sample"`
}

// ProcessingSnapshot 是一次 Runner 事务外计算使用的稳定输入快照。
type ProcessingSnapshot struct {
	Track      models.SpeakerTrack
	Utterances []ProcessingUtterance
}

// AudioClipFact 是安全 clip 计算所需的 utterance 与 ready 音频边界。
type AudioClipFact struct {
	Utterance models.Utterance
	AudioEnd  int64
}

// LoadAudioClipFact 读取 utterance，并只以数据库 ready 资产确定本场可回放边界。
func (repository *Repository) LoadAudioClipFact(ctx context.Context, utteranceID string) (AudioClipFact, error) {
	if repository == nil || repository.reader == nil || utteranceID == "" {
		return AudioClipFact{}, fmt.Errorf("读取 audio clip 事实：参数无效")
	}
	var utterance models.Utterance
	if err := repository.reader.WithContext(ctx).Where("id = ?", utteranceID).Take(&utterance).Error; err != nil {
		return AudioClipFact{}, fmt.Errorf("读取 audio clip utterance 失败：%w", err)
	}
	var audioEnd int64
	if err := repository.reader.WithContext(ctx).Raw(`SELECT COALESCE(MAX(end_sample), 0)
FROM audio_assets WHERE meeting_id = ? AND state = 'ready' AND kind IN ('microphone', 'mixed')`, utterance.MeetingID).Scan(&audioEnd).Error; err != nil {
		return AudioClipFact{}, fmt.Errorf("读取 audio clip 资产边界失败：%w", err)
	}
	return AudioClipFact{Utterance: utterance, AudioEnd: audioEnd}, nil
}

// TrackEmbeddingUpdate 描述一次 embedding 决策需要持久化的完整 track 字段。
type TrackEmbeddingUpdate struct {
	State              string
	ParticipantID      *string
	TopScore           *float64
	RunnerUpScore      *float64
	EvidenceDurationMS int64
	ModelID            string
	ModelVersion       string
	ModelSHA256        string
	Dimension          int
	Embedding          []byte
	ProfileID          string
	UpdatedAt          int64
}

// LoadProcessingSnapshot 只读取已 continuity 路由到当前 segment 的 final。
func (repository *Repository) LoadProcessingSnapshot(ctx context.Context, trackID string) (ProcessingSnapshot, error) {
	if repository == nil || repository.reader == nil || trackID == "" {
		return ProcessingSnapshot{}, fmt.Errorf("读取 speaker 处理快照：参数无效")
	}
	var track models.SpeakerTrack
	if err := repository.reader.WithContext(ctx).Where("id = ?", trackID).Take(&track).Error; err != nil {
		return ProcessingSnapshot{}, fmt.Errorf("读取 speaker 处理 track 失败：%w", err)
	}
	const statement = `SELECT utterance.id, utterance.asr_session_id,
       COALESCE(utterance.asr_speaker_label, '') AS asr_speaker_label,
       CASE WHEN evidence.routing_state = 'routed' THEN COALESCE(evidence.speaker_track_id, '') ELSE '' END AS speaker_track_id,
       event.seq AS final_seq, utterance.start_sample, utterance.end_sample
FROM utterances AS utterance
LEFT JOIN speaker_track_evidence AS evidence ON evidence.utterance_id = utterance.id
JOIN meeting_events AS event ON event.id = utterance.event_id
WHERE utterance.asr_session_id = ? AND event.kind = 'utterance.final'
ORDER BY event.seq ASC, utterance.id ASC`
	var utterances []ProcessingUtterance
	if err := repository.reader.WithContext(ctx).Raw(statement, track.ASRSessionID).Scan(&utterances).Error; err != nil {
		return ProcessingSnapshot{}, fmt.Errorf("读取 speaker session final 失败：%w", err)
	}
	return ProcessingSnapshot{Track: track, Utterances: utterances}, nil
}

// IsTrackMeetingFinalized 判断 track 所属会议是否已结束或中断，用于恢复时收敛短证据状态。
func (repository *Repository) IsTrackMeetingFinalized(ctx context.Context, trackID string) (bool, error) {
	if repository == nil || repository.reader == nil || trackID == "" {
		return false, fmt.Errorf("读取 speaker 会议终态：参数无效")
	}
	var lifecycleState string
	err := repository.reader.WithContext(ctx).Raw(`SELECT meeting.lifecycle_state
FROM speaker_tracks AS track
JOIN meetings AS meeting ON meeting.id = track.meeting_id
WHERE track.id = ?`, trackID).Scan(&lifecycleState).Error
	if err != nil {
		return false, fmt.Errorf("读取 speaker 会议终态失败：%w", err)
	}
	if lifecycleState == "" {
		return false, fmt.Errorf("读取 speaker 会议终态失败：track 不存在")
	}
	return lifecycleState == "ended" || lifecycleState == "interrupted", nil
}

// UpdateEvidenceState 在短事务中更新逐条 evidence 和尚未进入推理的 track 状态。
func (repository *Repository) UpdateEvidenceState(
	ctx context.Context,
	tx *gorm.DB,
	track models.SpeakerTrack,
	items []EvidenceItemUpdate,
	state string,
	durationMS int64,
	updatedAt int64,
) error {
	if err := repository.updateEvidenceItems(ctx, tx, track.ID, items, updatedAt); err != nil {
		return err
	}
	result := tx.WithContext(ctx).Model(&models.SpeakerTrack{}).
		Where("id = ? AND revision = ?", track.ID, track.Revision).
		Updates(map[string]any{
			"state": state, "evidence_duration_ms": durationMS, "last_error_code": nil,
			"revision": track.Revision + 1, "updated_at": updatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("更新 speaker evidence 状态失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("speaker track 已被其他任务更新")
	}
	return nil
}

// MarkTrackFailure 持久化稳定失败状态与错误码，使后台失败可见且不会伪装成成功。
func (repository *Repository) MarkTrackFailure(
	ctx context.Context,
	tx *gorm.DB,
	track models.SpeakerTrack,
	items []EvidenceItemUpdate,
	state string,
	durationMS int64,
	errorCode string,
	updatedAt int64,
) error {
	if err := repository.updateEvidenceItems(ctx, tx, track.ID, items, updatedAt); err != nil {
		return err
	}
	result := tx.WithContext(ctx).Model(&models.SpeakerTrack{}).
		Where("id = ? AND revision = ?", track.ID, track.Revision).
		Updates(map[string]any{
			"state": state, "evidence_duration_ms": durationMS, "last_error_code": errorCode,
			"revision": track.Revision + 1, "updated_at": updatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("记录 speaker 处理失败状态失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("speaker track 已被其他任务更新")
	}
	return nil
}

// CommitEmbeddingDecision 原子提交 embedding、成员决定、utterance 投影和可选 attribution event。
func (repository *Repository) CommitEmbeddingDecision(
	ctx context.Context,
	tx *gorm.DB,
	track models.SpeakerTrack,
	items []EvidenceItemUpdate,
	update TrackEmbeddingUpdate,
	event *models.MeetingEvent,
) error {
	if err := repository.updateEvidenceItems(ctx, tx, track.ID, items, update.UpdatedAt); err != nil {
		return err
	}
	values := map[string]any{
		"state": update.State, "automatic_participant_id": update.ParticipantID,
		"speaker_cluster_id": nil, "top_score": update.TopScore, "runner_up_score": update.RunnerUpScore,
		"evidence_duration_ms": update.EvidenceDurationMS, "model_id": update.ModelID,
		"model_version": update.ModelVersion, "model_sha256": update.ModelSHA256,
		"dimension": update.Dimension, "embedding": update.Embedding, "profile_id": update.ProfileID,
		"last_error_code": nil, "revision": track.Revision + 1, "updated_at": update.UpdatedAt,
	}
	result := tx.WithContext(ctx).Model(&models.SpeakerTrack{}).
		Where("id = ? AND revision = ? AND speaker_cluster_id IS NULL", track.ID, track.Revision).Updates(values)
	if result.Error != nil {
		return fmt.Errorf("提交 speaker embedding 决策失败：%w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("speaker track 已被其他任务处理")
	}
	if update.ParticipantID != nil {
		if err := repository.projectMemberDecision(ctx, tx, track.ID, *update.ParticipantID, update.TopScore, update.UpdatedAt); err != nil {
			return err
		}
	}
	if event != nil {
		return repository.CreateAttributionEvent(ctx, tx, *event)
	}
	return nil
}

// EvidenceItemUpdate 是 service evidence 结果映射到 repository 的稳定写模型。
type EvidenceItemUpdate struct {
	UtteranceID    string
	EvidenceOrder  int
	OverlapRisk    bool
	Included       bool
	ExcludedReason *string
}

// updateEvidenceItems 只更新当前 track 已存在的 evidence，不创建推测性事实。
func (repository *Repository) updateEvidenceItems(ctx context.Context, tx *gorm.DB, trackID string, items []EvidenceItemUpdate, updatedAt int64) error {
	for _, item := range items {
		result := tx.WithContext(ctx).Model(&models.SpeakerTrackEvidence{}).
			Where("speaker_track_id = ? AND utterance_id = ?", trackID, item.UtteranceID).
			Updates(map[string]any{
				"evidence_order": item.EvidenceOrder, "overlap_risk": item.OverlapRisk,
				"included": item.Included, "excluded_reason": item.ExcludedReason, "updated_at": updatedAt,
			})
		if result.Error != nil {
			return fmt.Errorf("更新 speaker evidence 明细失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("speaker evidence 不存在或已变化")
		}
	}
	return nil
}

// projectMemberDecision 只覆盖自动投影，绝不覆盖 manual_single/manual_cluster。
func (repository *Repository) projectMemberDecision(
	ctx context.Context,
	tx *gorm.DB,
	trackID string,
	participantID string,
	confidence *float64,
	updatedAt int64,
) error {
	updates := map[string]any{
		"current_participant_id": participantID, "speaker_cluster_id": nil,
		"speaker_assignment_source": "automatic_member", "speaker_confidence": confidence,
		"speaker_revision": gorm.Expr("speaker_revision + 1"), "updated_at": updatedAt,
	}
	if err := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("speaker_track_id = ? AND speaker_assignment_source NOT IN ?", trackID, []string{"manual_single", "manual_cluster"}).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("更新成员 utterance 投影失败：%w", err)
	}
	return nil
}
