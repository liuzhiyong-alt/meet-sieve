package minutes

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"
	minutesrepository "meet-sieve/internal/repository/minutes"
	"meet-sieve/models"
)

// TestMinuteProjector_RebuildsCurrentFromSQLite 验证文件只由 SQLite current 重建。
func TestMinuteProjector_RebuildsCurrentFromSQLite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "projector.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	meeting := models.Meeting{ID: "71717171-7171-4717-8717-717171717171", RelativeDir: "meetings/test"}
	if err := os.MkdirAll(filepath.Join(root, "meetings", "test"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO meetings (id, meeting_no, subject, relative_dir, local_timezone, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES (?, 'M1', '测试', ?, 'Asia/Shanghai', 'ended', 'saved', 'stopped', 'none', 'unchecked', 'draft', 'stopped', 0, 0)`, meeting.ID, meeting.RelativeDir).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.MinuteVersion{ID: "72727272-7272-4727-8727-727272727272", MeetingID: meeting.ID, VersionNo: 1, Source: "human", ContentMarkdown: "# 正文\n", State: "draft", IsCurrent: true, CreatedAt: 1, UpdatedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
	projector := NewMinuteProjector(minutesrepository.NewRepository(db, database.NewTransactionManager(db)), root)
	if err := projector.Flush(context.Background(), meeting); err != nil {
		t.Fatalf("投影失败：%v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "meetings", "test", "会议纪要草稿.md"))
	if err != nil || string(content) == "# 正文\n" {
		t.Fatalf("投影未包含版本头：content=%q err=%v", content, err)
	}
}
