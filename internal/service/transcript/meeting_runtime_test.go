package transcript

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	"meet-sieve/models"
)

// TestMeetingRuntimeRecordOnlyPersistsFullGapAndRawRecord 验证仅录音模式生成整段 gap 和确定性原始记录。
func TestMeetingRuntimeRecordOnlyPersistsFullGapAndRawRecord(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	workspace := t.TempDir()
	meetingDirectory := filepath.Join(workspace, "meetings", "realtime")
	if err := os.MkdirAll(meetingDirectory, 0o700); err != nil {
		t.Fatalf("创建 record_only 会议目录失败：%v", err)
	}
	runtime := NewMeetingRuntime(MeetingRuntimeDependencies{
		Settings:   NewSettingsService(SettingsServiceDependencies{Repository: repository}),
		Repository: repository, Transactions: database.NewTransactionManager(db), Events: events,
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber {
			return &coordinatorTranscriber{session: newCoordinatorSession(0)}
		},
		IDs: identity.NewFixedGenerator("77777777-7777-4777-8777-777777777777"), Clock: clock.NewFixed(time.UnixMilli(2_000)),
		Backoff:             []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second},
		FinalPersistTimeout: time.Second, FinalQueueCapacity: 128,
		RawRecord: NewRawRecordProjector(RawRecordProjectorDependencies{Repository: repository, WorkspaceRoot: workspace}), WorkspaceRoot: workspace,
	})
	if err := runtime.Start(context.Background(), testMeetingID, MeetingASRModeRecordOnly); err != nil {
		t.Fatalf("启动 record_only 失败：%v", err)
	}
	if err := db.Model(&models.Meeting{}).Where("id = ?", testMeetingID).Updates(map[string]any{"lifecycle_state": "finalizing"}).Error; err != nil {
		t.Fatalf("准备 record_only 收尾状态失败：%v", err)
	}
	if err := runtime.Stop(context.Background(), testMeetingID, 32000); err != nil {
		t.Fatalf("停止 record_only 失败：%v", err)
	}
	var gap models.ASRGap
	if err := db.Where("meeting_id = ?", testMeetingID).Take(&gap).Error; err != nil {
		t.Fatalf("读取 record_only gap 失败：%v", err)
	}
	if gap.StartSample != 0 || gap.EndSample != 32000 || gap.Reason != "record_only" {
		t.Fatalf("record_only gap 错误：%+v", gap)
	}
	content, err := os.ReadFile(filepath.Join(meetingDirectory, "会议原始记录.md"))
	if err != nil || len(content) == 0 {
		t.Fatalf("record_only 原始记录未生成：bytes=%d err=%v", len(content), err)
	}
}
