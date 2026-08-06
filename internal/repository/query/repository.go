// Package query 提供 Step 9 首页、会议记录和详情使用的只读 SQLite 查询。
package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	querydomain "meet-sieve/internal/domain/query"

	"gorm.io/gorm"
)

const maxMeetingPageSize = 50

// ListInput 表示 Repository 已校验后的会议列表查询。
type ListInput struct {
	Filter querydomain.MeetingFilter
	Cursor *querydomain.Cursor
	Limit  int
}

// MeetingSummaryRow 是首页和记录页共用的安全会议摘要。
type MeetingSummaryRow struct {
	ID                   string
	MeetingNo            string
	Subject              string
	StartedAt            int64
	EndedAt              *int64
	LifecycleState       string
	LocalSaveState       string
	RealtimeASRState     string
	GapState             string
	AgentState           string
	MinuteState          string
	LANState             string
	Participants         []string `gorm:"-"`
	ParticipantMemberIDs []string `gorm:"-"`
	HighestStatus        querydomain.MeetingStatus
	PendingGapID         string
	HasReadyAudio        bool
	RecordingDeleted     bool
}

// RecoveryFactsRow 是中断恢复页需要的本地文件、样本与缺口事实。
type RecoveryFactsRow struct {
	SegmentCount     int
	DurationSamples  int64
	SampleRate       int
	FirstSequence    int
	LastSequence     int
	GapCount         int
	PendingGapCount  int
	ReadyFileCount   int
	FailedFileCount  int
	DeletedFileCount int
	FailureStage     string
}

// MeetingPage 是一次不含总数的 keyset 查询结果。
type MeetingPage struct {
	Items   []MeetingSummaryRow
	HasMore bool
}

// Repository 只持有 reader pool，不拥有写事务或业务修复逻辑。
type Repository struct {
	reader *gorm.DB
}

// NewRepository 创建 Step 9 查询 Repository。
func NewRepository(reader *gorm.DB) *Repository {
	return &Repository{reader: reader}
}

