package apperr

import "go.uber.org/zap/zapcore"

// ClassifyLog 根据统一错误分类返回边界日志等级。
func ClassifyLog(appErr *AppError) zapcore.Level {
	if appErr == nil {
		return zapcore.InfoLevel
	}
	switch appErr.Kind {
	case KindCanceled:
		return zapcore.InfoLevel
	case KindValidation, KindBusiness:
		return zapcore.WarnLevel
	case KindDependency, KindSystem:
		return zapcore.ErrorLevel
	default:
		return zapcore.ErrorLevel
	}
}
