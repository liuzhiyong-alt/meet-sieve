package guest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	contentrepository "meet-sieve/internal/repository/content"
	contentservice "meet-sieve/internal/service/content"
	guestservice "meet-sieve/internal/service/guest"
	"meet-sieve/models"

	crerrors "github.com/cockroachdb/errors"
	"gorm.io/gorm"
)

// TestContentService_TextCommitsEntityAndEventIdempotently 验证文字和事件同事务提交且重试不占新 seq。
func TestContentService_TextCommitsEntityAndEventIdempotently(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	session := insertActiveGuestSession(t, db, testSessionID, "王小明", strings.Repeat("a", 64))
	service := newContentService(db)
	input := guestservice.ContentInput{
		RequestID: "33333333-3333-4333-8333-333333333333", Kind: "text", Content: "请看\r\n设计稿",
	}

	first, err := service.Create(context.Background(), session, input)
	if err != nil {
		t.Fatalf("创建文字消息失败：%v", err)
	}
	second, err := service.Create(context.Background(), session, input)
	if err != nil {
		t.Fatalf("幂等重试失败：%v", err)
	}
	if first.Kind != "text" || first.Seq != 1 || second.EntityID != first.EntityID || second.Seq != first.Seq {
		t.Fatalf("幂等结果不正确：first=%#v second=%#v", first, second)
	}
	assertRowCount(t, db, "messages", 1)
	assertRowCount(t, db, "meeting_events", 1)
	var message models.Message
	if err := db.Where("id = ?", first.EntityID).Take(&message).Error; err != nil || message.Content != "请看\n设计稿" {
		t.Fatalf("消息正文或实体缺失：%#v err=%v", message, err)
	}
}

// TestHostContentService_CommitsMarkdownIdempotently 验证主持人消息进入统一 seq，且重试不重复通知。
func TestHostContentService_CommitsMarkdownIdempotently(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	notifications := 0
	service := contentservice.NewService(contentservice.Dependencies{
		Repository: contentrepository.NewRepository(db), Transactions: database.NewTransactionManager(db),
		Clock: clock.NewFixed(time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)),
		IDs: identity.NewFixedGenerator(
			"61616161-6161-4161-8161-616161616161", "62626262-6262-4262-8262-626262626262",
		),
		OnTimelineChanged: func(string, int64, string) { notifications++ },
	})
	input := contentservice.SendMessageInput{
		MeetingID: testMeetingID, RequestID: "33333333-3333-4333-8333-333333333333", Content: "**已确认**\r\n下一项",
	}
	first, err := service.SendHostMessage(context.Background(), input)
	if err != nil {
		t.Fatalf("主持人消息提交失败：%v", err)
	}
	second, err := service.SendHostMessage(context.Background(), input)
	if err != nil {
		t.Fatalf("主持人消息幂等重试失败：%v", err)
	}
	if first.Seq != 1 || second != first || notifications != 1 {
		t.Fatalf("主持人消息幂等结果错误：first=%#v second=%#v notifications=%d", first, second, notifications)
	}
	var message models.Message
	if err := db.Where("id = ?", first.EntityID).Take(&message).Error; err != nil {
		t.Fatal(err)
	}
	if message.AuthorKind != "host" || message.ContentFormat != "markdown" || message.Content != "**已确认**\n下一项" {
		t.Fatalf("主持人 Markdown 实体错误：%#v", message)
	}
	page, err := service.ListTimeline(context.Background(), contentservice.TimelineQuery{
		MeetingID: testMeetingID, Direction: "latest", Limit: 100,
	})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ContentFormat != "markdown" || page.Entries[0].Source != "host" {
		t.Fatalf("统一时间线主持人投影错误：page=%#v err=%v", page, err)
	}
}

// TestContentService_LinkAndTextURLRemainDistinct 验证链接显式建 Resource，text 中 URL 不启发式拆分。
func TestContentService_LinkAndTextURLRemainDistinct(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	session := insertActiveGuestSession(t, db, testSessionID, "访客", strings.Repeat("a", 64))
	service := newContentService(db)

	link, err := service.Create(context.Background(), session, guestservice.ContentInput{
		RequestID: "33333333-3333-4333-8333-333333333333", Kind: "link", Content: "https://example.com/design#v1",
	})
	if err != nil {
		t.Fatalf("创建链接失败：%v", err)
	}
	message, err := service.Create(context.Background(), session, guestservice.ContentInput{
		RequestID: "44444444-4444-4444-8444-444444444444", Kind: "text", Content: "请看 https://example.com/design",
	})
	if err != nil {
		t.Fatalf("创建包含 URL 的文字失败：%v", err)
	}
	if link.Kind != "link" || link.Seq != 1 || message.Kind != "text" || message.Seq != 2 {
		t.Fatalf("内容类型或统一 seq 不正确：link=%#v message=%#v", link, message)
	}
	assertRowCount(t, db, "resources", 1)
	assertRowCount(t, db, "messages", 1)
}