// ListMeetings 使用唯一复合边界读取一页历史会议，不执行总数统计。
func (repository *Repository) ListMeetings(ctx context.Context, input ListInput) (MeetingPage, error) {
	if repository == nil || repository.reader == nil {
		return MeetingPage{}, fmt.Errorf("读取会议记录：Repository 不可用")
	}
	filter, err := querydomain.NormalizeFilter(input.Filter)
	if err != nil {
		return MeetingPage{}, err
	}
	limit := normalizeLimit(input.Limit)
	query := buildMeetingListQuery(repository.reader.WithContext(ctx), filter, input.Cursor)
	query, err = applyStatusFilter(query, filter.Status)
	if err != nil {
		return MeetingPage{}, err
	}

	rows, err := scanMeetingRows(query, limit+1)
	if err != nil {
		return MeetingPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	if input.Cursor != nil && input.Cursor.Direction == querydomain.DirectionPrevious {
		slices.Reverse(rows)
	}
	if err := repository.loadPageFacts(ctx, rows); err != nil {
		return MeetingPage{}, err
	}
	return MeetingPage{Items: rows, HasMore: hasMore}, nil
}

// FindHighestPriorityMeeting 按状态优先级有界查询一场最新的可续办会议。
func (repository *Repository) FindHighestPriorityMeeting(ctx context.Context) (*MeetingSummaryRow, error) {
	for _, status := range querydomain.ContinuationStatusesByPriority() {
		page, err := repository.ListMeetings(ctx, ListInput{
			Filter: querydomain.MeetingFilter{Status: string(status)},
			Limit:  1,
		})
		if err != nil {
			return nil, fmt.Errorf("读取最高优先级会议失败：%w", err)
		}
		if len(page.Items) == 0 {
			continue
		}
		// 更高状态已经先查询完毕；当前候选必须以统一 projector 的结果为准。
		if page.Items[0].HighestStatus == status {
			return &page.Items[0], nil
		}
	}
	return nil, nil
}

// buildMeetingListQuery 构造固定历史范围、搜索与 keyset 边界。
func buildMeetingListQuery(db *gorm.DB, filter querydomain.MeetingFilter, cursor *querydomain.Cursor) *gorm.DB {
	query := db.Table("meetings AS meeting").Select(
		"meeting.id", "meeting.meeting_no", "meeting.subject", "meeting.started_at", "meeting.ended_at",
		"meeting.lifecycle_state", "meeting.local_save_state", "meeting.realtime_asr_state", "meeting.gap_state",
		"meeting.agent_state", "meeting.minute_state", "meeting.lan_state",
		"EXISTS (SELECT 1 FROM audio_assets aa WHERE aa.meeting_id = meeting.id AND aa.state = 'ready') AS has_ready_audio",
		"EXISTS (SELECT 1 FROM deletion_jobs dj WHERE dj.meeting_id = meeting.id AND dj.kind = 'recording' AND dj.state = 'completed') AS recording_deleted",
	).Where("meeting.lifecycle_state NOT IN ? AND meeting.started_at IS NOT NULL", []string{"preparing", "recording", "finalizing"})
	if filter.Search != "" {
		pattern := "%" + escapeLike(filter.Search) + "%"
		query = query.Where(`(
			meeting.subject LIKE ? ESCAPE '\' OR meeting.meeting_no LIKE ? ESCAPE '\' OR EXISTS (
				SELECT 1 FROM meeting_participants AS participant
				WHERE participant.meeting_id = meeting.id
				  AND participant.display_name_snapshot LIKE ? ESCAPE '\'
			)
		)`, pattern, pattern, pattern)
	}
	if cursor == nil {
		return query.Order("meeting.started_at DESC").Order("meeting.meeting_no DESC")
	}
	if cursor.Direction == querydomain.DirectionPrevious {
		return query.Where("(meeting.started_at > ? OR (meeting.started_at = ? AND meeting.meeting_no > ?))", cursor.StartedAt, cursor.StartedAt, cursor.MeetingNo).
			Order("meeting.started_at ASC").Order("meeting.meeting_no ASC")
	}
	return query.Where("(meeting.started_at < ? OR (meeting.started_at = ? AND meeting.meeting_no < ?))", cursor.StartedAt, cursor.StartedAt, cursor.MeetingNo).
		Order("meeting.started_at DESC").Order("meeting.meeting_no DESC")
}

// applyStatusFilter 把已确认展示筛选映射回独立事实轴，禁止拼接任意 SQL。
func applyStatusFilter(query *gorm.DB, status string) (*gorm.DB, error) {
	switch status {
	case "":
		return query, nil
	case string(querydomain.StatusDeleting):
		return query.Where("meeting.lifecycle_state IN ? OR EXISTS (SELECT 1 FROM deletion_jobs d WHERE d.meeting_id = meeting.id AND d.state IN ('pending','running','failed'))", []string{"deleting", "delete_failed"}), nil
	case string(querydomain.StatusRecoveryRequired):
		return query.Where("meeting.local_save_state = 'failed' OR (meeting.lifecycle_state = 'interrupted' AND meeting.local_save_state <> 'saved')"), nil
	case string(querydomain.StatusGapConflict):
		return query.Where("meeting.gap_state = 'conflict'"), nil
	case string(querydomain.StatusGapPending):
		return query.Where("meeting.gap_state IN ?", []string{"failed", "pending", "processing"}), nil
	case string(querydomain.StatusMinuteCandidate):
		return query.Where("meeting.minute_state = 'draft'"), nil
	case string(querydomain.StatusAgentUnsynced):
		return query.Where("meeting.agent_state = 'unsynced'"), nil
	case string(querydomain.StatusMinuteConfirmed):
		return query.Where("meeting.minute_state = 'confirmed'"), nil
	case string(querydomain.StatusSaved):
		return query.Where("meeting.local_save_state = 'saved'"), nil
	default:
		return nil, fmt.Errorf("未知会议状态筛选：%s", status)
	}
}

// scanMeetingRows 执行显式列查询，避免数据库模型扩展时把内部字段带入 read model。
func scanMeetingRows(query *gorm.DB, limit int) ([]MeetingSummaryRow, error) {
	var rows []MeetingSummaryRow
	if err := query.Limit(limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取会议记录失败：%w", err)
	}
	return rows, nil
}

// loadPageFacts 用固定批量查询补充参与人、删除状态和待处理缺口，禁止按会议 N+1。
func (repository *Repository) loadPageFacts(ctx context.Context, rows []MeetingSummaryRow) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	indexes := make(map[string]int, len(rows))
	for index := range rows {
		ids = append(ids, rows[index].ID)
		indexes[rows[index].ID] = index
	}
	if err := repository.loadParticipants(ctx, ids, rows, indexes); err != nil {
		return err
	}
	deleting, err := repository.loadDeletingMeetings(ctx, ids)
	if err != nil {
		return err
	}
	gaps, err := repository.loadPendingGaps(ctx, ids)
	if err != nil {
		return err
	}
	for index := range rows {
		_, hasDeletion := deleting[rows[index].ID]
		rows[index].HighestStatus = projectHighestStatus(rows[index], hasDeletion)
		rows[index].PendingGapID = gaps[rows[index].ID]
	}
	return nil
}

