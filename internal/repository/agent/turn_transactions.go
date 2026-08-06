package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateQuestionInput 描述原子创建问题事实的全部已验证输入。
type CreateQuestionInput struct {
	Turn           models.AgentTurn
	Event          models.MeetingEvent
	Text           string
	Trigger        string
	UtteranceID    *string
	UtteranceIDs   []string
	VoiceCommandID string
	UpdatedAt      int64
}

// CreateQuestionResult 返回持久问题和固定 cutoff seq；Existing 表示幂等复用。
type CreateQuestionResult struct {
	Turn     models.AgentTurn
	Event    models.MeetingEvent
	Existing bool
}

// CreateQuestion 原子创建 pending turn、ai.question 并把会议切为 busy。
func (repository *Repository) CreateQuestion(ctx context.Context, input CreateQuestionInput) (CreateQuestionResult, error) {
	if repository == nil || repository.transactions == nil || input.Turn.ID == "" || input.Turn.MeetingID == "" || input.Turn.AgentSessionID == "" || input.Turn.IdempotencyKey == "" || input.Event.ID == "" {
		return CreateQuestionResult{}, fmt.Errorf("创建智能体问题：参数无效")
	}
	var result CreateQuestionResult
	err := repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		existing, event, err := findQuestionByIdempotency(ctx, tx, input.Turn.AgentSessionID, input.Turn.IdempotencyKey)
		if err != nil {
			return err
		}
		if existing != nil {
			result = CreateQuestionResult{Turn: *existing, Event: *event, Existing: true}
			return nil
		}
		meetingResult := tx.WithContext(ctx).Model(&models.Meeting{}).
			Where("id = ? AND lifecycle_state = 'recording' AND agent_state = 'available'", input.Turn.MeetingID).
			Updates(map[string]any{"agent_state": "busy", "updated_at": input.UpdatedAt})
		if meetingResult.Error != nil {
			return fmt.Errorf("取得智能体问题执行权失败：%w", meetingResult.Error)
		}
		if meetingResult.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.WithContext(ctx).Select(turnColumns()).Create(&input.Turn).Error; err != nil {
			return mapWriteError("创建 pending turn", err)
		}
		sequence, err := nextEventSeq(ctx, tx, input.Turn.MeetingID)
		if err != nil {
			return err
		}
		speakerKey, speakerLabel, err := repository.questionSpeakerSnapshot(ctx, tx, input)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"v": 3, "text": input.Text, "content_format": "markdown", "trigger": input.Trigger,
			"trigger_utterance_id": input.UtteranceID, "trigger_utterance_ids": input.UtteranceIDs,
			"speaker_key_snapshot": speakerKey, "speaker_label_snapshot": speakerLabel,
		})
		if err != nil {
			return fmt.Errorf("编码问题事件失败：%w", err)
		}
		entityType := "agent_turn"
		input.Event.Seq, input.Event.Kind, input.Event.Source = sequence, "ai.question", "host"
		input.Event.EntityType, input.Event.EntityID = &entityType, &input.Turn.ID
		payloadJSON := string(payload)
		input.Event.PayloadJSON = &payloadJSON
		if err := tx.WithContext(ctx).Select(eventColumns()).Create(&input.Event).Error; err != nil {
			return fmt.Errorf("创建问题事件失败：%w", err)
		}
		update := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state = 'pending'", input.Turn.ID).
			Updates(map[string]any{"question_event_id": input.Event.ID, "updated_at": input.UpdatedAt})
		if update.Error != nil || update.RowsAffected != 1 {
			return fmt.Errorf("关联问题事件失败：%w", update.Error)
		}
		input.Turn.QuestionEventID = &input.Event.ID
		if err := consumeVoiceCommand(ctx, tx, input); err != nil {
			return err
		}
		result = CreateQuestionResult{Turn: input.Turn, Event: input.Event}
		return nil
	})
	return result, err
}

