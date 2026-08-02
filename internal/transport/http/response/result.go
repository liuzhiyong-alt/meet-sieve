// Package response 定义 Gin transport 的统一响应结构。
package response

import (
	"meet-sieve/internal/infra/apperr"

	"github.com/gin-gonic/gin"
)

// Result 是 Gin transport 返回的统一响应结构。
type Result[T any] struct {
	// Code 与 HTTP status 保持一致。
	Code int `json:"code"`
	// Message 是可安全展示的用户提示。
	Message string `json:"message"`
	// Data 是成功响应的可选业务数据。
	Data *T `json:"data,omitempty"`
	// RequestID 用于关联响应、访问日志和错误日志。
	RequestID string `json:"request_id"`
}

// Success 输出成功响应，HTTP status 与 body code 保持一致。
func Success[T any](ctx *gin.Context, requestID string, data T) {
	ctx.JSON(apperr.CodeOK.Value, Result[T]{
		Code:      apperr.CodeOK.Value,
		Message:   apperr.CodeOK.Message,
		Data:      &data,
		RequestID: requestID,
	})
}

// Failure 输出安全失败响应，内部 cause 和 stack 不进入 JSON。
func Failure(ctx *gin.Context, requestID string, appErr *apperr.AppError) {
	if appErr == nil {
		appErr = apperr.Sys(nil, apperr.WithOp("http.response.nil_error"))
	}
	ctx.JSON(appErr.Code, Result[struct{}]{
		Code:      appErr.Code,
		Message:   appErr.Message,
		RequestID: requestID,
	})
}
