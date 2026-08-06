// Package correction 提供人工校对事务所需的最小 SQLite 读写能力。
package correction

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// Repository 负责 correction 事实、目标投影和统一事件的事务内读写。
type Repository struct{ reader *gorm.DB }

// ClusterUtteranceRow 是批量校对逐条审计所需的稳定 seq 投影。
type ClusterUtteranceRow struct {
	models.Utterance
	FinalSeq int64 `gorm:"column:final_seq"`
}

// MeetingClipEnrollmentFact 是二次确认后提取永久声纹所需的同场正式成员事实。
type MeetingClipEnrollmentFact struct {
	MeetingID   string `gorm:"column:meeting_id"`
	UtteranceID string `gorm:"column:utterance_id"`
	MemberID    string `gorm:"column:member_id"`
	StartSample int64  `gorm:"column:start_sample"`
	EndSample   int64  `gorm:"column:end_sample"`
}

// GetMeetingClipEnrollmentFact 只允许 ended/interrupted 会议中当前归属正式成员的 utterance。
func (repository *Repository) GetMeetingClipEnrollmentFact(ctx context.Context, tx *gorm.DB, meetingID string, utteranceID string) (MeetingClipEnrollmentFact, error) {
	const statement = `SELECT utterance.meeting_id, utterance.id AS utterance_id, participant.member_id,
       utterance.start_sample, utterance.end_sample
FROM utterances AS utterance
JOIN meetings AS meeting ON meeting.id = utterance.meeting_id
JOIN meeting_participants AS participant ON participant.id = utterance.current_participant_id
WHERE utterance.id = ? AND utterance.meeting_id = ?
  AND meeting.lifecycle_state IN ('ended', 'interrupted')
  AND participant.participant_kind = 'member' AND participant.member_id IS NOT NULL`
	var fact MeetingClipEnrollmentFact
	if err := tx.WithContext(ctx).Raw(statement, utteranceID, meetingID).Take(&fact).Error; err != nil {
		return MeetingClipEnrollmentFact{}, fmt.Errorf("读取会议片段声纹事实失败：%w", err)
	}
	return fact, nil
}

// NewRepository 创建 correction Repository；可选 reader 用于 Wails 分页快照。
func NewRepository(readers ...*gorm.DB) *Repository {
	var reader *gorm.DB
	if len(readers) > 0 {
		reader = readers[0]
	}
	return &Repository{reader: reader}
}

// EntryRow 是校对工作台分页读取的最小安全投影。
type EntryRow struct {
	Seq                    int64  `gorm:"column:seq"`
	UtteranceID            string `gorm:"column:utterance_id"`
	StartSample            int64  `gorm:"column:start_sample"`
	EndSample              int64  `gorm:"column:end_sample"`
	OriginalText           string `gorm:"column:original_text"`
	CurrentText            string `gorm:"column:current_text"`
	CurrentParticipantID   string `gorm:"column:current_participant_id"`
	ParticipantDisplayName string `gorm:"column:participant_display_name"`
	SpeakerClusterID       string `gorm:"column:speaker_cluster_id"`
	ClusterDisplayNo       int    `gorm:"column:cluster_display_no"`
	TrackDisplayNo         int    `gorm:"column:track_display_no"`
	ClusterParticipantID   string `gorm:"column:cluster_participant_id"`
	AssignmentSource       string `gorm:"column:speaker_assignment_source"`
	TextRevision           int    `gorm:"column:text_revision"`
	SpeakerRevision        int    `gorm:"column:speaker_revision"`
	ClusterRevision        int    `gorm:"column:cluster_revision"`
	ClusterCount           int    `gorm:"column:cluster_count"`
	AudioReady             bool   `gorm:"column:audio_ready"`
}

// ParticipantRow 是本场人工 speaker 选择所需的快照投影。
type ParticipantRow struct {
	ID          string `gorm:"column:id"`
	DisplayName string `gorm:"column:display_name_snapshot"`
	Kind        string `gorm:"column:participant_kind"`
	MemberID    string `gorm:"column:member_id"`
}

