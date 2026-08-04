package query_test

import (
	"context"
	"fmt"
	"testing"

	queryrepository "meet-sieve/internal/repository/query"
	queryservice "meet-sieve/internal/service/query"
)

// TestService_GetHomeFindsHighestPriorityOutsideRecentWindow 验证首页续办项不会被最近会议窗口截断。
func TestService_GetHomeFindsHighestPriorityOutsideRecentWindow(t *testing.T) {
	db := openQueryDatabase(t)
	for index := 0; index < 8; index++ {
		insertMeeting(
			t,
			db,
			fmt.Sprintf("10000000-0000-4000-8000-%012d", index+1),
			fmt.Sprintf("RECENT-%02d", index+1),
			"已完成会议",
			9000-int64(index*100),
			"saved",
		)
	}
	if err := db.Exec("UPDATE meetings SET minute_state = 'not_generated'").Error; err != nil {
		t.Fatalf("准备已保存会议失败：%v", err)
	}

	const recoveryMeetingID = "20000000-0000-4000-8000-000000000001"
	insertMeeting(t, db, recoveryMeetingID, "OLDER-RECOVERY", "较早的待恢复会议", 1000, "failed")
	if err := db.Exec(
		"UPDATE meetings SET lifecycle_state = 'interrupted', minute_state = 'not_generated' WHERE id = ?",
		recoveryMeetingID,
	).Error; err != nil {
		t.Fatalf("准备待恢复会议失败：%v", err)
	}

	service := queryservice.NewService(queryrepository.NewRepository(db))
	home, err := service.GetHome(context.Background())
	if err != nil {
		t.Fatalf("读取首页失败：%v", err)
	}
	if home.Continuation == nil || home.Continuation.ID != recoveryMeetingID {
		t.Fatalf("首页应选择全局待恢复会议：%+v", home.Continuation)
	}
	if len(home.RecentMeetings) != 5 || home.RecentMeetings[0].MeetingNo != "RECENT-01" {
		t.Fatalf("最近会议投影错误：%+v", home.RecentMeetings)
	}
}

// TestService_GetHomeCountsOnlySamePriorityMeetings 验证剩余数量只包含当前最高优先级的其他会议。
func TestService_GetHomeCountsOnlySamePriorityMeetings(t *testing.T) {
	db := openQueryDatabase(t)
	insertMeeting(t, db, "30000000-0000-4000-8000-000000000001", "RECOVERY-NEW", "较新的待恢复会议", 4000, "failed")
	insertMeeting(t, db, "30000000-0000-4000-8000-000000000002", "RECOVERY-OLD", "较早的待恢复会议", 3000, "failed")
	insertMeeting(t, db, "30000000-0000-4000-8000-000000000003", "GAP-PENDING", "待补转写会议", 2000, "saved")
	insertMeeting(t, db, "30000000-0000-4000-8000-000000000004", "SAVED", "已完成会议", 1000, "saved")
	if err := db.Exec("UPDATE meetings SET minute_state = 'not_generated'").Error; err != nil {
		t.Fatalf("准备首页状态失败：%v", err)
	}
	if err := db.Exec(
		"UPDATE meetings SET lifecycle_state = 'interrupted' WHERE meeting_no IN ('RECOVERY-NEW', 'RECOVERY-OLD')",
	).Error; err != nil {
		t.Fatalf("准备待恢复会议失败：%v", err)
	}
	if err := db.Exec("UPDATE meetings SET gap_state = 'pending' WHERE meeting_no = 'GAP-PENDING'").Error; err != nil {
		t.Fatalf("准备补转写会议失败：%v", err)
	}

	home, err := queryservice.NewService(queryrepository.NewRepository(db)).GetHome(context.Background())
	if err != nil {
		t.Fatalf("读取首页失败：%v", err)
	}
	if home.Continuation == nil || home.Continuation.MeetingNo != "RECOVERY-NEW" {
		t.Fatalf("首页应选择同优先级中最新的待恢复会议：%+v", home.Continuation)
	}
	if home.Remaining != 1 {
		t.Fatalf("首页剩余数量应只统计另一场待恢复会议：got %d", home.Remaining)
	}
}