// questionSpeakerSnapshot 在问题事务中保存触发 utterance 的身份快照；手动提问固定属于主持人。
func (repository *Repository) questionSpeakerSnapshot(ctx context.Context, tx *gorm.DB, input CreateQuestionInput) (string, string, error) {
	if input.Trigger != "wake_word" || input.UtteranceID == nil || *input.UtteranceID == "" {
		return "host", "你", nil
	}
	var row struct {
		ParticipantID    string `gorm:"column:participant_id"`
		ParticipantName  string `gorm:"column:participant_name"`
		ClusterID        string `gorm:"column:cluster_id"`
		ClusterDisplayNo int    `gorm:"column:cluster_display_no"`
		TrackID          string `gorm:"column:track_id"`
		TrackDisplayNo   int    `gorm:"column:track_display_no"`
	}
	err := tx.WithContext(ctx).Raw(`SELECT
    COALESCE(utterance.current_participant_id, '') AS participant_id,
    COALESCE(participant.display_name_snapshot, '') AS participant_name,
    COALESCE(utterance.speaker_cluster_id, '') AS cluster_id,
    COALESCE(cluster.display_no, 0) AS cluster_display_no,
    COALESCE(utterance.speaker_track_id, '') AS track_id,
    COALESCE(track.display_no, 0) AS track_display_no
FROM utterances AS utterance
LEFT JOIN meeting_participants AS participant ON participant.id = utterance.current_participant_id
LEFT JOIN speaker_clusters AS cluster ON cluster.id = utterance.speaker_cluster_id
LEFT JOIN speaker_tracks AS track ON track.id = utterance.speaker_track_id
WHERE utterance.id = ? AND utterance.meeting_id = ?`, *input.UtteranceID, input.Turn.MeetingID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "未识别说话人", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("读取 AI 提问者身份失败：%w", err)
	}
	return speakerSnapshotIdentity(row.ParticipantID, row.ParticipantName, row.ClusterID, row.ClusterDisplayNo, row.TrackID, row.TrackDisplayNo),
		speakerdomain.DisplayName(row.ParticipantName, row.ClusterDisplayNo, row.TrackDisplayNo), nil
}

// speakerSnapshotIdentity 返回与时间线一致的稳定说话人键，供审计快照和前端去重使用。
func speakerSnapshotIdentity(participantID string, participantName string, clusterID string, clusterDisplayNo int, trackID string, trackDisplayNo int) string {
	if participantID != "" && participantName != "" {
		return "participant:" + participantID
	}
	if clusterID != "" && clusterDisplayNo > 0 {
		return "cluster:" + clusterID
	}
	if trackID != "" && trackDisplayNo > 0 {
		return "track:" + trackID
	}
	return ""
}

// consumeVoiceCommand 校验并原子绑定语音指令的全部有序 final。
func consumeVoiceCommand(ctx context.Context, tx *gorm.DB, input CreateQuestionInput) error {
	if input.Trigger != "wake_word" {
		return nil
	}
	if input.VoiceCommandID == "" || len(input.UtteranceIDs) == 0 {
		return fmt.Errorf("消费语音指令：触发关系不完整")
	}
	var relations []models.AgentVoiceCommandUtterance
	if err := tx.WithContext(ctx).Where("command_id = ? AND state = 'candidate'", input.VoiceCommandID).
		Order("position ASC").Find(&relations).Error; err != nil {
		return fmt.Errorf("读取语音指令候选失败：%w", err)
	}
	if len(relations) != len(input.UtteranceIDs) {
		return ErrConflict
	}
	for index, relation := range relations {
		if relation.MeetingID != input.Turn.MeetingID || relation.Position != index || relation.UtteranceID != input.UtteranceIDs[index] {
			return ErrConflict
		}
	}
	result := tx.WithContext(ctx).Model(&models.AgentVoiceCommandUtterance{}).
		Where("command_id = ? AND state = 'candidate'", input.VoiceCommandID).
		Updates(map[string]any{"state": "consumed", "agent_turn_id": input.Turn.ID, "updated_at": input.UpdatedAt})
	if result.Error != nil {
		return fmt.Errorf("消费语音指令候选失败：%w", result.Error)
	}
	if result.RowsAffected != int64(len(relations)) {
		return ErrConflict
	}
	return nil
}

