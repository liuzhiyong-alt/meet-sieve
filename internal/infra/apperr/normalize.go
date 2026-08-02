package apperr

import (
	"fmt"

	crerrors "github.com/cockroachdb/errors"
)

// Biz 创建可预期的业务、校验或取消错误。
func Biz(code Code, opts ...Option) *AppError {
	return newAppError(code, nil, opts...)
}

// Sys 创建内部系统错误，并在首次分类时保留 cause 调用栈。
func Sys(cause error, opts ...Option) *AppError {
	return newAppError(CodeInternal, withStack(CodeInternal, cause), opts...)
}

// Dependency 创建下游依赖错误，并在首次分类时保留 cause 调用栈。
func Dependency(code Code, cause error, opts ...Option) *AppError {
	return newAppError(code, withStack(code, cause), opts...)
}

// RecoveredPanic 将 panic 转换成安全的内部错误。
func RecoveredPanic(recovered any, op string) *AppError {
	return Sys(fmt.Errorf("panic recovered: %v", recovered), WithOp(op))
}

// Normalize 将任意错误归一为可安全对外传输的 AppError。
// 已有 AppError 会被保留；未知错误统一映射为内部错误并保留 cause。
func Normalize(err error) *AppError {
	if err == nil {
		return nil
	}

	var appErr *AppError
	if crerrors.As(err, &appErr) {
		return appErr
	}
	return Sys(err, WithOp("unclassified"))
}

// newAppError 根据集中登记的错误码创建统一错误对象。
func newAppError(code Code, cause error, opts ...Option) *AppError {
	appErr := &AppError{
		Code:      code.Value,
		ErrorCode: code.ErrorCode,
		Kind:      code.Kind,
		Message:   code.Message,
		Cause:     cause,
		Retryable: code.Retryable,
	}
	for _, opt := range opts {
		opt(appErr)
	}
	return appErr
}

// withStack 确保系统和依赖错误始终具有可供边界日志使用的调用栈。
func withStack(code Code, cause error) error {
	if cause == nil {
		cause = crerrors.New(code.Message)
	}
	return crerrors.WithStack(cause)
}
