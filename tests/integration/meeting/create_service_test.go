package meeting_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	meetingrepository "meet-sieve/internal/repository/meeting"
	meetingservice "meet-sieve/internal/service/meeting"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestCreatePreparingPersistsMeetingAndOrderedSnapshots 验证会议与有序参会者快照在同一事务提交。
func TestCreatePreparingPersistsMeetingAndOrderedSnapshots(t *testing.T) {
	db := openMeetingDatabase(t)
	service := meetingservice.NewService(meetingservice.Dependencies{
		Repository:   meetingrepository.NewRepository(db),
		Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
			"44444444-4444-4444-8444-444444444444",
		),
		Clock:      clock.NewFixed(time.Date(2026, 8, 1, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))),
		DeviceCode: "ABCD",
	})

	created, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo:                 "20260801-ABCD-01",
		Subject:                   "  产品周会  ",
		TemporaryParticipantNames: []string{"访客甲", "访客乙"},
		LocalTimezone:             "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("创建 preparing 会议失败：%v", err)
	}
	if created.ID != "22222222-2222-4222-8222-222222222222" || created.Subject != "产品周会" || created.LifecycleState != "preparing" || created.LocalSaveState != "pending" {
		t.Fatalf("会议投影不正确：%+v", created)
	}

	var participants []models.MeetingParticipant
	if err := db.Select("id", "meeting_id", "member_id", "participant_kind", "display_name_snapshot", "sort_order", "created_at", "updated_at").
		Where("meeting_id = ?", created.ID).Order("sort_order ASC").Find(&participants).Error; err != nil {
		t.Fatalf("读取参会者快照失败：%v", err)
	}
	if len(participants) != 2 || participants[0].DisplayNameSnapshot != "访客甲" || participants[0].SortOrder != 0 || participants[1].DisplayNameSnapshot != "访客乙" || participants[1].SortOrder != 1 {
		t.Fatalf("参会者快照不正确：%+v", participants)
	}
}

// TestCreatePreparingMapsActiveMeetingConflict 验证重复开始返回稳定业务错误且不创建第二场会议。
func TestCreatePreparingMapsActiveMeetingConflict(t *testing.T) {
	db := openMeetingDatabase(t)
	first := newTemporaryMeetingService(db,
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	)
	if _, err := first.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-01", TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
	}); err != nil {
		t.Fatalf("准备首场会议失败：%v", err)
	}

	second := newTemporaryMeetingService(db,
		"44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555",
		"66666666-6666-4666-8666-666666666666",
	)
	_, err := second.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-02", TemporaryParticipantNames: []string{"另一访客"}, LocalTimezone: "Asia/Shanghai",
	})
	if got := apperr.Normalize(err); got.ErrorCode != "MEETING_ALREADY_ACTIVE" || got.Kind != apperr.KindBusiness {
		t.Fatalf("活动会议冲突语义不正确：%+v", got)
	}
}

// TestCreatePreparingMapsInvalidMeetingNumber 验证非法会议号在事务前返回稳定校验错误。
func TestCreatePreparingMapsInvalidMeetingNumber(t *testing.T) {
	db := openMeetingDatabase(t)
	service := newTemporaryMeetingService(db,
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	)

	_, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "invalid", TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
	})
	if got := apperr.Normalize(err); got.ErrorCode != "MEETING_NUMBER_INVALID" || got.Kind != apperr.KindValidation {
		t.Fatalf("会议号校验错误语义不正确：%+v", got)
	}
}

// TestCreatePreparingSnapshotsActiveMemberName 验证成员快照名称来自事务内数据库事实。
func TestCreatePreparingSnapshotsActiveMemberName(t *testing.T) {
	db := openMeetingDatabase(t)
	memberID := "77777777-7777-4777-8777-777777777777"
	if err := db.Exec(`INSERT INTO members (
		id, name, name_normalized, created_at, updated_at
	) VALUES (?, '数据库姓名', '数据库姓名', 0, 0)`, memberID).Error; err != nil {
		t.Fatalf("准备活动成员失败：%v", err)
	}
	service := newTemporaryMeetingService(db,
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	)

	created, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-01", MemberIDs: []string{memberID}, LocalTimezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("使用活动成员创建会议失败：%v", err)
	}
	var participant models.MeetingParticipant
	if err := db.Select("id", "meeting_id", "member_id", "participant_kind", "display_name_snapshot", "sort_order", "created_at", "updated_at").
		Where("meeting_id = ?", created.ID).Take(&participant).Error; err != nil {
		t.Fatalf("读取成员快照失败：%v", err)
	}
	if participant.MemberID == nil || *participant.MemberID != memberID || participant.DisplayNameSnapshot != "数据库姓名" || participant.ParticipantKind != "member" {
		t.Fatalf("成员快照不正确：%+v", participant)
	}
}

