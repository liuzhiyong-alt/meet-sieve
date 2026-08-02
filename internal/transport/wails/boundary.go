package wails

import (
	"meet-sieve/internal/infra/apperr"
	infraLogger "meet-sieve/internal/infra/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Boundary 统一处理 Wails binding 的请求标识、错误记录和 panic 恢复。
type Boundary struct {
	// logger 负责在 Wails 边界记录一次完整错误。
	logger *infraLogger.AppLogger
}

// NewBoundary 创建 Wails 调用边界。
func NewBoundary(appLogger *infraLogger.AppLogger) *Boundary {
	if appLogger == nil {
		appLogger = infraLogger.NewNop()
	}
	return &Boundary{logger: appLogger}
}

// Invoke 在统一边界内执行一次 Wails binding 调用。
func Invoke[T any](boundary *Boundary, op string, action func(requestID string) (T, error)) (result Result[T]) {
	if boundary == nil {
		boundary = NewBoundary(nil)
	}
	requestID := uuid.NewString()

	defer func() {
		if recovered := recover(); recovered != nil {
			appErr := apperr.RecoveredPanic(recovered, op)
			boundary.logError(requestID, appErr)
			result = Failure[T](requestID, appErr)
		}
	}()

	data, err := action(requestID)
	if err != nil {
		appErr := normalizeAt(err, op)
		boundary.logError(requestID, appErr)
		return Failure[T](requestID, appErr)
	}
	return Success(requestID, data)
}

// normalizeAt 为未分类错误补充 Wails binding 操作名。
func normalizeAt(err error, op string) *apperr.AppError {
	appErr := apperr.Normalize(err)
	if appErr.Op == "" || appErr.Op == "unclassified" {
		// 避免修改可能被其他调用链复用的原错误对象。
		cloned := *appErr
		cloned.Op = op
		appErr = &cloned
	}
	return appErr
}

// logError 在 Wails 边界记录一次完整错误上下文。
func (boundary *Boundary) logError(requestID string, appErr *apperr.AppError) {
	boundary.logger.LogError(
		"Wails 调用失败",
		requestID,
		appErr,
		zap.String("component", "wails"),
	)
}

// logInfo 记录 Wails 自动化验证等非错误边界事件。
func (boundary *Boundary) logInfo(message string, requestID string, fields ...zap.Field) {
	boundary.logger.Component("wails").Info(
		message,
		append([]zap.Field{zap.String("request_id", requestID)}, fields...)...,
	)
}
