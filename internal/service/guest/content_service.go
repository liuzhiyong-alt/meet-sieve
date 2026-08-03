package guest

import (
	"context"
	"errors"
	"fmt"

	guestdomain "meet-sieve/internal/domain/guest"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	contentrepository "meet-sieve/internal/repository/content"
	"meet-sieve/models"

	crerrors "github.com/cockroachdb/errors"
	"gorm.io/gorm"
)

// ContentDependencies 是访客文字和链接事务的显式依赖。
type ContentDependencies struct {
	Repository   *contentrepository.Repository
	Transactions *database.TransactionManager
	Clock        clock.Clock
	IDs          identity.Generator
	OnPersisted  func(string)
}

// ContentService 保证访客内容实体与 meeting event 原子提交。
type ContentService struct {
	repository   *contentrepository.Repository
	transactions *database.TransactionManager
	clock        clock.Clock
	ids          identity.Generator
	onPersisted  func(string)
}

// ContentInput 是显式区分 text/link 的幂等写入请求。
type ContentInput struct {
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
}

// ContentResult 是 Guest API 和提交后刷新通知的轻量结果。
type ContentResult struct {
	Kind       string `json:"kind"`
	EntityID   string `json:"entity_id"`
	Seq        int64  `json:"seq"`
	OccurredAt int64  `json:"occurred_at"`
}

// NewContentService 创建只进行短 SQLite 事务的内容服务。
func NewContentService(dependencies ContentDependencies) *ContentService {
	return &ContentService{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		clock: dependencies.Clock, ids: dependencies.IDs,
		onPersisted: dependencies.OnPersisted,
	}
}

// Create 规范化内容，然后在一个短事务中完成幂等检查、seq 分配和实体写入。
func (service *ContentService) Create(ctx context.Context, authenticated AuthenticatedSession, input ContentInput) (ContentResult, error) {
	if service == nil || service.repository == nil || service.transactions == nil || service.clock == nil || service.ids == nil {
		return ContentResult{}, fmt.Errorf("访客内容服务不可用")
	}
	if err := guestdomain.ValidateRequestID(input.RequestID); err != nil {
		return ContentResult{}, err
	}
	normalized, err := normalizeContent(input.Kind, input.Content)
	if err != nil {
		return ContentResult{}, err
	}
	var result ContentResult
	err = service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		committed, commitErr := service.createWithinTransaction(ctx, tx, authenticated.Session, input.RequestID, input.Kind, normalized)
		result = committed
		return commitErr
	})
	if err != nil {
		return ContentResult{}, mapContentError(err)
	}
	if service.onPersisted != nil {
		service.onPersisted(authenticated.Session.MeetingID)
	}
	return result, nil
}

// createWithinTransaction 表达幂等检查到实体+事件提交的主流程。
func (service *ContentService) createWithinTransaction(
	ctx context.Context,
	tx *gorm.DB,
	authenticated models.GuestSession,
	requestID string,
	kind string,
	content string,
) (ContentResult, error) {
	session, err := service.repository.GetWritableSession(ctx, tx, authenticated.MeetingID, authenticated.ID)
	if err != nil {
		return ContentResult{}, err
	}
	existing, err := service.repository.FindExisting(ctx, tx, session.ID, requestID)
	if err != nil {
		return ContentResult{}, err
	}
	if existing != nil {
		if existing.Kind != kind || existing.Content != content {
			return ContentResult{}, apperr.Biz(apperr.CodeConflict, apperr.WithOp("guest.content.idempotency_conflict"))
		}
		return resultFromExisting(*existing), nil
	}
	return service.commitNewContent(ctx, tx, session, requestID, kind, content)
}

// commitNewContent 分配唯一 seq 并写入一种显式内容实体。
func (service *ContentService) commitNewContent(
	ctx context.Context,
	tx *gorm.DB,
	session models.GuestSession,
	requestID string,
	kind string,
	content string,
) (ContentResult, error) {
	seq, err := service.repository.NextEventSeq(ctx, tx, session.MeetingID)
	if err != nil {
		return ContentResult{}, err
	}
	eventID := service.ids.New()
	entityID := service.ids.New()
	if eventID == "" || entityID == "" {
		return ContentResult{}, fmt.Errorf("生成内容实体 ID 失败")
	}
	now := service.clock.Now().UnixMilli()
	entityType := "message"
	eventKind := "message.created"
	if kind == "link" {
		entityType = "resource"
		eventKind = "resource.created"
	}
	event := models.MeetingEvent{
		ID: eventID, MeetingID: session.MeetingID, Seq: seq, Kind: eventKind, OccurredAt: now,
		Source: "guest", EntityType: &entityType, EntityID: &entityID, CreatedAt: now, UpdatedAt: now,
	}
	if kind == "text" {
		err = service.repository.CreateMessage(ctx, tx, event, buildMessage(entityID, eventID, requestID, content, session, now))
	} else {
		err = service.repository.CreateLink(ctx, tx, event, buildLink(entityID, eventID, requestID, content, session, now))
	}
	if err != nil {
		return ContentResult{}, err
	}
	return ContentResult{Kind: kind, EntityID: entityID, Seq: seq, OccurredAt: now}, nil
}

// normalizeContent 根据显式 kind 选择文字或 URL 规则，不做 URL 启发式。
func normalizeContent(kind string, content string) (string, error) {
	switch kind {
	case "text":
		return guestdomain.NormalizeMessage(content)
	case "link":
		return guestdomain.NormalizeLink(content)
	default:
		return "", apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("guest.content.kind"))
	}
}

// buildMessage 构建作者名称已快照化的 Guest 文字实体。
func buildMessage(id string, eventID string, requestID string, content string, session models.GuestSession, now int64) models.Message {
	return models.Message{
		ID: id, MeetingID: session.MeetingID, EventID: eventID, AuthorKind: "guest",
		GuestSessionID: &session.ID, RequestID: &requestID, DisplayNameSnapshot: session.DisplayName,
		Content: content, CreatedAt: now, UpdatedAt: now,
	}
}

// buildLink 构建 completed 链接资源，不创建任何本地文件。
func buildLink(id string, eventID string, requestID string, sourceURL string, session models.GuestSession, now int64) models.Resource {
	return models.Resource{
		ID: id, MeetingID: session.MeetingID, EventID: eventID, GuestSessionID: &session.ID,
		RequestID: &requestID, Kind: "link", SourceURL: &sourceURL, State: "completed",
		DescriptionRevision: 1, CreatedAt: now, UpdatedAt: now,
	}
}

// resultFromExisting 复用首次已提交结果，不再分配 seq。
func resultFromExisting(existing contentrepository.ExistingContent) ContentResult {
	return ContentResult{
		Kind: existing.Kind, EntityID: existing.EntityID, Seq: existing.Seq, OccurredAt: existing.OccurredAt,
	}
}

// mapContentError 将事务内业务失败保留为稳定错误，未知 SQLite 失败归一为内部错误。
func mapContentError(err error) error {
	var appErr *apperr.AppError
	if crerrors.As(err, &appErr) {
		return appErr
	}
	if errors.Is(err, contentrepository.ErrSessionInactive) {
		return apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("guest.content.session"))
	}
	if errors.Is(err, contentrepository.ErrMeetingNotWritable) {
		return apperr.Biz(apperr.CodeLANMeetingEnded, apperr.WithOp("guest.content.meeting"))
	}
	return apperr.Sys(err, apperr.WithOp("guest.content.commit"))
}
