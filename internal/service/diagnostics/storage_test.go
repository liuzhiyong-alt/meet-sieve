package diagnostics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/database"
)

// TestStorageScanUsesRealBytesWithoutFollowingSymlink 验证分类、Top 会议和符号链接边界。
func TestStorageScanUsesRealBytesWithoutFollowingSymlink(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(databasePath); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(db)
	if err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state,
		created_at, updated_at
	) VALUES ('11111111-1111-4111-8111-111111111111', 'M-01', '存储测试', 'meetings/M-01',
		'Asia/Shanghai', 1, 2, 'ended', 'saved', 'stopped', 'none', 'available', 'confirmed', 'stopped', 1, 2)`).Error; err != nil {
		t.Fatal(err)
	}
	writeStorageFile(t, filepath.Join(root, "meetings", "M-01", "audio", "recording.wav"), []byte("12345"))
	writeStorageFile(t, filepath.Join(root, "meetings", "M-01", "resources", "note.txt"), []byte("123"))
	external := t.TempDir()
	writeStorageFile(t, filepath.Join(external, "large.bin"), make([]byte, 4096))
	if err := os.Symlink(external, filepath.Join(root, "meetings", "M-01", "external")); err != nil {
		t.Fatal(err)
	}
	service := NewStorageScanService(db, root, "", "")
	if _, err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var snapshot StorageSnapshot
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		snapshot = service.Get()
		if !snapshot.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snapshot.Stage != StageCompleted {
		t.Fatalf("扫描未完成：%+v", snapshot)
	}
	if snapshot.Categories.Recordings != 5 || snapshot.Categories.Attachments != 3 {
		t.Fatalf("分类字节错误：%+v", snapshot.Categories)
	}
	if len(snapshot.TopMeetings) != 1 || snapshot.TopMeetings[0].Bytes != 8 {
		t.Fatalf("会议占用不得包含符号链接目标：%+v", snapshot.TopMeetings)
	}
	if len(snapshot.Warnings) == 0 {
		t.Fatal("符号链接必须产生 warning")
	}
}

// writeStorageFile 创建存储扫描测试文件。
func writeStorageFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
