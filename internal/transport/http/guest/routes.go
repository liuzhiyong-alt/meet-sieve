package guest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"meet-sieve/internal/infra/apperr"
	guestservice "meet-sieve/internal/service/guest"
	resourceservice "meet-sieve/internal/service/resource"
	httpmiddleware "meet-sieve/internal/transport/http/middleware"

	"github.com/gin-gonic/gin"
)

const (
	sessionCookieName = "meetsieve_guest_session"
	authenticatedKey  = "guest_authenticated_session"
	maxJSONBodyBytes  = 64 * 1024
	multipartOverhead = 1024 * 1024
	uploadTimeout     = 2 * time.Hour
)

// RouteDependencies 是 Guest API handler 需要的最小服务集合。
type RouteDependencies struct {
	Sessions       *guestservice.SessionService
	Content        *guestservice.ContentService
	Timeline       *guestservice.TimelineService
	Attachments    *resourceservice.AttachmentService
	Downloads      *resourceservice.DownloadService
	ExpectedOrigin func() string
	Generation     func() string
	Limiter        *Limiter
	Presence       *Presence
	WebAssets      fs.FS
}

// RegisterRoutes 只在 `/api/v1/guest` 注册访客协议，不改变 `/health` 契约。
func RegisterRoutes(engine *gin.Engine, dependencies RouteDependencies) {
	if engine == nil {
		return
	}
	if dependencies.Limiter == nil {
		dependencies.Limiter = NewLimiter()
	}
	if dependencies.Presence == nil {
		dependencies.Presence = NewPresence()
	}
	group := engine.Group("/api/v1/guest")
	group.Use(securityHeaders(), validateHostAndOrigin(dependencies.ExpectedOrigin))
	group.POST("/sessions", createSessionHandler(dependencies))

	authenticated := group.Group("")
	authenticated.Use(authenticate(dependencies))
	authenticated.GET("/meeting", meetingHandler(dependencies))
	authenticated.GET("/events", eventsHandler(dependencies))
	authenticated.POST("/messages", messageHandler(dependencies))
	authenticated.POST("/attachments", attachmentHandler(dependencies))
	authenticated.GET("/attachments/:id", downloadHandler(dependencies))
	authenticated.HEAD("/attachments/:id", downloadHandler(dependencies))
	registerWebRoutes(engine, dependencies.WebAssets)
}

// securityHeaders 为所有 Guest API 禁止缓存、MIME 猜测和外部引用来源。
func securityHeaders() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		ctx.Header("X-Content-Type-Options", "nosniff")
		ctx.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		ctx.Next()
	}
}

// validateHostAndOrigin 要求 Host 匹配当前 Listener，且写请求 Origin 为空或完全同源。
func validateHostAndOrigin(expectedOrigin func() string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		expected := ""
		if expectedOrigin != nil {
			expected = expectedOrigin()
		}
		parsed, err := url.Parse(expected)
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" || ctx.Request.Host != parsed.Host {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANGenerationChanged, apperr.WithOp("http.guest.host")))
			return
		}
		if isWriteMethod(ctx.Request.Method) {
			origin := ctx.GetHeader("Origin")
			if origin != "" && origin != parsed.Scheme+"://"+parsed.Host {
				httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("http.guest.origin")))
				return
			}
		}
		ctx.Next()
	}
}

// createSessionHandler 使用会议 fragment token 交换 HttpOnly session Cookie。
func createSessionHandler(dependencies RouteDependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if dependencies.Sessions == nil || !allowRequest(dependencies, "session:"+remoteIP(ctx.Request), 5, time.Minute) {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANRateLimited, apperr.WithOp("http.guest.create_session_limit")))
			return
		}
		var input guestservice.CreateSessionInput
		if err := decodeJSON(ctx, &input); err != nil {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("http.guest.create_session_json")))
			return
		}
		created, err := dependencies.Sessions.Create(ctx.Request.Context(), input)
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		setSessionCookie(ctx.Writer, created.Token, created.Session.ExpiresAt)
		Success(ctx, httpmiddleware.RequestIDFrom(ctx), sessionResponse{
			SessionID: created.Session.ID, DisplayName: created.Session.DisplayName,
			ExpiresAt: created.Session.ExpiresAt, Meeting: created.Meeting,
		})
	}
}

// authenticate 联合校验 Cookie、SQLite session 和当前 LAN generation。
func authenticate(dependencies RouteDependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if dependencies.Sessions == nil {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("http.guest.auth_service")))
			return
		}
		token, err := ctx.Cookie(sessionCookieName)
		if err != nil || token == "" {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("http.guest.cookie")))
			return
		}
		authenticated, err := dependencies.Sessions.Authenticate(ctx.Request.Context(), token)
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		ctx.Set(authenticatedKey, authenticated)
		dependencies.Presence.Mark(authenticated.Session.ID, time.Now())
		ctx.Next()
	}
}

