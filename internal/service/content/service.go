// Package content 提供主持人写入和桌面统一时间线投影。
package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	guestdomain "meet-sieve/internal/domain/guest"
	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	contentrepository "meet-sieve/internal/repository/content"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	defaultTimelineLimit = 100
	maxTimelineLimit     = 200
)

// Dependencies 是统一内容服务的显式依赖。
type Dependencies struct {
	Repository        *contentrepository.Repository
	Transactions      *database.TransactionManager
	Clock             clock.Clock
	IDs               identity.Generator
	OnPersisted       func(string)
	OnTimelineChanged func(string, int64, string)
}

// Service 负责主持人消息和桌面统一 Timeline，不直接依赖 Wails。
type Service struct {
	repository        *contentrepository.Repository
	transactions      *database.TransactionManager
	clock             clock.Clock
	ids               identity.Generator
	onPersisted       func(string)
	onTimelineChanged func(string, int64, string)
}

// TimelineQuery 描述桌面时间线的固定游标方向。
type TimelineQuery struct {
	MeetingID string
	Direction string
	CursorSeq int64
	Limit     int
}

// TimelinePage 是可恢复的持久事件页。
type TimelinePage struct {
	Entries      []TimelineEntry
	OldestSeq    int64
	LatestSeq    int64
	HasOlder     bool
	HasMoreAfter bool
}

// TimelineEntry 是桌面前端使用 kind 区分的统一事件投影。
type TimelineEntry struct {
	Seq             int64
	Kind            string
	OccurredAt      int64
	Source          string
	EntityID        string
	DisplayName     string
	Text            string
	ContentFormat   string
	SpeakerKey      string
	SpeakerLabel    string
	SpeakerRevision int
	StartSample     int64
	EndSample       int64
	State           string
	Reason          string
	ResourceKind    string
	OriginalName    string
	MediaType       string
	SizeBytes       int64
	SHA256          string
	URL             string
	Description     string
}

// SendMessageInput 是主持人发送 Markdown 会议消息的幂等输入。
type SendMessageInput struct {
	MeetingID string
	RequestID string
	Content   string
}

// SendResult 返回首次持久事件的真实身份。
type SendResult struct {
	EntityID   string
	Seq        int64
	OccurredAt int64
}

// NewService 创建统一内容服务；构造阶段不访问数据库。
func NewService(dependencies Dependencies) *Service {
	return &Service{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		clock: dependencies.Clock, ids: dependencies.IDs, onPersisted: dependencies.OnPersisted,
		onTimelineChanged: dependencies.OnTimelineChanged,
	}
}

// ListTimeline 返回一页统一持久事件，latest/before 的倒序查询会在返回前恢复升序。
func (service *Service) ListTimeline(ctx context.Context, query TimelineQuery) (TimelinePage, error) {
	if service == nil || service.repository == nil || query.MeetingID == "" {
		return TimelinePage{}, fmt.Errorf("统一时间线服务不可用")
	}
	limit := normalizeLimit(query.Limit)
	rows, err := service.repository.ListTimelineRows(ctx, query.MeetingID, query.Direction, query.CursorSeq, limit)
	if err != nil {
		return TimelinePage{}, err
	}
	hasExtra := len(rows) > limit
	if hasExtra {
		rows = rows[:limit]
	}
	if query.Direction == "latest" || query.Direction == "before" {
		slices.Reverse(rows)
	}
	page := TimelinePage{Entries: make([]TimelineEntry, 0, len(rows))}
	if len(rows) > 0 {
		page.OldestSeq = rows[0].Seq
		page.LatestSeq = rows[len(rows)-1].Seq
	}
	for _, row := range rows {
		if entry, visible := projectTimelineRow(row); visible {
			page.Entries = append(page.Entries, entry)
		}
	}
	page.HasOlder = (query.Direction == "latest" || query.Direction == "before") && hasExtra
	page.HasMoreAfter = query.Direction == "after" && hasExtra
	return page, nil
}

