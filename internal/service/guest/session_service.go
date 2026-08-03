// Package guest 编排 LAN 访客会话、消息和可见事件。
package guest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	guestdomain "meet-sieve/internal/domain/guest"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/lan"
	"meet-sieve/models"
)

const (
	// SessionTokenBytes 是访客 Cookie token 的 256-bit 字节数。
	SessionTokenBytes = 32
	sessionLifetime   = 24 * time.Hour
	lastSeenInterval  = 30 * time.Second
	attachmentLimit   = int64(500 * 1024 * 1024)
)

// SessionRepository 是 session 服务所需的最小持久化边界。
type SessionRepository interface {
	CreateSession(context.Context, models.GuestSession) error
	GetMeeting(context.Context, string) (models.Meeting, error)
	FindActiveByTokenHash(context.Context, string) (*models.GuestSession, error)
	MarkExpired(context.Context, string, int64) error
	TouchLastSeen(context.Context, string, int64, int64) error
}

// AccessResolver 验证会议入口令牌与 session 创建时的 LAN generation。
type AccessResolver interface {
	ResolveMeetingToken(string) (lan.MeetingAccess, bool)
	IsMeetingServing(string, string) bool
}

// SessionDependencies 是 SessionService 的显式依赖。
type SessionDependencies struct {
	Repository SessionRepository
	Access     AccessResolver
	Clock      clock.Clock
	IDs        identity.Generator
	Random     io.Reader
}

// SessionService 生成、哈希并验证临时访客会话。
type SessionService struct {
	repository SessionRepository
	access     AccessResolver
	clock      clock.Clock
	ids        identity.Generator
	random     io.Reader
	randomMu   sync.Mutex
	mu         sync.RWMutex
	generation map[string]string
}

// CreateSessionInput 是 Guest Web 交换会议令牌的一次性请求。
type CreateSessionInput struct {
	MeetingToken string `json:"meeting_token"`
	DisplayName  string `json:"display_name"`
}

// MeetingProjection 只包含访客可见的会议信息。
type MeetingProjection struct {
	ID                 string `json:"id"`
	Subject            string `json:"subject"`
	StartedAt          int64  `json:"started_at"`
	LANState           string `json:"lan_state"`
	AttachmentMaxBytes int64  `json:"attachment_max_bytes"`
	RelativeDir        string `json:"-"`
}

// CreatedSession 包含仅能交给 HTTP Cookie 的一次性原始 token。
type CreatedSession struct {
	Session models.GuestSession
	Meeting MeetingProjection
	Token   string
}

// AuthenticatedSession 是通过 Cookie、过期时间和 generation 联合校验的访客事实。
type AuthenticatedSession struct {
	Session    models.GuestSession
	Generation string
}

// NewSessionService 创建 session 服务，原始 token 不进入持久化依赖。
func NewSessionService(dependencies SessionDependencies) *SessionService {
	if dependencies.Random == nil {
		dependencies.Random = rand.Reader
	}
	return &SessionService{
		repository: dependencies.Repository, access: dependencies.Access, clock: dependencies.Clock,
		ids: dependencies.IDs, random: dependencies.Random, generation: make(map[string]string),
	}
}

// Create 校验入口令牌，并且只把 256-bit session token 的 SHA-256 写入 SQLite。
func (service *SessionService) Create(ctx context.Context, input CreateSessionInput) (CreatedSession, error) {
	if service == nil || service.repository == nil || service.access == nil || service.clock == nil || service.ids == nil {
		return CreatedSession{}, fmt.Errorf("访客会话服务不可用")
	}
	access, ok := service.access.ResolveMeetingToken(input.MeetingToken)
	if !ok {
		return CreatedSession{}, apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("guest.session.resolve_meeting_token"))
	}
	displayName, err := guestdomain.NormalizeDisplayName(input.DisplayName)
	if err != nil {
		return CreatedSession{}, err
	}
	meeting, err := service.repository.GetMeeting(ctx, access.MeetingID)
	if err != nil {
		return CreatedSession{}, apperr.Dependency(apperr.CodeLANStartFailed, err, apperr.WithOp("guest.session.get_meeting"))
	}
	token, tokenHash, err := service.newSessionToken()
	if err != nil {
		return CreatedSession{}, apperr.Sys(err, apperr.WithOp("guest.session.generate_token"))
	}
	now := service.clock.Now()
	session := models.GuestSession{
		ID: service.ids.New(), MeetingID: access.MeetingID, DisplayName: displayName,
		SessionTokenHash: tokenHash, State: "active", ExpiresAt: now.Add(sessionLifetime).UnixMilli(),
		CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
	}
	if err := service.repository.CreateSession(ctx, session); err != nil {
		return CreatedSession{}, apperr.Sys(err, apperr.WithOp("guest.session.create"))
	}
	service.mu.Lock()
	service.generation[session.ID] = access.Generation
	service.mu.Unlock()
	return CreatedSession{Session: session, Meeting: buildMeetingProjection(meeting, access.Generation), Token: token}, nil
}

