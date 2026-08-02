package meeting_test

import (
	"context"
	"errors"
	"testing"

	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestRepositoryPersistsMeetingLifecycle 验证录音提交点到安全结束的状态更新均受前置状态约束。
func TestRepositoryPersistsMeetingLifecycle(t *testing.T) {
	db := openMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	meeting := insertPreparingMeeting(t, db, "11111111-1111-4111-8111-111111111111", "20260801-ABCD-01")

	if err := repository.MarkRecordingStarted(context.Background(), meeting.ID, 100); err != nil {
		t.Fatalf("标记录音开始失败：%v", err)
	}
	if err := repository.BeginFinalizing(context.Background(), meeting.ID, 200); err != nil {
		t.Fatalf("取得收尾权失败：%v", err)
	}
	if err := repository.CompleteMeeting(context.Background(), meeting.ID, 300); err != nil {
		t.Fatalf("完成会议失败：%v", err)
	}

	var stored models.Meeting
	if err := db.Where("id = ?", meeting.ID).Take(&stored).Error; err != nil {
		t.Fatalf("读取会议失败：%v", err)
	}
	if stored.LifecycleState != "ended" || stored.LocalSaveState != "saved" || stored.StartedAt == nil || *stored.StartedAt != 100 || stored.EndedAt == nil || *stored.EndedAt != 300 {
		t.Fatalf("会议终态不正确：%+v", stored)
	}
	if err := repository.BeginFinalizing(context.Background(), meeting.ID, 400); !errors.Is(err, meetingrepository.ErrMeetingStateConflict) {
		t.Fatalf("终态会议必须拒绝重新收尾：%v", err)
	}
}

// TestRepositoryInterruptsActiveMeeting 验证活动会议故障会原子进入 interrupted/failed。
func TestRepositoryInterruptsActiveMeeting(t *testing.T) {
	db := openMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	meeting := insertPreparingMeeting(t, db, "22222222-2222-4222-8222-222222222222", "20260801-ABCD-02")
	if err := repository.MarkRecordingStarted(context.Background(), meeting.ID, 50); err != nil {
		t.Fatalf("准备中断录音会议失败：%v", err)
	}

	if err := repository.InterruptMeeting(context.Background(), meeting.ID, 100); err != nil {
		t.Fatalf("中断会议失败：%v", err)
	}
	var stored models.Meeting
	if err := db.Where("id = ?", meeting.ID).Take(&stored).Error; err != nil {
		t.Fatalf("读取会议失败：%v", err)
	}
	if stored.LifecycleState != "interrupted" || stored.LocalSaveState != "failed" || stored.EndedAt == nil || *stored.EndedAt != 100 {
		t.Fatalf("会议中断状态不正确：%+v", stored)
	}
}

// TestRepositoryFinishesRecoveredMeeting 验证启动恢复会冻结中断时刻，避免界面计时继续增长。
func TestRepositoryFinishesRecoveredMeeting(t *testing.T) {
	db := openMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	meeting := insertPreparingMeeting(t, db, "33333333-3333-4333-8333-333333333333", "20260801-ABCD-03")
	if err := repository.MarkRecordingStarted(context.Background(), meeting.ID, 100); err != nil {
		t.Fatalf("准备恢复录音会议失败：%v", err)
	}

	if err := repository.FinishRecovery(context.Background(), meeting.ID, true, 200); err != nil {
		t.Fatalf("完成会议恢复失败：%v", err)
	}
	var stored models.Meeting
	if err := db.Where("id = ?", meeting.ID).Take(&stored).Error; err != nil {
		t.Fatalf("读取恢复会议失败：%v", err)
	}
	if stored.LifecycleState != "interrupted" || stored.LocalSaveState != "saved" || stored.EndedAt == nil || *stored.EndedAt != 200 {
		t.Fatalf("恢复会议终态不正确：%+v", stored)
	}
}

// insertPreparingMeeting 写入状态测试使用的最小合法 preparing 会议。
func insertPreparingMeeting(t *testing.T, db *gorm.DB, id string, meetingNo string) models.Meeting {
	t.Helper()
	meeting := models.Meeting{
		ID: id, MeetingNo: meetingNo, Subject: "状态测试", RelativeDir: "meetings/" + meetingNo,
		LocalTimezone: "Asia/Shanghai", LifecycleState: "preparing", LocalSaveState: "pending",
		RealtimeASRState: "idle", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled",
		CreatedAt: 1, UpdatedAt: 1,
	}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatalf("准备会议失败：%v", err)
	}
	return meeting
}
