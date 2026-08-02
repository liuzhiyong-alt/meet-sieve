package middleware

import (
	"time"

	infraLogger "meet-sieve/internal/infra/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AccessLog 在完整响应生成后记录不含正文和查询参数的请求摘要。
func AccessLog(appLogger *infraLogger.AppLogger) gin.HandlerFunc {
	accessLogger := appLogger.Component("http.access")
	return func(ctx *gin.Context) {
		startedAt := time.Now()
		ctx.Next()
		accessLogger.Info("HTTP 访问",
			zap.String("request_id", RequestIDFrom(ctx)),
			zap.String("method", ctx.Request.Method),
			zap.String("path", routePath(ctx)),
			zap.Int("status", ctx.Writer.Status()),
			zap.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
	}
}

// routePath 优先返回路由模板，避免把用户输入的动态路径写入日志。
func routePath(ctx *gin.Context) string {
	if path := ctx.FullPath(); path != "" {
		return path
	}
	return ""
}
