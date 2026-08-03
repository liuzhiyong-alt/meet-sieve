package middleware

import (
	"strings"

	"meet-sieve/internal/infra/apperr"
	infraLogger "meet-sieve/internal/infra/logger"
	"meet-sieve/internal/transport/http/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ErrorHandler 在 Gin 错误链返回后统一归一化、记录并输出安全响应。
func ErrorHandler(appLogger *infraLogger.AppLogger) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Next()
		if len(ctx.Errors) == 0 {
			return
		}

		appErr := apperr.Normalize(ctx.Errors.Last().Err)
		appLogger.LogError(
			"HTTP 请求失败",
			RequestIDFrom(ctx),
			appErr,
			zap.String("component", "http"),
			zap.String("method", ctx.Request.Method),
			zap.String("path", routePath(ctx)),
		)
		if ctx.Writer.Written() {
			// 已经输出响应时只记录错误，避免追加 JSON 破坏原响应。
			ctx.Abort()
			return
		}
		if strings.HasPrefix(ctx.Request.URL.Path, "/api/v1/guest") {
			response.GuestFailure(ctx, RequestIDFrom(ctx), appErr)
		} else {
			response.Failure(ctx, RequestIDFrom(ctx), appErr)
		}
		ctx.Abort()
	}
}

// AbortWithError 将错误放入 Gin 错误链并终止后续 handler。
func AbortWithError(ctx *gin.Context, err error) {
	_ = ctx.Error(err)
	ctx.Abort()
}
