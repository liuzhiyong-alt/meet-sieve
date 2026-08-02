package wails_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	infraLogger "meet-sieve/internal/infra/logger"
	meetingrepository "meet-sieve/internal/repository/meeting"
	meetingservice "meet-sieve/internal/service/meeting"
	wailstransport "meet-sieve/internal/transport/wails"

	"gorm.io/gorm"
)

// TestMeetingBindingDraftAndActiveProjection 验证会议契约可恢复活动态且不泄漏工作目录路径。
func TestMeetingBindingDraftAndActiveProjection(t *testing.T) {
	db := openMeetingBindingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	service := meetingservice.NewService(meetingservice.Dependencies{
		Repository: repository, Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		), Clock: clock.NewFixed(time.Date(2026, 8, 1, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))),
		DeviceCode: "ABCD",
	})
	binding := wailstransport.NewMeetingBinding(
		func() (*meetingservice.Service, *meetingservice.RuntimeService, *meetingservice.RecoveryService, error) {
			return service, nil, nil, nil
		},
		func() context.Context { return context.Background() }, nil, wailstransport.NewBoundary(infraLogger.NewNop()),
	)

	draft := binding.GetCreateDraft()
	if draft.Code != 200 || draft.Data == nil || draft.Data.SuggestedMeetingNo != "20260801-ABCD-01" {
		t.Fatalf("会议草稿 DTO 不正确：%+v", draft)
	}
	if _, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-01", SuggestedMeetingNo: "20260801-ABCD-01",
		TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
	}); err != nil {
		t.Fatalf("准备活动会议失败：%v", err)
	}
	active := binding.GetActiveMeeting()
	if active.Code != 200 || active.Data == nil || !active.Data.Active || active.Data.Meeting == nil {
		t.Fatalf("活动会议 DTO 不正确：%+v", active)
	}
	encoded, err := json.Marshal(active)
	if err != nil {
		t.Fatalf("序列化活动会议失败：%v", err)
	}
	if strings.Contains(string(encoded), "relative_dir") || strings.Contains(string(encoded), "meetings/") {
		t.Fatalf("活动会议 DTO 泄漏了内部相对路径：%s", encoded)
	}
}

// openMeetingBindingDatabase 创建最新 schema 的会议 Binding 契约数据库。
func openMeetingBindingDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meeting-binding.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}
