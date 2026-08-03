// Package content 在调用方短事务中持久化 Guest 内容和统一会议事件。
package content

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

var (
	// ErrSessionInactive 表示 session 不存在、已撤销或不属于该会议。
	ErrSessionInactive = errors.New("访客会话不可用")
	// ErrMeetingNotWritable 表示会议已不允许 LAN 写入。
	ErrMeetingNotWritable = errors.New("会议不允许访客写入")
)

// ExistingContent 是幂等检查所需的已提交内容投影。
type ExistingContent struct {
	Kind         string
	Content      string
	EntityID     string
	Seq          int64
	OccurredAt   int64
	OriginalName string
	SizeBytes    int64
	SHA256       string
	MediaType    string
}

// GuestTimelineRow 是事件表与 message/resource 白名单 join 的安全读取行。
type GuestTimelineRow struct {
	Seq                 int64
	EventKind           string
	OccurredAt          int64
	MessageID           string
	MessageContent      string
	MessageDisplayName  string
	ResourceID          string
	ResourceKind        string
	ResourceState       string
	ResourceURL         string
	ResourceName        string
	ResourceMediaType   string
	ResourceSize        int64
	ResourceSHA256      string
	ResourceDescription string
	ResourceDisplayName string
	AgentAnswerText     string
	AgentAnswerVisible  bool
}

// GetCompletedAttachment 只返回指定会议内已完成的附件，避免跨会议枚举。
func (repository *Repository) GetCompletedAttachment(ctx context.Context, meetingID string, resourceID string) (*models.Resource, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || resourceID == "" {
		return nil, fmt.Errorf("读取附件：参数无效")
	}
	var resource models.Resource
	result := repository.reader.WithContext(ctx).Select(resourceColumns()).
		Where("id = ? AND meeting_id = ? AND kind = 'attachment' AND state = 'completed'", resourceID, meetingID).
		Limit(1).Find(&resource)
	if result.Error != nil {
		return nil, fmt.Errorf("读取附件失败：%w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &resource, nil
}

// ListReferencedSafeNames 返回会议已入库 completed 附件的内部文件名集合。
func (repository *Repository) ListReferencedSafeNames(ctx context.Context, meetingID string) (map[string]struct{}, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取已入库附件：参数无效")
	}
	var rows []struct {
		SafeName string `gorm:"column:safe_name"`
	}
	if err := repository.reader.WithContext(ctx).Model(&models.Resource{}).Select("safe_name").
		Where("meeting_id = ? AND kind = 'attachment' AND state = 'completed' AND safe_name IS NOT NULL", meetingID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取已入库附件失败：%w", err)
	}
	result := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.SafeName != "" {
			result[row.SafeName] = struct{}{}
		}
	}
	return result, nil
}

// Repository 负责显式 SQL 读写，不拥有事务边界。
type Repository struct {
	reader *gorm.DB
}

// NewRepository 创建内容 Repository，reader 只用于后续无事务投影查询。
func NewRepository(reader *gorm.DB) *Repository {
	return &Repository{reader: reader}
}

// GetWritableSession 在同一事务内重新校验 session 与会议写入状态。
func (repository *Repository) GetWritableSession(ctx context.Context, tx *gorm.DB, meetingID string, sessionID string) (models.GuestSession, error) {
	if tx == nil || meetingID == "" || sessionID == "" {
		return models.GuestSession{}, ErrSessionInactive
	}
	var row struct {
		models.GuestSession
		LifecycleState string `gorm:"column:lifecycle_state"`
	}
	err := tx.WithContext(ctx).Table("guest_sessions AS session").
		Select("session.id", "session.meeting_id", "session.display_name", "session.session_token_hash", "session.state",
			"session.expires_at", "session.last_seen_at", "session.created_at", "session.updated_at", "meeting.lifecycle_state").
		Joins("JOIN meetings AS meeting ON meeting.id = session.meeting_id").
		Where("session.id = ? AND session.meeting_id = ? AND session.state = 'active'", sessionID, meetingID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.GuestSession{}, ErrSessionInactive
	}
	if err != nil {
		return models.GuestSession{}, fmt.Errorf("读取可写访客会话失败：%w", err)
	}
	if row.LifecycleState != "recording" {
		return models.GuestSession{}, ErrMeetingNotWritable
	}
	return row.GuestSession, nil
}