// loadParticipants 批量读取本页不可变参会者快照。
func (repository *Repository) loadParticipants(ctx context.Context, ids []string, rows []MeetingSummaryRow, indexes map[string]int) error {
	var participants []struct {
		MeetingID string  `gorm:"column:meeting_id"`
		Name      string  `gorm:"column:display_name_snapshot"`
		MemberID  *string `gorm:"column:member_id"`
	}
	if err := repository.reader.WithContext(ctx).Table("meeting_participants").
		Select("meeting_id", "display_name_snapshot", "member_id").Where("meeting_id IN ?", ids).
		Order("meeting_id ASC").Order("sort_order ASC").Scan(&participants).Error; err != nil {
		return fmt.Errorf("读取会议参会者摘要失败：%w", err)
	}
	for _, participant := range participants {
		if index, exists := indexes[participant.MeetingID]; exists {
			rows[index].Participants = append(rows[index].Participants, participant.Name)
			if participant.MemberID != nil {
				rows[index].ParticipantMemberIDs = append(rows[index].ParticipantMemberIDs, *participant.MemberID)
			}
		}
	}
	return nil
}

// GetRecoveryFacts 汇总 interrupted 恢复页的真实资产、样本、缺口和失败阶段。
func (repository *Repository) GetRecoveryFacts(ctx context.Context, meetingID string) (RecoveryFactsRow, error) {
	if repository == nil || repository.reader == nil {
		return RecoveryFactsRow{}, fmt.Errorf("读取恢复事实：Repository 不可用")
	}
	var facts RecoveryFactsRow
	err := repository.reader.WithContext(ctx).Raw(`
		SELECT
			COALESCE(SUM(CASE WHEN kind = 'microphone' AND state <> 'deleted' THEN 1 ELSE 0 END), 0) AS segment_count,
			COALESCE(MAX(CASE WHEN kind = 'microphone' AND state <> 'deleted' THEN end_sample ELSE 0 END), 0) AS duration_samples,
			COALESCE(MAX(CASE WHEN kind = 'microphone' AND state <> 'deleted' THEN sample_rate ELSE 0 END), 0) AS sample_rate,
			COALESCE(MIN(CASE WHEN kind = 'microphone' AND state <> 'deleted' THEN sequence_no END), 0) AS first_sequence,
			COALESCE(MAX(CASE WHEN kind = 'microphone' AND state <> 'deleted' THEN sequence_no ELSE 0 END), 0) AS last_sequence,
			COALESCE(SUM(CASE WHEN state = 'ready' THEN 1 ELSE 0 END), 0) AS ready_file_count,
			COALESCE(SUM(CASE WHEN state = 'failed' THEN 1 ELSE 0 END), 0) AS failed_file_count,
			COALESCE(SUM(CASE WHEN state = 'deleted' THEN 1 ELSE 0 END), 0) AS deleted_file_count
		FROM audio_assets WHERE meeting_id = ?`, meetingID).Scan(&facts).Error
	if err != nil {
		return RecoveryFactsRow{}, fmt.Errorf("读取恢复音频事实失败：%w", err)
	}
	var gap struct {
		Count   int `gorm:"column:gap_count"`
		Pending int `gorm:"column:pending_gap_count"`
	}
	if err := repository.reader.WithContext(ctx).Raw(`
		SELECT COUNT(*) AS gap_count,
		COALESCE(SUM(CASE WHEN state IN ('pending','processing','failed','conflict') THEN 1 ELSE 0 END), 0) AS pending_gap_count
		FROM asr_gaps WHERE meeting_id = ?`, meetingID).Scan(&gap).Error; err != nil {
		return RecoveryFactsRow{}, fmt.Errorf("读取恢复缺口事实失败：%w", err)
	}
	facts.GapCount, facts.PendingGapCount = gap.Count, gap.Pending
	var stage *string
	if err := repository.reader.WithContext(ctx).Table("asr_sessions").Select("last_error_code").
		Where("meeting_id = ? AND last_error_code IS NOT NULL", meetingID).Order("updated_at DESC").Limit(1).Scan(&stage).Error; err != nil {
		return RecoveryFactsRow{}, fmt.Errorf("读取恢复失败阶段失败：%w", err)
	}
	if stage != nil {
		facts.FailureStage = *stage
	}
	return facts, nil
}

