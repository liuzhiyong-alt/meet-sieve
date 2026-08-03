package resource_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	contentrepository "meet-sieve/internal/repository/content"
	guestservice "meet-sieve/internal/service/guest"
	resourceservice "meet-sieve/internal/service/resource"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	meetingID = "11111111-1111-4111-8111-111111111111"
	sessionID = "22222222-2222-4222-8222-222222222222"
)

// TestAttachmentService_StreamsRenamesAndCommitsResourceEvent 验证文件先原子落盘，再与 event 一起提交 Resource。
func TestAttachmentService_StreamsRenamesAndCommitsResourceEvent(t *testing.T) {
	db, meetingDirectory := openResourceDatabase(t)
	session := insertResourceFixtures(t, db)
	service, _ := newAttachmentService(db, meetingDirectory)
	content := []byte("%PDF-1.7\nStep 6 attachment")
	input := resourceservice.AttachmentInput{
		RequestID: "33333333-3333-4333-8333-333333333333", OriginalName: "设计稿.pdf",
		DeclaredSize: int64(len(content)), DeclaredMediaType: "application/pdf", Description: "用于评审", Reader: bytes.NewReader(content),
	}

	first, err := service.Upload(context.Background(), session, input)
	if err != nil {
		t.Fatalf("上传附件失败：%v", err)
	}
	input.Reader = bytes.NewReader(content)
	second, err := service.Upload(context.Background(), session, input)
	if err != nil {
		t.Fatalf("幂等重试附件失败：%v", err)
	}
	if first.ResourceID != second.ResourceID || first.Seq != 1 || second.Seq != 1 || first.SHA256 == "" {
		t.Fatalf("附件幂等结果不正确：first=%#v second=%#v", first, second)
	}
	assertResourceCount(t, db, "resources", 1)
	assertResourceCount(t, db, "meeting_events", 1)

	var stored models.Resource
	if err := db.Where("id = ?", first.ResourceID).Take(&stored).Error; err != nil {
		t.Fatalf("读取 Resource 失败：%v", err)
	}
	if stored.RelativePath == nil || filepath.IsAbs(*stored.RelativePath) || strings.Contains(*stored.RelativePath, "设计稿") {
		t.Fatalf("Resource 未使用内部相对路径：%#v", stored.RelativePath)
	}
	finalPath := filepath.Join(meetingDirectory, filepath.FromSlash(*stored.RelativePath))
	written, err := os.ReadFile(finalPath)
	if err != nil || !bytes.Equal(written, content) {
		t.Fatalf("最终附件内容不正确：err=%v", err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		t.Fatalf("读取最终附件权限失败：%v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("最终附件权限不是 0600：mode=%v", info.Mode())
	}
	assertStagingEmpty(t, meetingDirectory)
}

// TestAttachmentService_BlockedMagicLeavesNoFormalFact 验证危险 magic 失败时删除 part 且不占 seq。
func TestAttachmentService_BlockedMagicLeavesNoFormalFact(t *testing.T) {
	db, meetingDirectory := openResourceDatabase(t)
	session := insertResourceFixtures(t, db)
	service, _ := newAttachmentService(db, meetingDirectory)
	content := append([]byte("MZ"), make([]byte, 32)...)

	_, err := service.Upload(context.Background(), session, resourceservice.AttachmentInput{
		RequestID: "33333333-3333-4333-8333-333333333333", OriginalName: "notes.txt",
		DeclaredSize: int64(len(content)), DeclaredMediaType: "application/octet-stream", Reader: bytes.NewReader(content),
	})
	if err == nil {
		t.Fatal("伪装为文本的 PE 附件必须失败")
	}
	assertResourceCount(t, db, "resources", 0)
	assertResourceCount(t, db, "meeting_events", 0)
	assertStagingEmpty(t, meetingDirectory)
}

// TestAttachmentService_MeetingCancelRemovesPart 验证会议取消在流式复制边界生效且不留 part。
func TestAttachmentService_MeetingCancelRemovesPart(t *testing.T) {
	db, meetingDirectory := openResourceDatabase(t)
	session := insertResourceFixtures(t, db)
	service, coordinator := newAttachmentService(db, meetingDirectory)
	reader := &cancelingReader{coordinator: coordinator, meetingID: meetingID, content: []byte("ordinary text")}

	if _, err := service.Upload(context.Background(), session, resourceservice.AttachmentInput{
		RequestID: "33333333-3333-4333-8333-333333333333", OriginalName: "notes.txt",
		DeclaredSize: int64(len(reader.content)), DeclaredMediaType: "text/plain", Reader: reader,
	}); err == nil {
		t.Fatal("会议取消后上传未失败")
	}
	assertResourceCount(t, db, "resources", 0)
	assertStagingEmpty(t, meetingDirectory)
}

type fixedDirectoryResolver struct{ path string }

func (resolver fixedDirectoryResolver) ResolveMeetingDirectory(_ context.Context, id string) (string, error) {
	if id != meetingID {
		return "", os.ErrNotExist
	}
	return resolver.path, nil
}

// newAttachmentService 创建真实文件系统和 SQLite 事务组合的附件服务。
func newAttachmentService(db *gorm.DB, meetingDirectory string) (*resourceservice.AttachmentService, *resourceservice.UploadCoordinator) {
	coordinator := resourceservice.NewUploadCoordinator()
	return resourceservice.NewAttachmentService(resourceservice.AttachmentDependencies{
		Repository: contentrepository.NewRepository(db), Transactions: database.NewTransactionManager(db),
		Coordinator: coordinator, Policy: resourceservice.NewFilePolicy(), Directories: fixedDirectoryResolver{path: meetingDirectory},
		Clock: clock.NewFixed(time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)),
		IDs: identity.NewFixedGenerator(
			"44444444-4444-4444-8444-444444444441", "55555555-5555-4555-8555-555555555551", "66666666-6666-4666-8666-666666666661",
			"44444444-4444-4444-8444-444444444442", "55555555-5555-4555-8555-555555555552", "66666666-6666-4666-8666-666666666662",
		),
		AvailableBytes: func(string) (uint64, error) { return 2 << 30, nil }, MinimumFreeBytes: 1 << 30,
	}), coordinator
}

// openResourceDatabase 创建最新 schema 和独立会议目录。
func openResourceDatabase(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "resource.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	meetingDirectory := filepath.Join(root, "meeting")
	if err := os.MkdirAll(meetingDirectory, 0o700); err != nil {
		t.Fatalf("创建会议目录失败：%v", err)
	}
	return db, meetingDirectory
}

// insertResourceFixtures 写入录音中会议和活动 Guest session。
func insertResourceFixtures(t *testing.T, db *gorm.DB) guestservice.AuthenticatedSession {
	t.Helper()
	err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state,
		agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (?, 'MS-20260802-0001', 'Attachment', 'meetings/attachment', 'Asia/Shanghai', 1000,
		'recording', 'saving', 'streaming', 'none', 'unchecked', 'not_generated', 'serving', 0, 0)`, meetingID).Error
	if err != nil {
		t.Fatalf("写入会议失败：%v", err)
	}
	session := models.GuestSession{
		ID: sessionID, MeetingID: meetingID, DisplayName: "访客", SessionTokenHash: strings.Repeat("a", 64),
		State: "active", ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), CreatedAt: 0, UpdatedAt: 0,
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("写入 Guest session 失败：%v", err)
	}
	return guestservice.AuthenticatedSession{Session: session, Generation: "generation-1"}
}

// assertResourceCount 断言固定业务表行数。
func assertResourceCount(t *testing.T, db *gorm.DB, table string, expected int64) {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil || count != expected {
		t.Fatalf("%s 行数=%d want=%d err=%v", table, count, expected, err)
	}
}

// assertStagingEmpty 验证失败、取消和幂等重试不留 `.part`。
func assertStagingEmpty(t *testing.T, meetingDirectory string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(meetingDirectory, "resources", ".staging", "*.part"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("暂存目录遗留 part：matches=%v err=%v", matches, err)
	}
}

type cancelingReader struct {
	coordinator *resourceservice.UploadCoordinator
	meetingID   string
	content     []byte
	done        bool
}

func (reader *cancelingReader) Read(destination []byte) (int, error) {
	if reader.done {
		return 0, os.ErrClosed
	}
	reader.done = true
	reader.coordinator.CancelMeeting(reader.meetingID)
	return copy(destination, reader.content), nil
}