// ListEntries 按 final seq 分页读取当前文字/说话人，不返回模型、embedding 或路径。
func (repository *Repository) ListEntries(ctx context.Context, meetingID string, afterSeq int64, limit int) ([]EntryRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取 correction entries：参数无效")
	}
	const statement = `SELECT event.seq, utterance.id AS utterance_id, utterance.start_sample, utterance.end_sample,
       utterance.original_text, utterance.current_text,
       COALESCE(utterance.current_participant_id, '') AS current_participant_id,
       COALESCE(participant.display_name_snapshot, '') AS participant_display_name,
       COALESCE(utterance.speaker_cluster_id, '') AS speaker_cluster_id,
       COALESCE(cluster.display_no, 0) AS cluster_display_no,
	   COALESCE(track.display_no, 0) AS track_display_no,
		COALESCE(cluster.assigned_participant_id, '') AS cluster_participant_id,
       utterance.speaker_assignment_source, utterance.text_revision, utterance.speaker_revision,
       COALESCE(cluster.revision, 0) AS cluster_revision,
       CASE WHEN cluster.id IS NULL THEN 0 ELSE (SELECT COUNT(*) FROM utterances scoped WHERE scoped.meeting_id=utterance.meeting_id AND scoped.speaker_cluster_id=cluster.id) END AS cluster_count,
       EXISTS(SELECT 1 FROM audio_assets audio WHERE audio.meeting_id=utterance.meeting_id AND audio.state='ready' AND audio.kind IN ('microphone','mixed') AND audio.start_sample <= utterance.start_sample AND audio.end_sample >= utterance.end_sample) AS audio_ready
FROM meeting_events AS event
JOIN utterances AS utterance ON utterance.event_id=event.id
LEFT JOIN agent_voice_command_utterances AS voice_command ON voice_command.utterance_id=utterance.id
LEFT JOIN meeting_participants AS participant ON participant.id=utterance.current_participant_id
LEFT JOIN speaker_clusters AS cluster ON cluster.id=utterance.speaker_cluster_id
LEFT JOIN speaker_tracks AS track ON track.id=utterance.speaker_track_id
WHERE event.meeting_id=? AND event.kind='utterance.final' AND event.seq>?
  AND (voice_command.id IS NULL OR voice_command.state='released')
ORDER BY event.seq ASC LIMIT ?`
	var rows []EntryRow
	if err := repository.reader.WithContext(ctx).Raw(statement, meetingID, afterSeq, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取 correction entries 失败：%w", err)
	}
	return rows, nil
}

// ListParticipants 返回本场可供人工校对选择的正式/临时 participant 快照。
func (repository *Repository) ListParticipants(ctx context.Context, meetingID string) ([]ParticipantRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取 correction participants：参数无效")
	}
	var rows []ParticipantRow
	if err := repository.reader.WithContext(ctx).Raw(`SELECT id, display_name_snapshot, participant_kind, COALESCE(member_id,'') AS member_id
FROM meeting_participants WHERE meeting_id=? ORDER BY sort_order ASC, id ASC`, meetingID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取 correction participants 失败：%w", err)
	}
	return rows, nil
}

// FindEntryCursor 返回单条 utterance 所属会议和 final seq，用同一分页映射恢复详情。
func (repository *Repository) FindEntryCursor(ctx context.Context, utteranceID string) (string, int64, error) {
	if repository == nil || repository.reader == nil || utteranceID == "" {
		return "", 0, fmt.Errorf("读取 correction entry cursor：参数无效")
	}
	var row struct {
		MeetingID string `gorm:"column:meeting_id"`
		Seq       int64  `gorm:"column:seq"`
	}
	if err := repository.reader.WithContext(ctx).Raw(`SELECT utterance.meeting_id, event.seq
FROM utterances AS utterance JOIN meeting_events AS event ON event.id=utterance.event_id
WHERE utterance.id=? AND event.kind='utterance.final'`, utteranceID).Take(&row).Error; err != nil {
		return "", 0, fmt.Errorf("读取 correction entry cursor 失败：%w", err)
	}
	return row.MeetingID, row.Seq, nil
}