// loadDeletingMeetings 批量读取本页活动或失败删除任务。
func (repository *Repository) loadDeletingMeetings(ctx context.Context, ids []string) (map[string]struct{}, error) {
	var rows []struct {
		MeetingID string `gorm:"column:meeting_id"`
	}
	if err := repository.reader.WithContext(ctx).Table("deletion_jobs").Distinct("meeting_id").
		Where("meeting_id IN ? AND state IN ?", ids, []string{"pending", "running", "failed"}).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取删除状态摘要失败：%w", err)
	}
	result := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		result[row.MeetingID] = struct{}{}
	}
	return result, nil
}

// loadPendingGaps 为每场会议批量选择一个优先处理的未解决缺口。
func (repository *Repository) loadPendingGaps(ctx context.Context, ids []string) (map[string]string, error) {
	var rows []struct {
		ID        string `gorm:"column:id"`
		MeetingID string `gorm:"column:meeting_id"`
	}
	if err := repository.reader.WithContext(ctx).Table("asr_gaps").Select("id", "meeting_id").
		Where("meeting_id IN ? AND state IN ?", ids, []string{"conflict", "pending", "processing", "failed"}).
		Order("CASE WHEN state = 'conflict' THEN 0 ELSE 1 END ASC").Order("updated_at DESC").Order("id ASC").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取待处理缺口摘要失败：%w", err)
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		if _, exists := result[row.MeetingID]; !exists {
			result[row.MeetingID] = row.ID
		}
	}
	return result, nil
}

// projectHighestStatus 从会议正交状态与删除事实生成唯一列表 badge。
func projectHighestStatus(row MeetingSummaryRow, hasDeletion bool) querydomain.MeetingStatus {
	return querydomain.HighestPriorityStatus(querydomain.MeetingStatusFacts{
		Deleting:        hasDeletion || row.LifecycleState == "deleting" || row.LifecycleState == "delete_failed",
		LocalSaveFailed: row.LocalSaveState == "failed" || (row.LifecycleState == "interrupted" && row.LocalSaveState != "saved"),
		GapConflict:     row.GapState == "conflict", GapProcessing: row.GapState == "failed" || row.GapState == "pending" || row.GapState == "processing",
		MinuteCandidate: row.MinuteState == "draft", AgentUnsynced: row.AgentState == "unsynced",
		MinuteConfirmed: row.MinuteState == "confirmed", LocalSaved: row.LocalSaveState == "saved",
	})
}

// normalizeLimit 固定会议页大小上限，避免调用方一次读取全部记录。
func normalizeLimit(limit int) int {
	if limit <= 0 || limit > maxMeetingPageSize {
		return maxMeetingPageSize
	}
	return limit
}

// escapeLike 把 SQLite LIKE 元字符转义为普通搜索文字。
func escapeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}