// SendHostMessage 在单 writer 短事务内提交主持人消息和统一事件。
func (service *Service) SendHostMessage(ctx context.Context, input SendMessageInput) (SendResult, error) {
	if service == nil || service.repository == nil || service.transactions == nil || service.clock == nil || service.ids == nil {
		return SendResult{}, fmt.Errorf("主持人消息服务不可用")
	}
	if err := guestdomain.ValidateRequestID(input.RequestID); err != nil {
		return SendResult{}, err
	}
	content, err := guestdomain.NormalizeMessage(input.Content)
	if err != nil {
		return SendResult{}, err
	}
	var result SendResult
	created := false
	err = service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if _, err := service.repository.GetWritableMeeting(ctx, tx, input.MeetingID); err != nil {
			return err
		}
		existing, err := service.repository.FindExistingHost(ctx, tx, input.MeetingID, input.RequestID)
		if err != nil {
			return err
		}
		if existing != nil {
			if existing.Kind != "text" || existing.Content != content {
				return apperr.Biz(apperr.CodeConflict, apperr.WithOp("content.host_message.idempotency"))
			}
			result = SendResult{EntityID: existing.EntityID, Seq: existing.Seq, OccurredAt: existing.OccurredAt}
			return nil
		}
		result, err = service.commitHostMessage(ctx, tx, input.MeetingID, input.RequestID, content)
		created = err == nil
		return err
	})
	if err != nil {
		if errors.Is(err, contentrepository.ErrMeetingNotWritable) {
			return SendResult{}, apperr.Biz(apperr.CodeLANMeetingEnded, apperr.WithOp("content.host_message.meeting"))
		}
		return SendResult{}, err
	}
	if created && service.onPersisted != nil {
		service.onPersisted(input.MeetingID)
	}
	if created && service.onTimelineChanged != nil {
		service.onTimelineChanged(input.MeetingID, result.Seq, "message_created")
	}
	return result, nil
}

// LatestFinalOccurredAt 返回系统状态区使用的最近 final 业务时间。
func (service *Service) LatestFinalOccurredAt(ctx context.Context, meetingID string) (int64, error) {
	if service == nil || service.repository == nil {
		return 0, fmt.Errorf("统一时间线服务不可用")
	}
	return service.repository.LatestFinalOccurredAt(ctx, meetingID)
}

// LatestEventSeq 返回旧事件源提交后的统一 seq，用于发布可恢复通知。
func (service *Service) LatestEventSeq(ctx context.Context, meetingID string) (int64, error) {
	if service == nil || service.repository == nil {
		return 0, fmt.Errorf("统一时间线服务不可用")
	}
	return service.repository.LatestEventSeq(ctx, meetingID)
}

// commitHostMessage 只负责新消息的 seq、事件头和消息实体写入。
func (service *Service) commitHostMessage(ctx context.Context, tx *gorm.DB, meetingID string, requestID string, content string) (SendResult, error) {
	seq, err := service.repository.NextEventSeq(ctx, tx, meetingID)
	if err != nil {
		return SendResult{}, err
	}
	eventID, messageID := service.ids.New(), service.ids.New()
	if eventID == "" || messageID == "" {
		return SendResult{}, fmt.Errorf("生成主持人消息 ID 失败")
	}
	now := service.clock.Now().UnixMilli()
	entityType := "message"
	event := models.MeetingEvent{
		ID: eventID, MeetingID: meetingID, Seq: seq, Kind: "message.created", OccurredAt: now,
		Source: "host", EntityType: &entityType, EntityID: &messageID, CreatedAt: now, UpdatedAt: now,
	}
	message := models.Message{
		ID: messageID, MeetingID: meetingID, EventID: eventID, AuthorKind: "host", RequestID: &requestID,
		DisplayNameSnapshot: "你", Content: content, ContentFormat: "markdown", CreatedAt: now, UpdatedAt: now,
	}
	if err := service.repository.CreateMessage(ctx, tx, event, message); err != nil {
		return SendResult{}, err
	}
	return SendResult{EntityID: messageID, Seq: seq, OccurredAt: now}, nil
}

// normalizeLimit 应用统一时间线的缺省页长和硬上限。
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultTimelineLimit
	}
	if limit > maxTimelineLimit {
		return maxTimelineLimit
	}
	return limit
}

type agentPayload struct {
	Version              int    `json:"v"`
	Text                 string `json:"text"`
	Reason               string `json:"reason"`
	ContentFormat        string `json:"content_format"`
	SpeakerKeySnapshot   string `json:"speaker_key_snapshot"`
	SpeakerLabelSnapshot string `json:"speaker_label_snapshot"`
}