// FindExisting 在 message/resource 两张表中查找 session 范围的 request ID。
func (repository *Repository) FindExisting(ctx context.Context, tx *gorm.DB, sessionID string, requestID string) (*ExistingContent, error) {
	if tx == nil || sessionID == "" || requestID == "" {
		return nil, fmt.Errorf("查询内容幂等记录：参数无效")
	}
	message, err := findExistingMessage(ctx, tx, sessionID, requestID)
	if err != nil || message != nil {
		return message, err
	}
	return findExistingResource(ctx, tx, sessionID, requestID)
}

// NextEventSeq 在单 writer 事务中分配下一个会议序号。
func (repository *Repository) NextEventSeq(ctx context.Context, tx *gorm.DB, meetingID string) (int64, error) {
	if tx == nil || meetingID == "" {
		return 0, fmt.Errorf("分配内容事件序号：参数无效")
	}
	var next int64
	if err := tx.WithContext(ctx).Raw("SELECT COALESCE(MAX(seq), 0) + 1 FROM meeting_events WHERE meeting_id = ?", meetingID).Scan(&next).Error; err != nil {
		return 0, fmt.Errorf("分配内容事件序号失败：%w", err)
	}
	return next, nil
}

// CreateMessage 在当前事务中插入 event header 和文字消息。
func (repository *Repository) CreateMessage(ctx context.Context, tx *gorm.DB, event models.MeetingEvent, message models.Message) error {
	if tx == nil {
		return fmt.Errorf("创建访客消息：事务不可用")
	}
	if err := tx.WithContext(ctx).Select(eventColumns()).Create(&event).Error; err != nil {
		return fmt.Errorf("写入消息事件失败：%w", err)
	}
	if err := tx.WithContext(ctx).Select(messageColumns()).Create(&message).Error; err != nil {
		return fmt.Errorf("写入消息实体失败：%w", err)
	}
	return nil
}

// CreateLink 在当前事务中插入 event header 和 completed 链接资源。
func (repository *Repository) CreateLink(ctx context.Context, tx *gorm.DB, event models.MeetingEvent, resource models.Resource) error {
	if tx == nil {
		return fmt.Errorf("创建访客链接：事务不可用")
	}
	if err := tx.WithContext(ctx).Select(eventColumns()).Create(&event).Error; err != nil {
		return fmt.Errorf("写入链接事件失败：%w", err)
	}
	if err := tx.WithContext(ctx).Select(resourceColumns()).Create(&resource).Error; err != nil {
		return fmt.Errorf("写入链接资源失败：%w", err)
	}
	return nil
}

// ListGuestTimelineRows 按统一 seq 扫描事件，只 join Guest 公开的实体字段。
func (repository *Repository) ListGuestTimelineRows(ctx context.Context, meetingID string, afterSeq int64, scanLimit int) ([]GuestTimelineRow, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || afterSeq < 0 || scanLimit <= 0 {
		return nil, fmt.Errorf("读取 Guest Timeline：参数无效")
	}
	statement := `SELECT
event.seq, event.kind AS event_kind, event.occurred_at,
COALESCE(message.id, '') AS message_id,
COALESCE(message.content, '') AS message_content,
COALESCE(message.display_name_snapshot, '') AS message_display_name,
COALESCE(resource.id, '') AS resource_id,
COALESCE(resource.kind, '') AS resource_kind,
COALESCE(resource.state, '') AS resource_state,
COALESCE(resource.source_url, '') AS resource_url,
COALESCE(resource.original_name, '') AS resource_name,
COALESCE(resource.media_type, '') AS resource_media_type,
COALESCE(resource.size_bytes, 0) AS resource_size,
COALESCE(resource.sha256, '') AS resource_sha256,
COALESCE(resource.current_description, '') AS resource_description,
COALESCE(guest.display_name, '') AS resource_display_name,
CASE WHEN event.kind = 'ai.answer'
          AND json_extract(event.payload_json, '$.v') = 1
          AND json_extract(event.payload_json, '$.guest_visible') = 1
     THEN COALESCE(json_extract(event.payload_json, '$.text'), '') ELSE '' END AS agent_answer_text,
CASE WHEN event.kind = 'ai.answer'
          AND json_extract(event.payload_json, '$.v') = 1
          AND json_extract(event.payload_json, '$.guest_visible') = 1
          AND trim(COALESCE(json_extract(event.payload_json, '$.text'), '')) <> ''
     THEN 1 ELSE 0 END AS agent_answer_visible
FROM meeting_events AS event
LEFT JOIN messages AS message
  ON event.kind = 'message.created' AND message.event_id = event.id AND message.meeting_id = event.meeting_id
LEFT JOIN resources AS resource
  ON event.kind = 'resource.created' AND resource.event_id = event.id AND resource.meeting_id = event.meeting_id
LEFT JOIN guest_sessions AS guest
  ON guest.id = resource.guest_session_id AND guest.meeting_id = event.meeting_id
WHERE event.meeting_id = ? AND event.seq > ?
ORDER BY event.seq ASC
LIMIT ?`
	var rows []GuestTimelineRow
	if err := repository.reader.WithContext(ctx).Raw(statement, meetingID, afterSeq, scanLimit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取 Guest Timeline 失败：%w", err)
	}
	return rows, nil
}

