package database_test

import (
	"testing"

	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestStep6Schema_AddsScopedGuestIdempotency 验证访客消息和资源由数据库约束保证会话内幂等。
func TestStep6Schema_AddsScopedGuestIdempotency(t *testing.T) {
	db := openMigratedDatabase(t)
	insertValidMeeting(t, db)
	insertGuestSession(t, db)

	requestID := "77777777-7777-4777-8777-777777777777"
	insertMeetingEvent(t, db, testEventID, 1)
	message := models.Message{
		ID: "88888888-8888-4888-8888-888888888888", MeetingID: testMeetingID, EventID: testEventID,
		AuthorKind: "guest", GuestSessionID: stringPointer("99999999-9999-4999-8999-999999999999"),
		RequestID: stringPointer(requestID), DisplayNameSnapshot: "访客", Content: "第一条消息",
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("写入首条访客消息失败：%v", err)
	}
	insertMeetingEvent(t, db, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 2)
	message.ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	message.EventID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if err := db.Create(&message).Error; err == nil {
		t.Fatal("同一访客会话和 request_id 必须被唯一约束拒绝")
	}
}

// insertGuestSession 写入供 Step 6 schema 契约复用的活动访客会话。
func insertGuestSession(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Exec(`INSERT INTO guest_sessions (
		id, meeting_id, display_name, session_token_hash, state, expires_at, created_at, updated_at
	) VALUES (
		'99999999-9999-4999-8999-999999999999', ?, '访客',
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'active', 86400000, 0, 0
	)`, testMeetingID).Error
	if err != nil {
		t.Fatalf("写入访客会话失败：%v", err)
	}
}

// stringPointer 返回测试模型所需的字符串指针。
func stringPointer(value string) *string { return &value }
