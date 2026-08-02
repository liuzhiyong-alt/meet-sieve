// Package middleware 提供 Gin transport 的基础中间件。
package middleware

import (
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	requestIDContextKey = "request_id"
	requestIDHeader     = "X-Request-ID"
)

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID 生成或贯穿受控格式的请求标识。
func RequestID() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		requestID := ctx.GetHeader(requestIDHeader)
		if !validRequestID.MatchString(requestID) {
			requestID = uuid.NewString()
		}
		ctx.Set(requestIDContextKey, requestID)
		ctx.Header(requestIDHeader, requestID)
		ctx.Next()
	}
}

// RequestIDFrom 从 Gin Context 读取当前请求标识。
func RequestIDFrom(ctx *gin.Context) string {
	if ctx == nil {
		return ""
	}
	return ctx.GetString(requestIDContextKey)
}
