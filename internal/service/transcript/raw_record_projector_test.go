package transcript

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"
)

// TestRawRecordProjector_RebuildKeepsOldFileOnWriteFailure 验证投影写失败不会破坏已有完整 Markdown。
func TestRawRecordProjector_RebuildKeepsOldFileOnWriteFailure(t *testing.T) {
	service, db := newEventServiceForTest(t,
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
	)
	if _, err := service.PersistFinal(context.Background(), FinalInput{MeetingID: testMeetingID, ASRSessionID: testSessionID, ProviderResultID: "provider-final-1", Text: "保留的转写", Range: mustSampleRange(t, 0, 16000), LastSentSample: 64000}); err != nil {
		t.Fatalf("写入 final 失败：%v", err)
	}
	var meeting models.Meeting
	if err := db.Where("id = ?", testMeetingID).Take(&meeting).Error; err != nil {
		t.Fatalf("读取会议失败：%v", err)
	}
	target := filepath.Join(t.TempDir(), "会议原始记录.md")
	if err := os.WriteFile(target, []byte("旧记录\n"), 0o600); err != nil {
		t.Fatalf("创建旧记录失败：%v", err)
	}
	projector := NewRawRecordProjector(RawRecordProjectorDependencies{Repository: transcriptrepository.NewRepository(db), WriteAtomic: func(string, []byte, os.FileMode) error { return errors.New("disk failed") }})
	err := projector.Rebuild(context.Background(), meeting, target)
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeRawRecordRefreshFailed.ErrorCode {
		t.Fatalf("写失败必须是 RAW_RECORD_REFRESH_FAILED：%v", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "旧记录\n" {
		t.Fatalf("写失败不得破坏旧记录：content=%q err=%v", content, readErr)
	}
}

// TestRawRecordProjector_MarkDirtyDebounces 验证连续提交只执行最后一次后台投影。
func TestRawRecordProjector_MarkDirtyDebounces(t *testing.T) {
	service, db := newEventServiceForTest(t,
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
	)
	if _, err := service.PersistFinal(context.Background(), FinalInput{MeetingID: testMeetingID, ASRSessionID: testSessionID, ProviderResultID: "provider-final-1", Text: "防抖转写", Range: mustSampleRange(t, 0, 16000), LastSentSample: 64000}); err != nil {
		t.Fatalf("写入 final 失败：%v", err)
	}
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "meetings", "test"), 0o700); err != nil {
		t.Fatalf("创建会议目录失败：%v", err)
	}
	var tasks []func()
	var canceled []bool
	projector := NewRawRecordProjector(RawRecordProjectorDependencies{
		Repository: transcriptrepository.NewRepository(db), WorkspaceRoot: workspace, Debounce: 2 * time.Second,
		Schedule: func(_ time.Duration, task func()) func() {
			index := len(tasks)
			tasks = append(tasks, task)
			canceled = append(canceled, false)
			return func() { canceled[index] = true }
		},
	})
	if err := projector.MarkDirty(testMeetingID); err != nil {
		t.Fatalf("首次标记 dirty 失败：%v", err)
	}
	if err := projector.MarkDirty(testMeetingID); err != nil {
		t.Fatalf("第二次标记 dirty 失败：%v", err)
	}
	if len(tasks) != 2 || !canceled[0] || canceled[1] {
		t.Fatalf("防抖取消状态错误：tasks=%d canceled=%v", len(tasks), canceled)
	}
	tasks[0]()
	if _, err := os.Stat(filepath.Join(workspace, "meetings", "test", "会议原始记录.md")); !os.IsNotExist(err) {
		t.Fatalf("已取消的旧任务不得覆盖新一代调度：%v", err)
	}
	tasks[1]()
	if state := projector.State(testMeetingID); state.State != "current" || state.ErrorCode != "" {
		t.Fatalf("刷新成功状态错误：%+v", state)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "meetings", "test", "会议原始记录.md"))
	if err != nil || len(content) == 0 {
		t.Fatalf("防抖任务未生成原始记录：bytes=%d err=%v", len(content), err)
	}
}
