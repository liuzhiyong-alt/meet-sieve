package transcript

import (
	"context"
	"fmt"

	transcriptrepository "meet-sieve/internal/repository/transcript"
)

// TimelineEntry 是前端可恢复的持久 final/gap 判别联合。
type TimelineEntry struct {
	// Seq 是会议内严格递增的持久事件序号。
	Seq int64
	// Kind 只允许 utterance.final 或 asr.gap。
	Kind string
	// OccurredAt 是事件发生时间的 Unix 毫秒值。
	OccurredAt int64
	// StartSample 是全局半开区间起点。
	StartSample int64
	// EndSample 是全局半开区间终点。
	EndSample int64
	// Text 仅 final 使用。
	Text string
	// SpeakerLabel 仅 final 使用，可能为空。
	SpeakerLabel string
	// SessionOrder 是匿名 ASR session 顺序，不暴露 UUID。
	SessionOrder int
	// GapReason 仅 gap 使用。
	GapReason string
}

// TimelineService 从 SQLite 快照和 seq 游标恢复持久事件。
type TimelineService struct {
	repository *transcriptrepository.Repository
}

// NewTimelineService 创建 Timeline 查询服务。
func NewTimelineService(repository *transcriptrepository.Repository) *TimelineService {
	return &TimelineService{repository: repository}
}

// List 返回 afterSeq 之后最多 200 条 final/gap。
func (service *TimelineService) List(ctx context.Context, meetingID string, afterSeq int64, limit int) ([]TimelineEntry, error) {
	if service == nil || service.repository == nil {
		return nil, fmt.Errorf("Timeline 服务未初始化")
	}
	rows, err := service.repository.ListTimelineRows(ctx, meetingID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	sessions, err := service.repository.LoadSessions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	orders := make(map[string]int, len(sessions))
	for index, session := range sessions {
		orders[session.ID] = index + 1
	}
	entries := make([]TimelineEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, TimelineEntry{Seq: row.Seq, Kind: row.Kind, OccurredAt: row.OccurredAt, StartSample: row.StartSample, EndSample: row.EndSample, Text: row.Text, SpeakerLabel: row.SpeakerLabel, SessionOrder: orders[row.ASRSessionID], GapReason: row.GapReason})
	}
	return entries, nil
}