// FindByRequest 按全局 request ID 查询已提交校对。
func (repository *Repository) FindByRequest(ctx context.Context, tx *gorm.DB, requestID string) (*models.Correction, error) {
	var correction models.Correction
	err := tx.WithContext(ctx).Where("request_id = ?", requestID).Take(&correction).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询 correction 幂等记录失败：%w", err)
	}
	return &correction, nil
}

// GetMeeting 读取校对会议状态。
func (repository *Repository) GetMeeting(ctx context.Context, tx *gorm.DB, meetingID string) (models.Meeting, error) {
	var meeting models.Meeting
	if err := tx.WithContext(ctx).Where("id = ?", meetingID).Take(&meeting).Error; err != nil {
		return models.Meeting{}, fmt.Errorf("读取 correction 会议失败：%w", err)
	}
	return meeting, nil
}

// GetUtterance 读取同场单条转写目标。
func (repository *Repository) GetUtterance(ctx context.Context, tx *gorm.DB, meetingID string, targetID string) (models.Utterance, error) {
	var value models.Utterance
	if err := tx.WithContext(ctx).Where("id = ? AND meeting_id = ?", targetID, meetingID).Take(&value).Error; err != nil {
		return models.Utterance{}, fmt.Errorf("读取 correction utterance 失败：%w", err)
	}
	return value, nil
}

// GetResource 读取同场 completed resource 目标。
func (repository *Repository) GetResource(ctx context.Context, tx *gorm.DB, meetingID string, targetID string) (models.Resource, error) {
	var value models.Resource
	if err := tx.WithContext(ctx).Where("id = ? AND meeting_id = ? AND state = 'completed'", targetID, meetingID).Take(&value).Error; err != nil {
		return models.Resource{}, fmt.Errorf("读取 correction resource 失败：%w", err)
	}
	return value, nil
}

// GetCluster 读取同场 speaker cluster。
func (repository *Repository) GetCluster(ctx context.Context, tx *gorm.DB, meetingID string, clusterID string) (models.SpeakerCluster, error) {
	var value models.SpeakerCluster
	if err := tx.WithContext(ctx).Where("id = ? AND meeting_id = ?", clusterID, meetingID).Take(&value).Error; err != nil {
		return models.SpeakerCluster{}, fmt.Errorf("读取 correction cluster 失败：%w", err)
	}
	return value, nil
}

// ListClusterUtterances 按 final seq 返回指定 meeting + cluster 的当前全部片段。
func (repository *Repository) ListClusterUtterances(ctx context.Context, tx *gorm.DB, meetingID string, clusterID string) ([]ClusterUtteranceRow, error) {
	const statement = `SELECT utterance.*, event.seq AS final_seq
FROM utterances AS utterance
JOIN meeting_events AS event ON event.id = utterance.event_id
WHERE utterance.meeting_id = ? AND utterance.speaker_cluster_id = ?
ORDER BY event.seq ASC, utterance.id ASC`
	var rows []ClusterUtteranceRow
	if err := tx.WithContext(ctx).Raw(statement, meetingID, clusterID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取 cluster utterances 失败：%w", err)
	}
	return rows, nil
}

// ParticipantExists 验证目标 participant 属于同一会议。
func (repository *Repository) ParticipantExists(ctx context.Context, tx *gorm.DB, meetingID string, participantID string) (bool, error) {
	var count int64
	err := tx.WithContext(ctx).Model(&models.MeetingParticipant{}).
		Where("id = ? AND meeting_id = ?", participantID, meetingID).Count(&count).Error
	return count == 1, err
}

// UpdateUtteranceText 以 expected revision 更新 current text，保留 original text。
func (repository *Repository) UpdateUtteranceText(ctx context.Context, tx *gorm.DB, target models.Utterance, text string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("id = ? AND text_revision = ?", target.ID, target.TextRevision).
		Updates(map[string]any{"current_text": text, "text_revision": target.TextRevision + 1, "updated_at": updatedAt})
	return requireSingleUpdate(result, "更新 utterance text")
}

