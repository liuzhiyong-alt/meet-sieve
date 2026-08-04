package guest

import (
	"context"
	"fmt"

	contentrepository "meet-sieve/internal/repository/content"
)

const (
	defaultTimelineLimit = 100
	maxTimelineLimit     = 200
)

// TimelineService 把统一事件流投影为 Guest 可见白名单。
type TimelineService struct {
	repository *contentrepository.Repository
}

// TimelinePage 使不可见事件也能通过 next_seq 推进游标。
type TimelinePage struct {
	Events  []TimelineEvent `json:"events"`
	NextSeq int64           `json:"next_seq"`
	HasMore bool            `json:"has_more"`
}

// TimelineEvent 是不包含 payload_json 的 Guest 安全判别投影。
type TimelineEvent struct {
	Seq           int64  `json:"seq"`
	Kind          string `json:"kind"`
	OccurredAt    int64  `json:"occurred_at"`
	EntityID      string `json:"entity_id"`
	DisplayName   string `json:"display_name"`
	Text          string `json:"text,omitempty"`
	URL           string `json:"url,omitempty"`
	OriginalName  string `json:"original_name,omitempty"`
	MediaType     string `json:"media_type,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	Description   string `json:"description,omitempty"`
	ContentFormat string `json:"content_format,omitempty"`
}

// NewTimelineService 创建 Guest 事件白名单查询服务。
func NewTimelineService(repository *contentrepository.Repository) *TimelineService {
	return &TimelineService{repository: repository}
}

// List 最多扫描 limit 条统一事件，跳过不可见项但仍推进 next_seq。
func (service *TimelineService) List(ctx context.Context, meetingID string, afterSeq int64, limit int) (TimelinePage, error) {
	if service == nil || service.repository == nil || meetingID == "" || afterSeq < 0 {
		return TimelinePage{}, fmt.Errorf("读取 Guest Timeline：参数无效")
	}
	limit = NormalizeTimelineLimit(limit)
	rows, err := service.repository.ListGuestTimelineRows(ctx, meetingID, afterSeq, limit+1)
	if err != nil {
		return TimelinePage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	page := TimelinePage{Events: make([]TimelineEvent, 0, len(rows)), NextSeq: afterSeq, HasMore: hasMore}
	for _, row := range rows {
		page.NextSeq = row.Seq
		if event, visible := projectTimelineRow(row); visible {
			page.Events = append(page.Events, event)
		}
	}
	return page, nil
}

// NormalizeTimelineLimit 应用 Guest Timeline 的缺省值和硬上限。
func NormalizeTimelineLimit(limit int) int {
	if limit <= 0 {
		return defaultTimelineLimit
	}
	if limit > maxTimelineLimit {
		return maxTimelineLimit
	}
	return limit
}

// projectTimelineRow 按明确 kind/state 白名单构建 DTO，其他事件一律不公开。
func projectTimelineRow(row contentrepository.GuestTimelineRow) (TimelineEvent, bool) {
	switch {
	case row.EventKind == "message.created" && row.MessageID != "":
		return TimelineEvent{
			Seq: row.Seq, Kind: "message", OccurredAt: row.OccurredAt,
			EntityID: row.MessageID, DisplayName: row.MessageDisplayName, Text: row.MessageContent,
			ContentFormat: row.MessageContentFormat,
		}, true
	case row.EventKind == "resource.created" && row.ResourceKind == "link" && row.ResourceState == "completed" && row.ResourceID != "":
		return TimelineEvent{
			Seq: row.Seq, Kind: "link", OccurredAt: row.OccurredAt,
			EntityID: row.ResourceID, DisplayName: row.ResourceDisplayName, URL: row.ResourceURL,
		}, true
	case row.EventKind == "resource.created" && row.ResourceKind == "attachment" && row.ResourceState == "completed" && row.ResourceID != "":
		return TimelineEvent{
			Seq: row.Seq, Kind: "attachment", OccurredAt: row.OccurredAt,
			EntityID: row.ResourceID, DisplayName: row.ResourceDisplayName,
			OriginalName: row.ResourceName, MediaType: row.ResourceMediaType, SizeBytes: row.ResourceSize,
			SHA256: row.ResourceSHA256, Description: row.ResourceDescription,
		}, true
	case row.EventKind == "ai.answer" && row.AgentAnswerVisible:
		return TimelineEvent{
			Seq: row.Seq, Kind: "ai_answer", OccurredAt: row.OccurredAt,
			DisplayName: "AI", Text: row.AgentAnswerText, ContentFormat: row.AgentAnswerFormat,
		}, true
	default:
		return TimelineEvent{}, false
	}
}
