package resourceopen

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/database"

	"gorm.io/gorm"
)

type launcherStub struct{ open, reveal, link int }

func (launcher *launcherStub) Open(context.Context, string) error    { launcher.open++; return nil }
func (launcher *launcherStub) Reveal(context.Context, string) error  { launcher.reveal++; return nil }
func (launcher *launcherStub) OpenURL(context.Context, string) error { launcher.link++; return nil }

// TestOpenAttachmentVerifiesAndPersistsState 验证只有完整性通过才调用系统 launcher。
func TestOpenAttachmentVerifiesAndPersistsState(t *testing.T) {
	root, db := openResourceDatabase(t)
	path := filepath.Join(root, "meetings", "M-01", "resources", "note.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("attachment")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	insertResourceFacts(t, db, int64(len(content)), hex.EncodeToString(digest[:]))
	launcher := &launcherStub{}
	service := NewService(db, database.NewTransactionManager(db), root, launcher)
	result, err := service.OpenAttachment(context.Background(), "33333333-3333-4333-8333-333333333333")
	if err != nil || !result.Opened || launcher.open != 1 {
		t.Fatalf("打开附件失败：result=%+v launcher=%+v err=%v", result, launcher, err)
	}
	var state string
	_ = db.Raw("SELECT integrity_state FROM resources WHERE id = ?", result.ResourceID).Scan(&state).Error
	if state != "verified" {
		t.Fatalf("完整性状态未持久化：%s", state)
	}
	if err := os.WriteFile(path, []byte("changed!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenAttachment(context.Background(), result.ResourceID); err == nil {
		t.Fatal("内容变化后必须阻止打开")
	}
	if launcher.open != 1 {
		t.Fatal("校验失败不得调用 launcher")
	}
	_ = db.Raw("SELECT integrity_state FROM resources WHERE id = ?", result.ResourceID).Scan(&state).Error
	if state != "changed" {
		t.Fatalf("变化状态未持久化：%s", state)
	}
}

// openResourceDatabase 创建最新 SQLite 资源测试库。
func openResourceDatabase(t *testing.T) (string, *gorm.DB) {
	t.Helper()
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
	t.Cleanup(func() { _ = database.Close(db) })
	return root, db
}

// insertResourceFacts 写入会议、事件和 completed 附件事实。
func insertResourceFacts(t *testing.T, db *gorm.DB, size int64, digest string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state,
		created_at, updated_at
	) VALUES ('11111111-1111-4111-8111-111111111111', 'M-01', '资源测试', 'meetings/M-01',
		'Asia/Shanghai', 1, 2, 'ended', 'saved', 'stopped', 'none', 'available', 'confirmed', 'stopped', 1, 2)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO meeting_events (id, meeting_id, seq, kind, occurred_at, source, created_at, updated_at)
		VALUES ('22222222-2222-4222-8222-222222222222', '11111111-1111-4111-8111-111111111111', 1, 'resource.created', 1, 'guest', 1, 1)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO resources (
		id, meeting_id, event_id, kind, original_name, safe_name, relative_path, size_bytes, sha256, state, created_at, updated_at
	) VALUES ('33333333-3333-4333-8333-333333333333', '11111111-1111-4111-8111-111111111111',
		'22222222-2222-4222-8222-222222222222', 'attachment', 'note.txt', 'note.txt', 'resources/note.txt', ?, ?, 'completed', 1, 2)`, size, digest).Error; err != nil {
		t.Fatal(err)
	}
}