// UpdateUtteranceSpeaker 只更新单条 current speaker 投影，保留自动 track/cluster/score 历史。
func (repository *Repository) UpdateUtteranceSpeaker(ctx context.Context, tx *gorm.DB, target models.Utterance, participantID string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("id = ? AND speaker_revision = ?", target.ID, target.SpeakerRevision).
		Updates(map[string]any{
			"current_participant_id": participantID, "speaker_assignment_source": "manual_single",
			"speaker_confidence": nil, "speaker_revision": target.SpeakerRevision + 1, "updated_at": updatedAt,
		})
	return requireSingleUpdate(result, "更新 utterance speaker")
}

// UpdateResourceDescription 以 expected revision 更新 current description，保留 original description。
func (repository *Repository) UpdateResourceDescription(ctx context.Context, tx *gorm.DB, target models.Resource, description string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.Resource{}).
		Where("id = ? AND description_revision = ?", target.ID, target.DescriptionRevision).
		Updates(map[string]any{"current_description": description, "description_revision": target.DescriptionRevision + 1, "updated_at": updatedAt})
	return requireSingleUpdate(result, "更新 resource description")
}

// AssignCluster 原子更新 cluster assignment/revision 和当前全部 utterance 投影。
func (repository *Repository) AssignCluster(ctx context.Context, tx *gorm.DB, cluster models.SpeakerCluster, participantID string, updatedAt int64) error {
	result := tx.WithContext(ctx).Model(&models.SpeakerCluster{}).
		Where("id = ? AND revision = ?", cluster.ID, cluster.Revision).
		Updates(map[string]any{
			"assigned_participant_id": participantID, "assignment_source": "manual",
			"revision": cluster.Revision + 1, "updated_at": updatedAt,
		})
	if err := requireSingleUpdate(result, "更新 speaker cluster"); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Model(&models.Utterance{}).
		Where("meeting_id = ? AND speaker_cluster_id = ?", cluster.MeetingID, cluster.ID).
		Updates(map[string]any{
			"current_participant_id": participantID, "speaker_assignment_source": "manual_cluster",
			"speaker_confidence": nil, "speaker_revision": gorm.Expr("speaker_revision + 1"), "updated_at": updatedAt,
		}).Error; err != nil {
		return fmt.Errorf("批量更新 cluster utterances 失败：%w", err)
	}
	return nil
}

// NextEventSeq 分配当前会议下一统一事件序号。
func (repository *Repository) NextEventSeq(ctx context.Context, tx *gorm.DB, meetingID string) (int64, error) {
	var seq int64
	if err := tx.WithContext(ctx).Raw("SELECT COALESCE(MAX(seq), 0) + 1 FROM meeting_events WHERE meeting_id = ?", meetingID).Scan(&seq).Error; err != nil {
		return 0, fmt.Errorf("分配 correction event seq 失败：%w", err)
	}
	return seq, nil
}

// CreateAudit 原子写统一事件、correction 和逐条 item。
func (repository *Repository) CreateAudit(ctx context.Context, tx *gorm.DB, event models.MeetingEvent, correction models.Correction, item models.CorrectionItem) error {
	return repository.CreateBatchAudit(ctx, tx, event, correction, []models.CorrectionItem{item})
}

// CreateBatchAudit 原子写统一事件、correction 和有序逐条 items。
func (repository *Repository) CreateBatchAudit(ctx context.Context, tx *gorm.DB, event models.MeetingEvent, correction models.Correction, items []models.CorrectionItem) error {
	if err := tx.WithContext(ctx).Create(&event).Error; err != nil {
		return fmt.Errorf("创建 correction event 失败：%w", err)
	}
	if err := tx.WithContext(ctx).Create(&correction).Error; err != nil {
		return fmt.Errorf("创建 correction 失败：%w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("correction items 不能为空")
	}
	if err := tx.WithContext(ctx).Create(&items).Error; err != nil {
		return fmt.Errorf("创建 correction item 失败：%w", err)
	}
	return nil
}

// requireSingleUpdate 将乐观锁零行统一为明确错误。
func requireSingleUpdate(result *gorm.DB, operation string) error {
	if result.Error != nil {
		return fmt.Errorf("%s失败：%w", operation, result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%s失败：目标 revision 已变化", operation)
	}
	return nil
}