// TestContentService_IdempotencyConflictRejectsDifferentContent 验证同 session/request ID 不能改变 kind 或正文。
func TestContentService_IdempotencyConflictRejectsDifferentContent(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	session := insertActiveGuestSession(t, db, testSessionID, "访客", strings.Repeat("a", 64))
	service := newContentService(db)
	requestID := "33333333-3333-4333-8333-333333333333"
	if _, err := service.Create(context.Background(), session, guestservice.ContentInput{RequestID: requestID, Kind: "text", Content: "原文"}); err != nil {
		t.Fatalf("创建首次消息失败：%v", err)
	}
	_, err := service.Create(context.Background(), session, guestservice.ContentInput{RequestID: requestID, Kind: "link", Content: "https://example.com"})
	var appErr *apperr.AppError
	if !crerrors.As(err, &appErr) || appErr.Code != apperr.CodeConflict.Value {
		t.Fatalf("幂等冲突未返回稳定 409：%v", err)
	}
	assertRowCount(t, db, "meeting_events", 1)
}

// TestContentService_SameRequestIDIsScopedBySession 验证不同 session 可安全复用客户端 request ID。
func TestContentService_SameRequestIDIsScopedBySession(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	first := insertActiveGuestSession(t, db, testSessionID, "甲", strings.Repeat("a", 64))
	second := insertActiveGuestSession(t, db, "55555555-5555-4555-8555-555555555555", "乙", strings.Repeat("b", 64))
	service := newContentService(db)
	request := guestservice.ContentInput{RequestID: "33333333-3333-4333-8333-333333333333", Kind: "text", Content: "各自的消息"}

	firstResult, err := service.Create(context.Background(), first, request)
	if err != nil {
		t.Fatalf("第一个 session 写入失败：%v", err)
	}
	secondResult, err := service.Create(context.Background(), second, request)
	if err != nil {
		t.Fatalf("第二个 session 写入失败：%v", err)
	}
	if firstResult.EntityID == secondResult.EntityID || firstResult.Seq != 1 || secondResult.Seq != 2 {
		t.Fatalf("跨 session 幂等范围不正确：first=%#v second=%#v", firstResult, secondResult)
	}
}

// newContentService 创建使用真实 SQLite 单 writer 事务的内容服务。
func newContentService(db *gorm.DB) *guestservice.ContentService {
	return guestservice.NewContentService(guestservice.ContentDependencies{
		Repository:   contentrepository.NewRepository(db),
		Transactions: database.NewTransactionManager(db),
		Clock:        clock.NewFixed(time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)),
		IDs: identity.NewFixedGenerator(
			"66666666-6666-4666-8666-666666666661", "77777777-7777-4777-8777-777777777771",
			"66666666-6666-4666-8666-666666666662", "77777777-7777-4777-8777-777777777772",
			"66666666-6666-4666-8666-666666666663", "77777777-7777-4777-8777-777777777773",
		),
	})
}

// insertActiveGuestSession 写入供内容事务验证使用的活动 session。
func insertActiveGuestSession(t *testing.T, db *gorm.DB, id string, name string, tokenHash string) guestservice.AuthenticatedSession {
	t.Helper()
	session := models.GuestSession{
		ID: id, MeetingID: testMeetingID, DisplayName: name, SessionTokenHash: tokenHash,
		State: "active", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), CreatedAt: 0, UpdatedAt: 0,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("写入活动 session 失败：%v", err)
	}
	return guestservice.AuthenticatedSession{Session: session, Generation: "generation-1"}
}

// assertRowCount 断言指定固定表的行数。
func assertRowCount(t *testing.T, db *gorm.DB, table string, expected int64) {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil {
		t.Fatalf("统计 %s 失败：%v", table, err)
	}
	if count != expected {
		t.Fatalf("%s 行数不正确：got %d want %d", table, count, expected)
	}
}
