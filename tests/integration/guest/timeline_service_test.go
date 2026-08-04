package guest_test

import (
	"context"
	"strings"
	"testing"

	contentrepository "meet-sieve/internal/repository/content"
	guestservice "meet-sieve/internal/service/guest"

	"gorm.io/gorm"
)

// TestTimelineService_AllowlistProjectionAdvancesPastInvisibleEvents 验证访客只看到白名单投影，不可见事件仍推进 next_seq。
func TestTimelineService_AllowlistProjectionAdvancesPastInvisibleEvents(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	session := insertActiveGuestSession(t, db, testSessionID, "访客", strings.Repeat("a", 64))
	insertInvisibleEvent(t, db, "88888888-8888-4888-8888-888888888881", 1, "ai.question")
	content := newContentService(db)
	if _, err := content.Create(context.Background(), session, guestservice.ContentInput{
		RequestID: "33333333-3333-4333-8333-333333333333", Kind: "text", Content: "会议消息",
	}); err != nil {
		t.Fatalf("创建消息失败：%v", err)
	}
	if _, err := content.Create(context.Background(), session, guestservice.ContentInput{
		RequestID: "44444444-4444-4444-8444-444444444444", Kind: "link", Content: "https://example.com/design",
	}); err != nil {
		t.Fatalf("创建链接失败：%v", err)
	}
	insertInvisibleEvent(t, db, "88888888-8888-4888-8888-888888888884", 4, "ai.cancelled")
	service := guestservice.NewTimelineService(contentrepository.NewRepository(db))

	first, err := service.List(context.Background(), testMeetingID, 0, 2)
	if err != nil {
		t.Fatalf("读取第一页失败：%v", err)
	}
	if len(first.Events) != 1 || first.Events[0].Kind != "message" || first.NextSeq != 2 || !first.HasMore {
		t.Fatalf("第一页白名单或游标不正确：%#v", first)
	}
	second, err := service.List(context.Background(), testMeetingID, first.NextSeq, 2)
	if err != nil {
		t.Fatalf("读取第二页失败：%v", err)
	}
	if len(second.Events) != 1 || second.Events[0].Kind != "link" || second.NextSeq != 4 || second.HasMore {
		t.Fatalf("第二页白名单或游标不正确：%#v", second)
	}
	if second.Events[0].URL != "https://example.com/design" || second.Events[0].DisplayName != "访客" {
		t.Fatalf("链接安全投影不完整：%#v", second.Events[0])
	}
}

// TestTimelineService_ClampsLimit 验证事件扫描页大小缺省 100 且最大 200。
func TestTimelineService_ClampsLimit(t *testing.T) {
	t.Parallel()
	if got := guestservice.NormalizeTimelineLimit(0); got != 100 {
		t.Fatalf("缺省 limit=%d, want 100", got)
	}
	if got := guestservice.NormalizeTimelineLimit(500); got != 200 {
		t.Fatalf("最大 limit=%d, want 200", got)
	}
}

// TestTimelineService_ExposesOnlyVersionedGuestVisibleAIAnswer 验证 Guest 只读取合法公开回答。
func TestTimelineService_ExposesOnlyVersionedGuestVisibleAIAnswer(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	rows := []struct {
		id      string
		seq     int
		kind    string
		payload string
	}{
		{"91919191-9191-4919-8919-919191919191", 1, "ai.question", `{"v":1,"text":"秘密问题"}`},
		{"92929292-9292-4929-8929-929292929292", 2, "ai.answer", `{"v":1,"text":"公开回答","guest_visible":true}`},
		{"92929292-9292-4929-8929-929292929293", 3, "ai.answer", `{"v":2,"text":"**Markdown 回答**","content_format":"markdown","guest_visible":true}`},
		{"93939393-9393-4939-8939-939393939393", 4, "ai.answer", `{"v":1,"text":"内部回答","guest_visible":false}`},
		{"94949494-9494-4949-8949-949494949494", 5, "ai.failed", `{"v":1,"reason":"failed"}`},
	}
	for _, row := range rows {
		if err := db.Exec(`INSERT INTO meeting_events (
			id, meeting_id, seq, kind, occurred_at, source, payload_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, 1785672000000, 'agent', ?, 1785672000000, 1785672000000)`,
			row.id, testMeetingID, row.seq, row.kind, row.payload).Error; err != nil {
			t.Fatal(err)
		}
	}
	page, err := guestservice.NewTimelineService(contentrepository.NewRepository(db)).List(context.Background(), testMeetingID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].Kind != "ai_answer" || page.Events[0].Text != "公开回答" || page.Events[0].ContentFormat != "plain" || page.Events[1].ContentFormat != "markdown" || page.NextSeq != 5 {
		t.Fatalf("Guest AI 白名单错误：%#v", page)
	}
}

// insertInvisibleEvent 写入合法但不向 Guest 公开的会议事件。
func insertInvisibleEvent(t *testing.T, db *gorm.DB, id string, seq int64, kind string) {
	t.Helper()
	err := db.Exec(`INSERT INTO meeting_events (
		id, meeting_id, seq, kind, occurred_at, source, created_at, updated_at
	) VALUES (?, ?, ?, ?, 1785672000000, 'agent', 1785672000000, 1785672000000)`, id, testMeetingID, seq, kind).Error
	if err != nil {
		t.Fatalf("写入不可见事件失败：%v", err)
	}
}
