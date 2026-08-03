package guest_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	guestrepository "meet-sieve/internal/repository/guest"
	guestservice "meet-sieve/internal/service/guest"
	"meet-sieve/internal/service/lan"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	testMeetingID = "11111111-1111-4111-8111-111111111111"
	testSessionID = "22222222-2222-4222-8222-222222222222"
)

// TestSessionService_CreateStoresOnlySHA256 验证 256-bit 原始 session token 只返回一次，SQLite 仅保存 SHA-256。
func TestSessionService_CreateStoresOnlySHA256(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	randomBytes := strings.Repeat("s", guestservice.SessionTokenBytes)
	service := newSessionService(db, now, randomBytes)

	created, err := service.Create(context.Background(), guestservice.CreateSessionInput{
		MeetingToken: "meeting-token", DisplayName: "  王小明 ",
	})
	if err != nil {
		t.Fatalf("创建访客会话失败：%v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(created.Token)
	if err != nil || len(decoded) != guestservice.SessionTokenBytes {
		t.Fatalf("session token 不是 256-bit base64url：len=%d err=%v", len(decoded), err)
	}
	if created.Session.DisplayName != "王小明" || created.Session.ExpiresAt != now.Add(24*time.Hour).UnixMilli() {
		t.Fatalf("会话规范化或有效期不正确：%#v", created.Session)
	}

	var stored models.GuestSession
	if err := db.Where("id = ?", testSessionID).Take(&stored).Error; err != nil {
		t.Fatalf("读取会话失败：%v", err)
	}
	expectedHash := sha256.Sum256([]byte(created.Token))
	if stored.SessionTokenHash != hex.EncodeToString(expectedHash[:]) || strings.Contains(stored.SessionTokenHash, created.Token) {
		t.Fatalf("SQLite 未仅保存 token SHA-256：hash=%q", stored.SessionTokenHash)
	}
	if created.Meeting.Subject != "Step 6 测试会议" || created.Meeting.RelativeDir != "" {
		t.Fatalf("会议安全投影缺失或泄漏路径：%#v", created.Meeting)
	}
}

// TestSessionService_AuthenticateRejectsExpiredOrStoppedMeeting 验证过期和 LAN 已停止时 Cookie 立即失效。
func TestSessionService_AuthenticateRejectsExpiredOrStoppedMeeting(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	access := &fakeAccessResolver{serving: true}
	repository := guestrepository.NewRepository(db, database.NewTransactionManager(db))
	service := guestservice.NewSessionService(guestservice.SessionDependencies{
		Repository: repository,
		Access:     access,
		Clock:      clock.NewFixed(now),
		IDs:        identity.NewFixedGenerator(testSessionID),
		Random:     strings.NewReader(strings.Repeat("s", guestservice.SessionTokenBytes)),
	})
	created, err := service.Create(context.Background(), guestservice.CreateSessionInput{MeetingToken: "meeting-token", DisplayName: "访客"})
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	if _, err := service.Authenticate(context.Background(), created.Token); err != nil {
		t.Fatalf("活动会话验证失败：%v", err)
	}

	access.serving = false
	if _, err := service.Authenticate(context.Background(), created.Token); err == nil {
		t.Fatal("LAN 已停止时会话仍然有效")
	}
	access.serving = true
	if err := db.Model(&models.GuestSession{}).Where("id = ?", testSessionID).
		UpdateColumns(map[string]any{"expires_at": now.Add(-time.Second).UnixMilli(), "updated_at": now.UnixMilli()}).Error; err != nil {
		t.Fatalf("设置过期时间失败：%v", err)
	}
	if _, err := service.Authenticate(context.Background(), created.Token); err == nil {
		t.Fatal("过期会话仍然有效")
	}
	var state string
	if err := db.Model(&models.GuestSession{}).Select("state").Where("id = ?", testSessionID).Scan(&state).Error; err != nil || state != "expired" {
		t.Fatalf("过期会话未持久化 expired：state=%q err=%v", state, err)
	}
}

// TestRepository_RevokeMeetingIsIdempotent 验证 LAN 停止时仅撤销当前会议活动 session。
func TestRepository_RevokeMeetingIsIdempotent(t *testing.T) {
	db := openGuestDatabase(t)
	insertRecordingMeeting(t, db)
	repository := guestrepository.NewRepository(db, database.NewTransactionManager(db))
	if err := repository.CreateSession(context.Background(), models.GuestSession{
		ID: testSessionID, MeetingID: testMeetingID, DisplayName: "访客",
		SessionTokenHash: strings.Repeat("a", 64), State: "active", ExpiresAt: 1000, CreatedAt: 0, UpdatedAt: 0,
	}); err != nil {
		t.Fatalf("写入会话失败：%v", err)
	}
	if err := repository.RevokeMeeting(context.Background(), testMeetingID); err != nil {
		t.Fatalf("撤销会话失败：%v", err)
	}
	if err := repository.RevokeMeeting(context.Background(), testMeetingID); err != nil {
		t.Fatalf("幂等撤销会话失败：%v", err)
	}
	var state string
	if err := db.Model(&models.GuestSession{}).Select("state").Where("id = ?", testSessionID).Scan(&state).Error; err != nil || state != "revoked" {
		t.Fatalf("会话未被撤销：state=%q err=%v", state, err)
	}
}

// newSessionService 创建使用真实 SQLite 和固定边界的会话服务。
func newSessionService(db *gorm.DB, now time.Time, randomBytes string) *guestservice.SessionService {
	return guestservice.NewSessionService(guestservice.SessionDependencies{
		Repository: guestrepository.NewRepository(db, database.NewTransactionManager(db)),
		Access:     &fakeAccessResolver{serving: true},
		Clock:      clock.NewFixed(now),
		IDs:        identity.NewFixedGenerator(testSessionID),
		Random:     strings.NewReader(randomBytes),
	})
}

type fakeAccessResolver struct{ serving bool }

func (resolver *fakeAccessResolver) ResolveMeetingToken(token string) (lan.MeetingAccess, bool) {
	if token != "meeting-token" || !resolver.serving {
		return lan.MeetingAccess{}, false
	}
	return lan.MeetingAccess{MeetingID: testMeetingID, Generation: "generation-1"}, true
}

func (resolver *fakeAccessResolver) IsMeetingServing(meetingID string, generation string) bool {
	return resolver.serving && meetingID == testMeetingID && generation == "generation-1"
}

// openGuestDatabase 创建执行最新 migration 的真实 SQLite 测试库。
func openGuestDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "guest.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}

// insertRecordingMeeting 写入允许 LAN 访问的最小会议事实。
func insertRecordingMeeting(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state,
		agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (?, 'MS-20260802-0001', 'Step 6 测试会议', 'meetings/private-path', 'Asia/Shanghai', 1000,
		'recording', 'saving', 'streaming', 'none', 'unchecked', 'not_generated', 'serving', 0, 0)`, testMeetingID).Error
	if err != nil {
		t.Fatalf("写入会议失败：%v", err)
	}
}
