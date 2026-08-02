package wails

import "meet-sieve/internal/infra/apperr"

// Result 是 Wails binding 返回给前端的统一响应结构。
// Error 的内部 cause 不会出现在此结构中。
type Result[T any] struct {
	Code      int    `json:"code"`
	ErrorCode string `json:"errorCode,omitempty"`
	Message   string `json:"message"`
	Data      *T     `json:"data,omitempty"`
	RequestID string `json:"requestId"`
}

// Success 构造成功响应。
func Success[T any](requestID string, data T) Result[T] {
	return Result[T]{
		Code:      apperr.CodeOK.Value,
		Message:   apperr.CodeOK.Message,
		Data:      &data,
		RequestID: requestID,
	}
}

// Failure 构造安全失败响应。
func Failure[T any](requestID string, err error) Result[T] {
	appErr := apperr.Normalize(err)
	if appErr == nil {
		appErr = apperr.Sys(nil, apperr.WithOp("wails.result.nil_error"))
	}
	return Result[T]{
		Code:      appErr.Code,
		ErrorCode: appErr.ErrorCode,
		Message:   appErr.Message,
		RequestID: requestID,
	}
}