// MarkTurnRunning 比较 pending 状态写入唯一 provider turn ID。
func (repository *Repository) MarkTurnRunning(ctx context.Context, turnID string, providerTurnID string, startedAt int64) error {
	if repository == nil || repository.transactions == nil || turnID == "" || providerTurnID == "" {
		return fmt.Errorf("启动智能体 turn：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state = 'pending'", turnID).
			Updates(map[string]any{"provider_turn_id": providerTurnID, "state": "running", "started_at": startedAt, "updated_at": startedAt})
		if result.Error != nil {
			return fmt.Errorf("启动智能体 turn 失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// SetRunningProviderTurn 更新同一用户任务当前执行的 provider work unit 身份。
func (repository *Repository) SetRunningProviderTurn(ctx context.Context, turnID string, providerTurnID string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || turnID == "" || providerTurnID == "" {
		return fmt.Errorf("更新 provider turn：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state = 'running'", turnID).
			Updates(map[string]any{"provider_turn_id": providerTurnID, "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("更新 provider turn 失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// MarkBatchRunning 把一个 pending batch 切为 running 并增加尝试次数。
func (repository *Repository) MarkBatchRunning(ctx context.Context, batchID string, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || batchID == "" {
		return fmt.Errorf("启动同步批次：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		result := tx.WithContext(ctx).Model(&models.SyncBatch{}).Where("id = ? AND state = 'pending'", batchID).
			Updates(map[string]any{"state": "running", "attempt_count": gorm.Expr("attempt_count + 1"), "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("启动同步批次失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// CommitIngest 原子提交一个成功 ingest 的 snapshot 和 batch 游标，用户任务仍保持 running。
func (repository *Repository) CommitIngest(ctx context.Context, turnID string, providerTurnID string, batchID string, snapshot models.ContextSnapshot, updatedAt int64) error {
	if repository == nil || repository.transactions == nil || turnID == "" || providerTurnID == "" || batchID == "" {
		return fmt.Errorf("提交 ingest：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		turn, err := getRunningTurn(ctx, tx, turnID, providerTurnID)
		if err != nil {
			return err
		}
		if err := upsertSnapshotTx(ctx, tx, snapshot); err != nil {
			return err
		}
		result := tx.WithContext(ctx).Model(&models.SyncBatch{}).
			Where("id = ? AND agent_session_id = ? AND state = 'running'", batchID, turn.AgentSessionID).
			Updates(map[string]any{"state": "completed", "updated_at": updatedAt})
		if result.Error != nil {
			return fmt.Errorf("完成 ingest 批次失败：%w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// CommitTurnSuccessInput 描述一次成功 work unit 的原子提交事实。
type CommitTurnSuccessInput struct {
	TurnID         string
	ProviderTurnID string
	AnswerEvent    *models.MeetingEvent
	AnswerText     string
	Snapshot       models.ContextSnapshot
	BatchIDs       []string
	UpdatedAt      int64
}

// CommitTurnSuccess 原子提交回答、滚动快照、batch、turn 和 meeting 状态。
func (repository *Repository) CommitTurnSuccess(ctx context.Context, input CommitTurnSuccessInput) error {
	if repository == nil || repository.transactions == nil || input.TurnID == "" || input.ProviderTurnID == "" || input.Snapshot.AgentSessionID == "" {
		return fmt.Errorf("提交智能体成功结果：参数无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		turn, err := getRunningTurn(ctx, tx, input.TurnID, input.ProviderTurnID)
		if err != nil {
			return err
		}
		var answerEventID *string
		if input.AnswerEvent != nil {
			if err := createAnswerEvent(ctx, tx, turn, input.AnswerEvent, input.AnswerText); err != nil {
				return err
			}
			answerEventID = &input.AnswerEvent.ID
		}
		if err := upsertSnapshotTx(ctx, tx, input.Snapshot); err != nil {
			return err
		}
		if len(input.BatchIDs) > 0 {
			batchResult := tx.WithContext(ctx).Model(&models.SyncBatch{}).
				Where("id IN ? AND agent_session_id = ? AND state IN ?", input.BatchIDs, turn.AgentSessionID, []string{"pending", "running"}).
				Updates(map[string]any{"state": "completed", "updated_at": input.UpdatedAt})
			if batchResult.Error != nil || batchResult.RowsAffected != int64(len(input.BatchIDs)) {
				return fmt.Errorf("提交同步批次失败：%w", batchResult.Error)
			}
		}
		turnResult := tx.WithContext(ctx).Model(&models.AgentTurn{}).
			Where("id = ? AND state = 'running' AND provider_turn_id = ?", input.TurnID, input.ProviderTurnID).
			Updates(map[string]any{"state": "completed", "answer_event_id": answerEventID, "ended_at": input.UpdatedAt, "updated_at": input.UpdatedAt})
		if turnResult.Error != nil || turnResult.RowsAffected != 1 {
			return ErrConflict
		}
		meetingResult := tx.WithContext(ctx).Model(&models.Meeting{}).
			Where("id = ? AND agent_state = 'busy'", turn.MeetingID).
			Updates(map[string]any{"agent_state": "available", "updated_at": input.UpdatedAt})
		if meetingResult.Error != nil || meetingResult.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// FailTurnInput 描述失败、取消或超时的最小稳定事实。
type FailTurnInput struct {
	TurnID     string
	State      string
	Event      models.MeetingEvent
	Reason     string
	ErrorCode  string
	AgentState string
	UpdatedAt  int64
}

// FailTurn 原子写入无 partial 的失败事件并以 compare-and-set 收敛 turn。
func (repository *Repository) FailTurn(ctx context.Context, input FailTurnInput) error {
	if repository == nil || repository.transactions == nil || input.TurnID == "" || input.Event.ID == "" {
		return fmt.Errorf("收敛智能体 turn：参数无效")
	}
	if input.State != "failed" && input.State != "cancelled" && input.State != "timed_out" {
		return fmt.Errorf("收敛智能体 turn：终态无效")
	}
	return repository.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var turn models.AgentTurn
		err := tx.WithContext(ctx).Select(turnColumns()).Where("id = ? AND state IN ?", input.TurnID, []string{"pending", "running"}).Take(&turn).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrConflict
		}
		if err != nil {
			return fmt.Errorf("读取待收敛 turn 失败：%w", err)
		}
		sequence, err := nextEventSeq(ctx, tx, turn.MeetingID)
		if err != nil {
			return err
		}
		kind := "ai.failed"
		if input.State == "cancelled" || input.State == "timed_out" {
			kind = "ai.cancelled"
		}
		payload, _ := json.Marshal(map[string]any{"v": 1, "reason": input.Reason})
		entityType, payloadJSON := "agent_turn", string(payload)
		input.Event.MeetingID, input.Event.Seq, input.Event.Kind = turn.MeetingID, sequence, kind
		input.Event.Source, input.Event.EntityType, input.Event.EntityID = "agent", &entityType, &turn.ID
		input.Event.PayloadJSON = &payloadJSON
		if err := tx.WithContext(ctx).Select(eventColumns()).Create(&input.Event).Error; err != nil {
			return fmt.Errorf("创建智能体失败事件失败：%w", err)
		}
		turnResult := tx.WithContext(ctx).Model(&models.AgentTurn{}).Where("id = ? AND state IN ?", turn.ID, []string{"pending", "running"}).
			Updates(map[string]any{"state": input.State, "ended_at": input.UpdatedAt, "last_error_code": input.ErrorCode, "updated_at": input.UpdatedAt})
		if turnResult.Error != nil || turnResult.RowsAffected != 1 {
			return ErrConflict
		}
		agentState := input.AgentState
		if agentState == "" {
			agentState = "available"
		}
		meetingResult := tx.WithContext(ctx).Model(&models.Meeting{}).Where("id = ? AND agent_state = 'busy'", turn.MeetingID).
			Updates(map[string]any{"agent_state": agentState, "updated_at": input.UpdatedAt})
		if meetingResult.Error != nil || meetingResult.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

// findQuestionByIdempotency 返回已关联的原问题事件。
func findQuestionByIdempotency(ctx context.Context, tx *gorm.DB, sessionID string, key string) (*models.AgentTurn, *models.MeetingEvent, error) {
	var turn models.AgentTurn
	err := tx.WithContext(ctx).Select(turnColumns()).Where("agent_session_id = ? AND idempotency_key = ?", sessionID, key).Take(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("读取幂等问题失败：%w", err)
	}
	if turn.QuestionEventID == nil {
		return nil, nil, ErrConflict
	}
	var event models.MeetingEvent
	if err := tx.WithContext(ctx).Select(eventColumns()).Where("id = ?", *turn.QuestionEventID).Take(&event).Error; err != nil {
		return nil, nil, fmt.Errorf("读取幂等问题事件失败：%w", err)
	}
	return &turn, &event, nil
}

// nextEventSeq 在单 writer 事务中分配会议下一序号。
func nextEventSeq(ctx context.Context, tx *gorm.DB, meetingID string) (int64, error) {
	var sequence int64
	if err := tx.WithContext(ctx).Raw("SELECT COALESCE(MAX(seq), 0) + 1 FROM meeting_events WHERE meeting_id = ?", meetingID).Scan(&sequence).Error; err != nil {
		return 0, fmt.Errorf("分配智能体事件序号失败：%w", err)
	}
	return sequence, nil
}

// getRunningTurn 校验成功提交仍属于同一 provider turn。
func getRunningTurn(ctx context.Context, tx *gorm.DB, turnID string, providerTurnID string) (models.AgentTurn, error) {
	var turn models.AgentTurn
	err := tx.WithContext(ctx).Select(turnColumns()).
		Where("id = ? AND state = 'running' AND provider_turn_id = ?", turnID, providerTurnID).Take(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.AgentTurn{}, ErrConflict
	}
	if err != nil {
		return models.AgentTurn{}, fmt.Errorf("读取 running turn 失败：%w", err)
	}
	return turn, nil
}

// createAnswerEvent 分配新 seq 并写入固定公开 payload。
func createAnswerEvent(ctx context.Context, tx *gorm.DB, turn models.AgentTurn, event *models.MeetingEvent, answer string) error {
	sequence, err := nextEventSeq(ctx, tx, turn.MeetingID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"v": 2, "text": answer, "content_format": "markdown", "guest_visible": true,
	})
	if err != nil {
		return fmt.Errorf("编码回答事件失败：%w", err)
	}
	entityType, payloadJSON := "agent_turn", string(payload)
	event.MeetingID, event.Seq, event.Kind, event.Source = turn.MeetingID, sequence, "ai.answer", "agent"
	event.EntityType, event.EntityID, event.PayloadJSON = &entityType, &turn.ID, &payloadJSON
	if err := tx.WithContext(ctx).Select(eventColumns()).Create(event).Error; err != nil {
		return fmt.Errorf("创建回答事件失败：%w", err)
	}
	return nil
}

// upsertSnapshotTx 在成功事务内部覆盖 session 唯一快照。
func upsertSnapshotTx(ctx context.Context, tx *gorm.DB, snapshot models.ContextSnapshot) error {
	err := tx.WithContext(ctx).Select(snapshotColumns()).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "agent_session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"agent_turn_id", "through_seq", "content_json", "content_sha256", "updated_at",
		}),
	}).Create(&snapshot).Error
	if err != nil {
		return fmt.Errorf("提交滚动快照失败：%w", err)
	}
	return nil
}

// eventColumns 返回 meeting_events 的完整显式列。
func eventColumns() []string {
	return []string{"id", "meeting_id", "seq", "kind", "occurred_at", "source", "entity_type", "entity_id", "payload_json", "created_at", "updated_at"}
}