// Authenticate 校验 Cookie token hash、有效期与 LAN generation，并节流更新 last_seen_at。
func (service *SessionService) Authenticate(ctx context.Context, token string) (AuthenticatedSession, error) {
	if service == nil || service.repository == nil || service.access == nil || service.clock == nil || token == "" {
		return AuthenticatedSession{}, invalidSession()
	}
	tokenHash := hashToken(token)
	session, err := service.repository.FindActiveByTokenHash(ctx, tokenHash)
	if err != nil {
		return AuthenticatedSession{}, apperr.Sys(err, apperr.WithOp("guest.session.find"))
	}
	if session == nil || subtle.ConstantTimeCompare([]byte(session.SessionTokenHash), []byte(tokenHash)) != 1 {
		return AuthenticatedSession{}, invalidSession()
	}
	now := service.clock.Now().UnixMilli()
	if session.ExpiresAt <= now {
		_ = service.repository.MarkExpired(ctx, session.ID, now)
		service.deleteGeneration(session.ID)
		return AuthenticatedSession{}, apperr.Biz(apperr.CodeLANSessionExpired, apperr.WithOp("guest.session.expired"))
	}
	generationID, ok := service.sessionGeneration(session.ID)
	if !ok || !service.access.IsMeetingServing(session.MeetingID, generationID) {
		return AuthenticatedSession{}, apperr.Biz(apperr.CodeLANGenerationChanged, apperr.WithOp("guest.session.generation"))
	}
	threshold := now - lastSeenInterval.Milliseconds()
	if err := service.repository.TouchLastSeen(ctx, session.ID, now, threshold); err != nil {
		return AuthenticatedSession{}, apperr.Sys(err, apperr.WithOp("guest.session.touch"))
	}
	return AuthenticatedSession{Session: *session, Generation: generationID}, nil
}

// Meeting 返回当前已认证 session 可见的会议安全投影。
func (service *SessionService) Meeting(ctx context.Context, authenticated AuthenticatedSession) (MeetingProjection, error) {
	if service == nil || service.repository == nil || service.access == nil ||
		!service.access.IsMeetingServing(authenticated.Session.MeetingID, authenticated.Generation) {
		return MeetingProjection{}, apperr.Biz(apperr.CodeLANGenerationChanged, apperr.WithOp("guest.session.meeting_generation"))
	}
	meeting, err := service.repository.GetMeeting(ctx, authenticated.Session.MeetingID)
	if err != nil {
		return MeetingProjection{}, apperr.Sys(err, apperr.WithOp("guest.session.meeting"))
	}
	return buildMeetingProjection(meeting, authenticated.Generation), nil
}

// newSessionToken 串行读取可替换随机源，返回原始 token 和十六进制 SHA-256。
func (service *SessionService) newSessionToken() (string, string, error) {
	bytes := make([]byte, SessionTokenBytes)
	service.randomMu.Lock()
	_, err := io.ReadFull(service.random, bytes)
	service.randomMu.Unlock()
	if err != nil {
		return "", "", fmt.Errorf("读取 session 随机数：%w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	return token, hashToken(token), nil
}

// hashToken 返回只能进入 SQLite 的小写十六进制 SHA-256。
func hashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// buildMeetingProjection 移除工作目录、成员、录音和 ASR 等宿主事实。
func buildMeetingProjection(meeting models.Meeting, publicID string) MeetingProjection {
	startedAt := int64(0)
	if meeting.StartedAt != nil {
		startedAt = *meeting.StartedAt
	}
	return MeetingProjection{
		ID: publicID, Subject: meeting.Subject, StartedAt: startedAt,
		LANState: "serving", AttachmentMaxBytes: attachmentLimit,
	}
}

// sessionGeneration 返回当前进程中 session 创建时的 generation。
func (service *SessionService) sessionGeneration(sessionID string) (string, bool) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	generationID, ok := service.generation[sessionID]
	return generationID, ok
}

// deleteGeneration 清理已失效 session 的进程内 generation 关联。
func (service *SessionService) deleteGeneration(sessionID string) {
	service.mu.Lock()
	delete(service.generation, sessionID)
	service.mu.Unlock()
}

// invalidSession 统一返回不允许枚举 token 的认证失败。
func invalidSession() error {
	return apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("guest.session.authenticate"))
}