// TestCreatePreparingMapsParticipantsRequired 验证空参会者提交返回稳定业务错误且不进入事务。
func TestCreatePreparingMapsParticipantsRequired(t *testing.T) {
	db := openMeetingDatabase(t)
	service := newTemporaryMeetingService(db,
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	)

	_, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-01", LocalTimezone: "Asia/Shanghai",
	})
	if got := apperr.Normalize(err); got.ErrorCode != "MEETING_PARTICIPANTS_REQUIRED" || got.Kind != apperr.KindBusiness {
		t.Fatalf("空参会者错误语义不正确：%+v", got)
	}
}

// TestCreatePreparingMapsInvalidParticipant 验证不存在、归档或重复成员返回稳定错误。
func TestCreatePreparingMapsInvalidParticipant(t *testing.T) {
	db := openMeetingDatabase(t)
	service := newTemporaryMeetingService(db,
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	)

	_, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-01", MemberIDs: []string{"missing-member"}, LocalTimezone: "Asia/Shanghai",
	})
	if got := apperr.Normalize(err); got.ErrorCode != "MEETING_PARTICIPANT_INVALID" {
		t.Fatalf("无效参会者错误语义不正确：%+v", got)
	}
}

// TestCreatePreparingMapsMeetingNumberConflict 验证历史会议号冲突不会泄漏 SQLite 错误。
func TestCreatePreparingMapsMeetingNumberConflict(t *testing.T) {
	db := openMeetingDatabase(t)
	if err := db.Create(&models.Meeting{
		ID: "99999999-9999-4999-8999-999999999999", MeetingNo: "20260801-ABCD-01",
		Subject: "历史会议", RelativeDir: "meetings/history", LocalTimezone: "Asia/Shanghai",
		StartedAt: int64Pointer(1), EndedAt: int64Pointer(2), LifecycleState: "ended", LocalSaveState: "saved",
		RealtimeASRState: "idle", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled",
		CreatedAt: 1, UpdatedAt: 2,
	}).Error; err != nil {
		t.Fatalf("准备历史会议失败：%v", err)
	}
	service := newTemporaryMeetingService(db,
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	)

	_, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-01", TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
	})
	if got := apperr.Normalize(err); got.ErrorCode != "MEETING_NUMBER_CONFLICT" {
		t.Fatalf("会议号冲突错误语义不正确：%+v", got)
	}
}

// TestCreatePreparingReplacesUneditedSuggestionInsideTransaction 验证未编辑建议号会使用事务内实际序号。
func TestCreatePreparingReplacesUneditedSuggestionInsideTransaction(t *testing.T) {
	db := openMeetingDatabase(t)
	if err := db.Create(&models.MeetingNumberSequence{
		ID: "99999999-9999-4999-8999-999999999999", LocalDate: "20260801", DeviceCode: "ABCD",
		NextSequence: 2, CreatedAt: 1, UpdatedAt: 1,
	}).Error; err != nil {
		t.Fatalf("准备会议序号失败：%v", err)
	}
	service := newTemporaryMeetingService(db,
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	)

	created, err := service.CreatePreparing(context.Background(), meetingservice.CreatePreparingInput{
		MeetingNo: "20260801-ABCD-01", SuggestedMeetingNo: "20260801-ABCD-01",
		TemporaryParticipantNames: []string{"访客"}, LocalTimezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("使用建议号创建会议失败：%v", err)
	}
	if created.MeetingNo != "20260801-ABCD-02" {
		t.Fatalf("必须使用事务内实际序号，got=%s", created.MeetingNo)
	}
}

// TestGetCreateDraftDoesNotConsumeSequence 验证打开开始页只预览建议号，不提前写入每日序号。
func TestGetCreateDraftDoesNotConsumeSequence(t *testing.T) {
	db := openMeetingDatabase(t)
	service := newTemporaryMeetingService(db)

	draft, err := service.GetCreateDraft(context.Background())
	if err != nil {
		t.Fatalf("读取创建草稿失败：%v", err)
	}
	if draft.SuggestedMeetingNo != "20260801-ABCD-01" || draft.DefaultSubject != "未命名会议" {
		t.Fatalf("创建草稿不正确：%+v", draft)
	}
	var count int64
	if err := db.Model(&models.MeetingNumberSequence{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("预览建议号不得消耗序号：count=%d err=%v", count, err)
	}
}

// int64Pointer 返回测试记录所需的时间指针。
func int64Pointer(value int64) *int64 { return &value }

// newTemporaryMeetingService 创建使用固定 UUID 和同一真实 SQLite 的会议服务。
func newTemporaryMeetingService(db *gorm.DB, ids ...string) *meetingservice.Service {
	return meetingservice.NewService(meetingservice.Dependencies{
		Repository: meetingrepository.NewRepository(db), Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(ids...), Clock: clock.NewFixed(time.Date(2026, 8, 1, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))),
		DeviceCode: "ABCD",
	})
}

// openMeetingDatabase 创建执行最新 migration 的隔离 SQLite 数据库。
func openMeetingDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meeting.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}
