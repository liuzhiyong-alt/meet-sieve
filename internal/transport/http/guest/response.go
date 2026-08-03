// Package guest 定义只用于局域网访客 API 的 HTTP 协议契约。
package guest

import (
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/transport/http/response"

	"github.com/gin-gonic/gin"
)

// Success 输出 Guest API 成功响应。
func Success[T any](ctx *gin.Context, requestID string, data T) {
	response.GuestSuccess(ctx, requestID, data)
}

// Failure 输出不包含 cause、路径或堆栈的 Guest API 失败响应。
func Failure(ctx *gin.Context, requestID string, appErr *apperr.AppError) {
	appErr = normalizeResponseError(appErr)
	response.GuestFailure(ctx, requestID, appErr)
}

// normalizeResponseError 只允许 Step 6 契约声明的 HTTP status 进入 Guest 响应。
func normalizeResponseError(appErr *apperr.AppError) *apperr.AppError {
	if appErr == nil || appErr.ErrorCode == "" || !isAllowedStatus(appErr.Code) {
		return apperr.Sys(nil, apperr.WithOp("http.guest.response"))
	}
	return appErr
}

// isAllowedStatus 判断状态码是否属于 Guest API 公开契约。
func isAllowedStatus(status int) bool {
	switch status {
	case 400, 401, 404, 409, 413, 429, 500, 503:
		return true
	default:
		return false
	}
}