// projectTimelineRow 把数据库内部行映射为前端白名单判别结构。
func projectTimelineRow(row contentrepository.TimelineRow) (TimelineEntry, bool) {
	base := TimelineEntry{Seq: row.Seq, OccurredAt: row.OccurredAt, Source: row.Source, EntityID: row.EntityID}
	switch row.EventKind {
	case "utterance.final":
		base.Kind, base.Text, base.ContentFormat = "utterance", row.UtteranceText, "plain"
		base.StartSample, base.EndSample = row.StartSample, row.EndSample
		base.SpeakerKey, base.SpeakerLabel = speakerIdentity(row)
		base.SpeakerRevision = row.SpeakerRevision
		return base, row.UtteranceID != ""
	case "asr.gap":
		base.Kind, base.Reason, base.ContentFormat = "gap", row.GapReason, "plain"
		return base, row.GapID != ""
	case "message.created":
		base.Kind, base.EntityID = "message", row.MessageID
		base.DisplayName, base.Text = row.MessageDisplayName, row.MessageContent
		base.ContentFormat = fallbackFormat(row.MessageContentFormat)
		return base, row.MessageID != ""
	case "resource.created":
		base.Kind, base.EntityID = "resource", row.ResourceID
		base.DisplayName = row.ResourceGuestDisplayName
		if base.DisplayName == "" {
			base.DisplayName = "你"
		}
		base.ResourceKind, base.State = row.ResourceKind, row.ResourceState
		base.OriginalName, base.MediaType, base.SizeBytes = row.ResourceName, row.ResourceMediaType, row.ResourceSize
		base.SHA256, base.URL, base.Description = row.ResourceSHA256, row.ResourceURL, row.ResourceDescription
		return base, row.ResourceID != ""
	case "ai.question", "ai.answer", "ai.cancelled", "ai.failed":
		var payload agentPayload
		if json.Unmarshal([]byte(row.PayloadJSON), &payload) != nil {
			return TimelineEntry{}, false
		}
		base.Kind, base.Text, base.Reason = "ai_"+row.EventKind[3:], payload.Text, payload.Reason
		if row.EventKind == "ai.question" {
			base.SpeakerKey, base.DisplayName = questionSpeakerIdentity(row, payload)
		} else {
			base.DisplayName = "AI 助手"
		}
		base.ContentFormat = "plain"
		if payload.Version >= 2 && payload.ContentFormat == "markdown" {
			base.ContentFormat = "markdown"
		}
		return base, true
	default:
		return TimelineEntry{}, false
	}
}

// questionSpeakerIdentity 优先使用触发 utterance 的当前投影，使人工校对可回写既有 AI 问题。
func questionSpeakerIdentity(row contentrepository.TimelineRow, payload agentPayload) (string, string) {
	if row.AgentTriggerID != "" {
		return speakerIdentityParts(
			row.AgentParticipantID, row.AgentParticipantName, row.AgentClusterID,
			row.AgentClusterDisplayNo, row.AgentTrackID, row.AgentTrackDisplayNo,
		)
	}
	if payload.SpeakerLabelSnapshot != "" {
		return payload.SpeakerKeySnapshot, payload.SpeakerLabelSnapshot
	}
	return "host", "你"
}

// speakerIdentity 选择可跨刷新稳定的说话人 key 和展示名。
func speakerIdentity(row contentrepository.TimelineRow) (string, string) {
	key, label := speakerIdentityParts(row.ParticipantID, row.ParticipantName, row.SpeakerClusterID, row.ClusterDisplayNo, row.SpeakerTrackID, row.TrackDisplayNo)
	if key != "" {
		return key, label
	}
	// 无标签且尚未建立本地 track 时按 utterance 隔离，避免被错误合并为同一人。
	return "unlabeled:" + row.UtteranceID, label
}

// speakerIdentityParts 把当前 participant、cluster 与 track 投影统一映射为稳定 key 和展示名称。
func speakerIdentityParts(participantID string, participantName string, clusterID string, clusterDisplayNo int, trackID string, trackDisplayNo int) (string, string) {
	if participantID != "" {
		return "participant:" + participantID, speakerdomain.DisplayName(participantName, clusterDisplayNo, trackDisplayNo)
	}
	if clusterID != "" {
		return "cluster:" + clusterID, speakerdomain.DisplayName("", clusterDisplayNo, trackDisplayNo)
	}
	if trackID != "" {
		return "track:" + trackID, speakerdomain.DisplayName("", 0, trackDisplayNo)
	}
	return "", speakerdomain.DisplayName("", 0, 0)
}

// fallbackFormat 只接受已登记格式，异常旧数据安全退回纯文本。
func fallbackFormat(value string) string {
	if value == "markdown" {
		return value
	}
	return "plain"
}
