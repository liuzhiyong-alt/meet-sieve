package meeting

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"
)

// TestRecoveryServiceRepairsPartAndRebuildsRecording 验证启动恢复按真实文件修复分片、补资产并禁止续录。
func TestRecoveryServiceRepairsPartAndRebuildsRecording(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	meeting := models.Meeting{
		ID: "11111111-1111-4111-8111-111111111111", MeetingNo: "20260801-ABCD-01",
		Subject: "崩溃恢复", RelativeDir: "meetings/20260801-ABCD-01-崩溃恢复", LocalTimezone: "Asia/Shanghai",
		StartedAt: int64TestPointer(10), LifecycleState: "recording", LocalSaveState: "saving",
		RealtimeASRState: "idle", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled",
		CreatedAt: 1, UpdatedAt: 10,
	}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatalf("准备崩溃会议失败：%v", err)
	}
	segmentsDirectory := filepath.Join(root, filepath.FromSlash(meeting.RelativeDir), "audio", "segments")
	if err := os.MkdirAll(segmentsDirectory, 0o700); err != nil {
		t.Fatalf("创建分片目录失败：%v", err)
	}
	writer, err := NewWAVPartWriter(filepath.Join(segmentsDirectory, "000001.wav.part"), 32000)
	if err != nil {
		t.Fatalf("创建崩溃 part 失败：%v", err)
	}
	if err := writer.WritePCM([]byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("写入崩溃 part 失败：%v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("模拟进程退出失败：%v", err)
	}

	recovery := NewRecoveryService(RecoveryDependencies{
		Repository: repository, WorkspaceRoot: root, Clock: clock.NewFixed(time.UnixMilli(20)),
		IDs: identity.NewFixedGenerator(
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		), CheckpointSamples: 32000,
	})
	results, err := recovery.RecoverInterruptedMeetings(context.Background())
	if err != nil {
		t.Fatalf("启动恢复失败：%v", err)
	}
	if len(results) != 1 || !results[0].RecordingRecovered {
		t.Fatalf("恢复投影不正确：%+v", results)
	}
	for _, path := range []string{
		filepath.Join(segmentsDirectory, "000001.wav"),
		filepath.Join(root, filepath.FromSlash(meeting.RelativeDir), "audio", "recording.wav"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("恢复后缺少文件 %s：%v", path, err)
		}
	}
	stored, err := repository.GetMeeting(context.Background(), meeting.ID)
	if err != nil {
		t.Fatalf("读取恢复会议失败：%v", err)
	}
	if stored.LifecycleState != "interrupted" || stored.LocalSaveState != "saved" {
		t.Fatalf("恢复会议状态不正确：%+v", stored)
	}
	var assetCount int64
	if err := db.Model(&models.AudioAsset{}).Where("meeting_id = ?", meeting.ID).Count(&assetCount).Error; err != nil || assetCount != 2 {
		t.Fatalf("恢复资产数量不正确：count=%d err=%v", assetCount, err)
	}
	again, err := recovery.RecoverInterruptedMeetings(context.Background())
	if err != nil || len(again) != 0 {
		t.Fatalf("重复启动恢复必须幂等：results=%+v err=%v", again, err)
	}
}

// TestRecoveryServicePreservesUnrepairablePart 验证无法确认的 part 不被删除或伪造成 ready 资产。
func TestRecoveryServicePreservesUnrepairablePart(t *testing.T) {
	root, db := openRuntimeMeetingDatabase(t)
	repository := meetingrepository.NewRepository(db)
	meeting := models.Meeting{
		ID: "44444444-4444-4444-8444-444444444444", MeetingNo: "20260801-ABCD-02",
		Subject: "损坏恢复", RelativeDir: "meetings/20260801-ABCD-02-损坏恢复", LocalTimezone: "Asia/Shanghai",
		StartedAt: int64TestPointer(10), LifecycleState: "finalizing", LocalSaveState: "saving",
		RealtimeASRState: "idle", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled",
		CreatedAt: 1, UpdatedAt: 10,
	}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatalf("准备损坏会议失败：%v", err)
	}
	segmentsDirectory := filepath.Join(root, filepath.FromSlash(meeting.RelativeDir), "audio", "segments")
	if err := os.MkdirAll(segmentsDirectory, 0o700); err != nil {
		t.Fatalf("创建分片目录失败：%v", err)
	}
	partPath := filepath.Join(segmentsDirectory, "000001.wav.part")
	if err := os.WriteFile(partPath, []byte("broken"), 0o600); err != nil {
		t.Fatalf("准备损坏 part 失败：%v", err)
	}
	recovery := NewRecoveryService(RecoveryDependencies{
		Repository: repository, WorkspaceRoot: root, Clock: clock.NewFixed(time.UnixMilli(20)),
		IDs: identity.NewUUIDGenerator(), CheckpointSamples: 32000,
	})

	results, err := recovery.RecoverInterruptedMeetings(context.Background())
	if err != nil || len(results) != 1 || results[0].ErrorCode != "MEETING_RECOVERY_FAILED" {
		t.Fatalf("损坏恢复结果不正确：results=%+v err=%v", results, err)
	}
	if _, err := os.Stat(partPath); err != nil {
		t.Fatalf("无法修复的 part 必须保留：%v", err)
	}
	stored, err := repository.GetMeeting(context.Background(), meeting.ID)
	if err != nil || stored.LifecycleState != "interrupted" || stored.LocalSaveState != "failed" {
		t.Fatalf("损坏会议状态不正确：meeting=%+v err=%v", stored, err)
	}
	var count int64
	if err := db.Model(&models.AudioAsset{}).Where("meeting_id = ?", meeting.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("损坏 part 不得生成 ready 资产：count=%d err=%v", count, err)
	}
}

// int64TestPointer 返回测试模型使用的 int64 指针。
func int64TestPointer(value int64) *int64 { return &value }
