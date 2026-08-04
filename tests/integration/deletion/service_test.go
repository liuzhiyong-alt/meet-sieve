package deletion_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	deletionrepository "meet-sieve/internal/repository/deletion"
	queryrepository "meet-sieve/internal/repository/query"
	deletionservice "meet-sieve/internal/service/deletion"
	lifecycleservice "meet-sieve/internal/service/lifecycle"
	queryservice "meet-sieve/internal/service/query"

	"gorm.io/gorm"
)

const testMeetingID = "11111111-1111-4111-8111-111111111111"

// TestDeleteRecordingPreservesFactsAndAttachments 验证录音删除仅清理 audio 子树并保留会议与附件。
func TestDeleteRecordingPreservesFactsAndAttachments(t *testing.T) {
	root, db, service := openDeletionService(t)
	insertMeeting(t, db)
	meetingRoot := filepath.Join(root, "meetings", "M-01")
	writeFile(t, filepath.Join(meetingRoot, "audio", "recording.wav"), []byte("audio"))
	writeFile(t, filepath.Join(meetingRoot, "resources", "note.txt"), []byte("resource"))
	insertAudioAsset(t, db)

	preview, err := service.PreviewRecording(context.Background(), testMeetingID)
	if err != nil || preview.FileCount != 1 || preview.UnknownCount == 0 {
		t.Fatalf("录音预览错误：preview=%+v err=%v", preview, err)
	}
	result, err := service.DeleteRecording(context.Background(), testMeetingID, preview.Revision, preview.Digest)
	if err != nil || result.State != "completed" {
		t.Fatalf("删除录音失败：result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(meetingRoot, "audio", "recording.wav")); !os.IsNotExist(err) {
		t.Fatal("录音文件应被删除")
	}
	if _, err := os.Stat(filepath.Join(meetingRoot, "resources", "note.txt")); err != nil {
		t.Fatalf("附件必须保留：%v", err)
	}
	var meetingCount int64
	_ = db.Table("meetings").Where("id = ?", testMeetingID).Count(&meetingCount).Error
	if meetingCount != 1 {
		t.Fatal("删除录音不得删除会议事实")
	}
	var state string
	_ = db.Raw("SELECT state FROM audio_assets WHERE meeting_id = ?", testMeetingID).Scan(&state).Error
	if state != "deleted" {
		t.Fatalf("音频资产状态错误：%s", state)
	}
	detail, err := queryservice.NewService(queryrepository.NewRepository(db)).GetMeetingDetail(context.Background(), testMeetingID)
	if err != nil || detail.CanPlayAudio || detail.CanRetranscribe {
		t.Fatalf("录音删除后播放和补转写必须禁用：detail=%+v err=%v", detail, err)
	}
}

// TestDeleteMeetingDoesNotFollowExternalSymlink 验证整场删除移除链接本身而不触碰外部目标。
func TestDeleteMeetingDoesNotFollowExternalSymlink(t *testing.T) {
	root, db, service := openDeletionService(t)
	insertMeeting(t, db)
	meetingRoot := filepath.Join(root, "meetings", "M-01")
	writeFile(t, filepath.Join(meetingRoot, "note.txt"), []byte("meeting"))
	external := filepath.Join(t.TempDir(), "external.txt")
	writeFile(t, external, []byte("keep"))
	if err := os.Symlink(external, filepath.Join(meetingRoot, "outside")); err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewMeeting(context.Background(), testMeetingID)
	if err != nil || preview.SymlinkCount != 1 {
		t.Fatalf("整场预览错误：preview=%+v err=%v", preview, err)
	}
	result, err := service.DeleteMeeting(context.Background(), testMeetingID, "M-01", preview.Revision, preview.Digest)
	if err != nil || result.State != "completed" {
		t.Fatalf("删除整场失败：result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("外部符号链接目标不得删除：%v", err)
	}
	if _, err := os.Stat(meetingRoot); !os.IsNotExist(err) {
		t.Fatal("会议目录应完整删除")
	}
	var count int64
	_ = db.Table("meetings").Where("id = ?", testMeetingID).Count(&count).Error
	if count != 0 {
		t.Fatal("全部文件成功后应删除会议事实")
	}
}

// openDeletionService 创建真实迁移库和真实临时工作目录。
func openDeletionService(t *testing.T) (string, *gorm.DB, *deletionservice.Service) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	repository := deletionrepository.NewRepository(db, database.NewTransactionManager(db))
	service := deletionservice.NewService(deletionservice.Dependencies{
		Repository: repository, Maintenance: lifecycleservice.NewCoordinator(nil),
		IDs:   identity.NewFixedGenerator("99999999-9999-4999-8999-999999999999"),
		Clock: clock.NewFixed(time.UnixMilli(2000)), WorkspaceRoot: root,
	})
	return root, db, service
}

// insertMeeting 写入结束会议最小事实。
func insertMeeting(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state,
		created_at, updated_at
	) VALUES (?, 'M-01', '测试会议', 'meetings/M-01', 'Asia/Shanghai', 1, 2,
		'ended', 'saved', 'stopped', 'none', 'available', 'not_generated', 'stopped', 1, 2)`, testMeetingID).Error
	if err != nil {
		t.Fatalf("写入会议失败：%v", err)
	}
}

// insertAudioAsset 写入与真实录音文件对应的资产事实。
func insertAudioAsset(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Exec(`INSERT INTO audio_assets (
		id, meeting_id, kind, sequence_no, relative_path, start_sample, end_sample,
		sample_rate, bit_depth, channels, size_bytes, sha256, state, created_at, updated_at
	) VALUES ('22222222-2222-4222-8222-222222222222', ?, 'mixed', 1,
		'meetings/M-01/audio/recording.wav', 0, 1, 16000, 16, 1, 5,
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'ready', 1, 2)`, testMeetingID).Error
	if err != nil {
		t.Fatalf("写入音频资产失败：%v", err)
	}
}

// writeFile 创建父目录并写入真实测试文件。
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