// meetingHandler 返回不含工作目录、成员、录音或 ASR 状态的会议投影。
func meetingHandler(dependencies RouteDependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authenticated, ok := authenticatedFrom(ctx)
		if !ok {
			return
		}
		meeting, err := dependencies.Sessions.Meeting(ctx.Request.Context(), authenticated)
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		Success(ctx, httpmiddleware.RequestIDFrom(ctx), meeting)
	}
}

// eventsHandler 按 after_seq 返回白名单事件页。
func eventsHandler(dependencies RouteDependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authenticated, ok := authenticatedFrom(ctx)
		if !ok {
			return
		}
		if !allowRequest(dependencies, "events:"+authenticated.Session.ID, 2, time.Second) {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANRateLimited, apperr.WithOp("http.guest.events_limit")))
			return
		}
		afterSeq, limit, err := parseTimelineQuery(ctx)
		if err != nil || dependencies.Timeline == nil {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("http.guest.events_query")))
			return
		}
		page, err := dependencies.Timeline.List(ctx.Request.Context(), authenticated.Session.MeetingID, afterSeq, limit)
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		Success(ctx, httpmiddleware.RequestIDFrom(ctx), page)
	}
}

// messageHandler 处理显式 text/link 请求，不对正文 URL 做启发式。
func messageHandler(dependencies RouteDependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authenticated, ok := authenticatedFrom(ctx)
		if !ok {
			return
		}
		if !allowRequest(dependencies, "messages:"+authenticated.Session.ID, 30, time.Minute) {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANRateLimited, apperr.WithOp("http.guest.messages_limit")))
			return
		}
		var input guestservice.ContentInput
		if err := decodeJSON(ctx, &input); err != nil || dependencies.Content == nil {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("http.guest.message_json")))
			return
		}
		result, err := dependencies.Content.Create(ctx.Request.Context(), authenticated, input)
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		Success(ctx, httpmiddleware.RequestIDFrom(ctx), result)
	}
}

// attachmentHandler 手工消费单 multipart part，不调用会使用系统临时目录的便捷解析。
func attachmentHandler(dependencies RouteDependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authenticated, ok := authenticatedFrom(ctx)
		if !ok {
			return
		}
		if dependencies.Attachments == nil {
			httpmiddleware.AbortWithError(ctx, apperr.Sys(nil, apperr.WithOp("http.guest.attachment_service")))
			return
		}
		input, err := parseAttachment(ctx)
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		uploadContext, cancel := context.WithTimeout(ctx.Request.Context(), uploadTimeout)
		defer cancel()
		result, err := dependencies.Attachments.Upload(uploadContext, authenticated, input)
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		Success(ctx, httpmiddleware.RequestIDFrom(ctx), result)
	}
}

// downloadHandler 在当前认证会议范围内输出附件 GET/HEAD/单 Range。
func downloadHandler(dependencies RouteDependencies) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		authenticated, ok := authenticatedFrom(ctx)
		if !ok {
			return
		}
		if dependencies.Downloads == nil {
			httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeAttachmentNotFound, apperr.WithOp("http.guest.download_service")))
			return
		}
		opened, err := dependencies.Downloads.Open(ctx.Request.Context(), authenticated, ctx.Param("id"))
		if err != nil {
			httpmiddleware.AbortWithError(ctx, err)
			return
		}
		defer opened.File.Close()
		resourceservice.ServeDownload(ctx.Writer, ctx.Request, opened.File, opened.OriginalName(), opened.MediaType())
	}
}

type sessionResponse struct {
	SessionID   string                         `json:"session_id"`
	DisplayName string                         `json:"display_name"`
	ExpiresAt   int64                          `json:"expires_at"`
	Meeting     guestservice.MeetingProjection `json:"meeting"`
}

// setSessionCookie 设置 HttpOnly、SameSite Strict 且仅会议期间有效的 Cookie。
func setSessionCookie(writer http.ResponseWriter, token string, expiresAt int64) {
	expires := time.UnixMilli(expiresAt)
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(writer, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: false, Expires: expires, MaxAge: maxAge,
	})
}

// decodeJSON 以 64 KiB 硬上限解析单个不含未知字段的 JSON 对象。
func decodeJSON(ctx *gin.Context, destination any) error {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(ctx.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("JSON 只允许一个对象")
	}
	return nil
}

