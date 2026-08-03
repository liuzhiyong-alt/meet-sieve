package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	guestrepository "meet-sieve/internal/repository/guest"
	guestservice "meet-sieve/internal/service/guest"
	"meet-sieve/internal/service/lan"
	transporthttp "meet-sieve/internal/transport/http"
	guesthttp "meet-sieve/internal/transport/http/guest"

	"gorm.io/gorm"
)

const guestHTTPMeetingID = "11111111-1111-4111-8111-111111111111"

// TestGuestSessionRoute_SetsStrictHttpOnlyCookieAndSafeEnvelope 验证会议 token 只交换为 HttpOnly Cookie，响应不泄漏 hash。
func TestGuestSessionRoute_SetsStrictHttpOnlyCookieAndSafeEnvelope(t *testing.T) {
	engine := newGuestRouteEngine(t)
	request := httptest.NewRequest(http.MethodPost, "http://192.168.1.20:43125/api/v1/guest/sessions",
		strings.NewReader(`{"meeting_token":"meeting-token","display_name":"访客"}`))
	request.Host = "192.168.1.20:43125"
	request.RemoteAddr = "192.168.1.30:51000"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://192.168.1.20:43125")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("创建 Guest session 失败：status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" || cookies[0].Secure {
		t.Fatalf("Guest Cookie 安全属性不正确：%#v", cookies)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析 Guest envelope 失败：%v", err)
	}
	if payload["success"] != true || payload["code"] != "OK" || strings.Contains(recorder.Body.String(), "session_token_hash") || strings.Contains(recorder.Body.String(), cookies[0].Value) {
		t.Fatalf("Guest session 响应契约不安全：%s", recorder.Body.String())
	}
	for _, header := range []string{"Cache-Control", "X-Content-Type-Options", "Content-Security-Policy"} {
		if recorder.Header().Get(header) == "" {
			t.Fatalf("Guest 响应缺少安全头 %s", header)
		}
	}
}

// TestGuestRoutes_RejectMismatchedHostAndOriginWithGuestEnvelope 验证 Host/Origin 不同源时使用 Guest 字符串错误契约。
func TestGuestRoutes_RejectMismatchedHostAndOriginWithGuestEnvelope(t *testing.T) {
	engine := newGuestRouteEngine(t)
	for _, mutate := range []func(*http.Request){
		func(request *http.Request) { request.Host = "evil.example:43125" },
		func(request *http.Request) { request.Header.Set("Origin", "http://evil.example:43125") },
	} {
		request := httptest.NewRequest(http.MethodPost, "http://192.168.1.20:43125/api/v1/guest/sessions",
			strings.NewReader(`{"meeting_token":"meeting-token","display_name":"访客"}`))
		request.Host = "192.168.1.20:43125"
		request.RemoteAddr = "192.168.1.30:51000"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://192.168.1.20:43125")
		mutate(request)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		var payload struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.Success || payload.Code == "" {
			t.Fatalf("非同源失败未使用 Guest envelope：status=%d body=%s err=%v", recorder.Code, recorder.Body.String(), err)
		}
	}
}

// newGuestRouteEngine 创建使用真实 SQLite session repository 的 Guest HTTP Engine。
func newGuestRouteEngine(t *testing.T) http.Handler {
	t.Helper()
	_, appLogger, _ := newTestEngine(t)
	db := openGuestHTTPDatabase(t)
	repository := guestrepository.NewRepository(db, database.NewTransactionManager(db))
	sessions := guestservice.NewSessionService(guestservice.SessionDependencies{
		Repository: repository, Access: guestHTTPAccess{}, Clock: clock.NewFixed(time.Now()),
		IDs: identity.NewFixedGenerator(
			"22222222-2222-4222-8222-222222222221", "22222222-2222-4222-8222-222222222222",
		),
		Random: strings.NewReader(strings.Repeat("s", guestservice.SessionTokenBytes*2)),
	})
	return transporthttp.NewGuestEngine(health.NewRegistry(), appLogger, guesthttp.RouteDependencies{
		Sessions:       sessions,
		ExpectedOrigin: func() string { return "http://192.168.1.20:43125" },
		Generation:     func() string { return "generation-1" },
	})
}

type guestHTTPAccess struct{}

func (guestHTTPAccess) ResolveMeetingToken(token string) (lan.MeetingAccess, bool) {
	if token != "meeting-token" {
		return lan.MeetingAccess{}, false
	}
	return lan.MeetingAccess{MeetingID: guestHTTPMeetingID, Generation: "generation-1"}, true
}

func (guestHTTPAccess) IsMeetingServing(meetingID string, generation string) bool {
	return meetingID == guestHTTPMeetingID && generation == "generation-1"
}

// openGuestHTTPDatabase 创建 Guest route 所需的最新 SQLite schema 和会议事实。
func openGuestHTTPDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "guest-http.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	err = db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone, started_at,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state,
		agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (?, 'MS-20260802-0001', 'Guest HTTP', 'meetings/http', 'Asia/Shanghai', 1000,
		'recording', 'saving', 'streaming', 'none', 'unchecked', 'not_generated', 'serving', 0, 0)`, guestHTTPMeetingID).Error
	if err != nil {
		t.Fatalf("写入 Guest HTTP 会议失败：%v", err)
	}
	return db
}