// findExistingMessage 显式 join event 返回已提交文字的 seq。
func findExistingMessage(ctx context.Context, tx *gorm.DB, sessionID string, requestID string) (*ExistingContent, error) {
	var row struct {
		EntityID   string `gorm:"column:entity_id"`
		Content    string `gorm:"column:content"`
		Seq        int64  `gorm:"column:seq"`
		OccurredAt int64  `gorm:"column:occurred_at"`
	}
	result := tx.WithContext(ctx).Table("messages AS message").
		Select("message.id AS entity_id", "message.content", "event.seq", "event.occurred_at").
		Joins("JOIN meeting_events AS event ON event.id = message.event_id").
		Where("message.guest_session_id = ? AND message.request_id = ?", sessionID, requestID).Limit(1).Find(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("查询消息幂等记录失败：%w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &ExistingContent{Kind: "text", Content: row.Content, EntityID: row.EntityID, Seq: row.Seq, OccurredAt: row.OccurredAt}, nil
}

// findExistingResource 显式 join event 返回已提交链接或附件的 seq 与幂等元数据。
func findExistingResource(ctx context.Context, tx *gorm.DB, sessionID string, requestID string) (*ExistingContent, error) {
	var row struct {
		EntityID     string `gorm:"column:entity_id"`
		Kind         string `gorm:"column:kind"`
		Content      string `gorm:"column:content"`
		Seq          int64  `gorm:"column:seq"`
		OccurredAt   int64  `gorm:"column:occurred_at"`
		OriginalName string `gorm:"column:original_name"`
		SizeBytes    int64  `gorm:"column:size_bytes"`
		SHA256       string `gorm:"column:sha256"`
		MediaType    string `gorm:"column:media_type"`
	}
	result := tx.WithContext(ctx).Table("resources AS resource").
		Select("resource.id AS entity_id", "resource.kind", "COALESCE(resource.source_url, '') AS content",
			"event.seq", "event.occurred_at", "COALESCE(resource.original_name, '') AS original_name",
			"COALESCE(resource.size_bytes, 0) AS size_bytes", "COALESCE(resource.sha256, '') AS sha256",
			"COALESCE(resource.media_type, '') AS media_type").
		Joins("JOIN meeting_events AS event ON event.id = resource.event_id").
		Where("resource.guest_session_id = ? AND resource.request_id = ?", sessionID, requestID).
		Limit(1).Find(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("查询链接幂等记录失败：%w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &ExistingContent{
		Kind: row.Kind, Content: row.Content, EntityID: row.EntityID, Seq: row.Seq, OccurredAt: row.OccurredAt,
		OriginalName: row.OriginalName, SizeBytes: row.SizeBytes, SHA256: row.SHA256, MediaType: row.MediaType,
	}, nil
}

// eventColumns 返回内容事件写入的显式字段。
func eventColumns() []string {
	return []string{"id", "meeting_id", "seq", "kind", "occurred_at", "source", "entity_type", "entity_id", "payload_json", "created_at", "updated_at"}
}

// messageColumns 返回 Guest 消息写入的显式字段。
func messageColumns() []string {
	return []string{
		"id", "meeting_id", "event_id", "author_kind", "member_id", "guest_session_id",
		"request_id", "display_name_snapshot", "content", "created_at", "updated_at",
	}
}

// resourceColumns 返回 Guest 链接资源写入的显式字段。
func resourceColumns() []string {
	return []string{
		"id", "meeting_id", "event_id", "guest_session_id", "request_id", "kind",
		"original_name", "safe_name", "relative_path", "source_url", "media_type", "size_bytes", "sha256",
		"original_description", "current_description", "description_revision", "state", "created_at", "updated_at",
	}
}