// parseTimelineQuery 解析非负 after_seq 和可选 limit。
func parseTimelineQuery(ctx *gin.Context) (int64, int, error) {
	afterSeq := int64(0)
	limit := 0
	var err error
	if value := ctx.Query("after_seq"); value != "" {
		afterSeq, err = strconv.ParseInt(value, 10, 64)
		if err != nil || afterSeq < 0 {
			return 0, 0, fmt.Errorf("after_seq 无效")
		}
	}
	if value := ctx.Query("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit <= 0 {
			return 0, 0, fmt.Errorf("limit 无效")
		}
	}
	return afterSeq, limit, nil
}

// parseAttachment 校验头部并返回能检测额外 part 的单文件 reader。
func parseAttachment(ctx *gin.Context) (resourceservice.AttachmentInput, error) {
	requestID := ctx.GetHeader("Idempotency-Key")
	declaredSize, err := strconv.ParseInt(ctx.GetHeader("X-File-Size"), 10, 64)
	if err != nil || resourceservice.ValidateDeclaredSize(declaredSize) != nil {
		return resourceservice.AttachmentInput{}, apperr.Biz(apperr.CodeAttachmentTooLarge, apperr.WithOp("http.guest.attachment_size"))
	}
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, resourceservice.MaxAttachmentBytes+multipartOverhead)
	reader, err := ctx.Request.MultipartReader()
	if err != nil {
		return resourceservice.AttachmentInput{}, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("http.guest.multipart"))
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "file" {
		return resourceservice.AttachmentInput{}, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("http.guest.multipart_file"))
	}
	_, parameters, parseErr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	originalName := parameters["filename"]
	if parseErr != nil || originalName == "" {
		_ = part.Close()
		return resourceservice.AttachmentInput{}, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("http.guest.multipart_filename"))
	}
	return resourceservice.AttachmentInput{
		RequestID: requestID, OriginalName: originalName, DeclaredSize: declaredSize,
		DeclaredMediaType: part.Header.Get("Content-Type"), Description: ctx.GetHeader("X-File-Description"),
		Reader: &singlePartReader{part: part, multipart: reader},
	}, nil
}

type singlePartReader struct {
	part        *multipart.Part
	multipart   *multipart.Reader
	terminalErr error
	done        bool
}

// Read 在首个 part 结束时确认 multipart 中没有第二个 part。
func (reader *singlePartReader) Read(destination []byte) (int, error) {
	if reader.terminalErr != nil {
		err := reader.terminalErr
		reader.terminalErr = nil
		return 0, err
	}
	if reader.done {
		return 0, io.EOF
	}
	count, err := reader.part.Read(destination)
	if err != io.EOF {
		return count, err
	}
	reader.done = true
	_ = reader.part.Close()
	next, nextErr := reader.multipart.NextPart()
	if nextErr == nil {
		_ = next.Close()
		reader.terminalErr = fmt.Errorf("multipart 只允许一个 file part")
	} else if nextErr != io.EOF {
		reader.terminalErr = nextErr
	}
	if count > 0 {
		return count, nil
	}
	if reader.terminalErr != nil {
		return reader.Read(destination)
	}
	return 0, io.EOF
}

// authenticatedFrom 读取已由 middleware 联合校验的 session。
func authenticatedFrom(ctx *gin.Context) (guestservice.AuthenticatedSession, bool) {
	value, exists := ctx.Get(authenticatedKey)
	authenticated, ok := value.(guestservice.AuthenticatedSession)
	if !exists || !ok {
		httpmiddleware.AbortWithError(ctx, apperr.Biz(apperr.CodeLANSessionInvalid, apperr.WithOp("http.guest.auth_context")))
		return guestservice.AuthenticatedSession{}, false
	}
	return authenticated, true
}

// allowRequest 把 generation 放入限流键，新代不复用旧会议窗口。
func allowRequest(dependencies RouteDependencies, key string, limit int, window time.Duration) bool {
	generationID := ""
	if dependencies.Generation != nil {
		generationID = dependencies.Generation()
	}
	return dependencies.Limiter.Allow(generationID+":"+key, limit, window, time.Now())
}

// remoteIP 只信任 TCP RemoteAddr，不接受局域网客户端伪造的代理头。
func remoteIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return request.RemoteAddr
}

// isWriteMethod 判断请求是否必须进行 Origin 校验。
func isWriteMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

// ExpectedOriginFromJoinURL 从宿主入口 URL 移除路径和 fragment，用于 Host/Origin 契约。
func ExpectedOriginFromJoinURL(joinURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(joinURL))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
