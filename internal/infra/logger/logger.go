// Package logger 提供 MeetSieve 的结构化文件日志和统一错误日志。
package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"

	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const appLogFileName = "app.log"

// AppLogger 保存应用日志器及其滚动文件生命周期。
type AppLogger struct {
	// root 是带构建公共字段的 Zap 根日志器。
	root *zap.Logger
	// rolling 持有 app.log 的轮转写入器。
	rolling *lumberjack.Logger
	// closeOnce 保证日志资源只关闭一次。
	closeOnce sync.Once
	// closeErr 保存首次关闭结果供重复调用返回。
	closeErr error
}

// New 创建 JSON 文件日志器，并确保平台日志目录存在。
func New(cfg config.LogConfig, logDir string, build buildinfo.Info) (*AppLogger, error) {
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	rolling := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, appLogFileName),
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
		Compress:   cfg.Compress,
	}
	core := zapcore.NewCore(newJSONEncoder(), zapcore.AddSync(rolling), level)
	root := zap.New(core, zap.AddCaller()).With(
		zap.String("app_version", build.Version),
		zap.String("commit", build.Commit),
	)
	return &AppLogger{root: root, rolling: rolling}, nil
}

// NewNop 创建不产生输出的日志器，用于日志文件初始化失败后的 UI 降级路径。
func NewNop() *AppLogger {
	return &AppLogger{root: zap.NewNop()}
}

// Component 返回带稳定组件字段的子日志器。
func (logger *AppLogger) Component(component string) *zap.Logger {
	if logger == nil || logger.root == nil {
		return zap.NewNop()
	}
	return logger.root.With(zap.String("component", component))
}

// LogError 在统一边界记录一次完整错误上下文。
func (logger *AppLogger) LogError(message string, requestID string, appErr *apperr.AppError, fields ...zap.Field) {
	if logger == nil || logger.root == nil || appErr == nil {
		return
	}
	errorFields := buildErrorFields(requestID, appErr, fields)
	boundaryLogger := logger.root.WithOptions(zap.AddCallerSkip(1))
	switch apperr.ClassifyLog(appErr) {
	case zapcore.InfoLevel:
		boundaryLogger.Info(message, errorFields...)
	case zapcore.WarnLevel:
		boundaryLogger.Warn(message, errorFields...)
	default:
		boundaryLogger.Error(message, errorFields...)
	}
}

// Sync 刷新日志缓冲，并过滤平台文件描述符的良性错误。
func (logger *AppLogger) Sync() error {
	if logger == nil || logger.root == nil {
		return nil
	}
	return filterSyncError(logger.root.Sync())
}

// SyncAndClose 幂等地刷新日志并关闭滚动文件。
func (logger *AppLogger) SyncAndClose() error {
	if logger == nil {
		return nil
	}
	logger.closeOnce.Do(func() {
		syncErr := logger.Sync()
		var closeErr error
		if logger.rolling != nil {
			closeErr = logger.rolling.Close()
		}
		logger.closeErr = multierr.Combine(syncErr, closeErr)
	})
	return logger.closeErr
}

// buildErrorFields 构造统一错误日志字段，并对 cause、stack 和扩展字段执行脱敏。
func buildErrorFields(requestID string, appErr *apperr.AppError, extra []zap.Field) []zap.Field {
	fields := []zap.Field{
		zap.String("request_id", requestID),
		zap.Int("error_code", appErr.Code),
		zap.String("error_kind", string(appErr.Kind)),
		zap.String("operation", appErr.Op),
		zap.Bool("retryable", appErr.Retryable),
	}
	keys := make([]string, 0, len(appErr.Fields))
	for key := range appErr.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fields = append(fields, zap.String(key, redactSensitive(appErr.Fields[key])))
	}
	if appErr.Cause != nil {
		fields = append(fields,
			zap.String("error", redactSensitive(appErr.Cause.Error())),
			zap.String("error_stack", redactSensitive(fmt.Sprintf("%+v", appErr.Cause))),
		)
	}
	return append(fields, extra...)
}

// parseLevel 解析内嵌配置中的日志等级。
func parseLevel(raw string) (zapcore.Level, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return zapcore.InfoLevel, fmt.Errorf("解析日志级别失败: %w", err)
	}
	return level, nil
}

// newJSONEncoder 创建统一字段名和 ISO8601 时间格式的 JSON 编码器。
func newJSONEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	return zapcore.NewJSONEncoder(encoderConfig)
}

// filterSyncError 忽略 stdout/stderr 在部分平台上的良性 Sync 错误。
func filterSyncError(err error) error {
	if err == nil {
		return nil
	}
	var unexpected []error
	for _, item := range multierr.Errors(err) {
		if !isBenignSyncError(item) {
			unexpected = append(unexpected, item)
		}
	}
	return multierr.Combine(unexpected...)
}

// isBenignSyncError 判断日志同步时可忽略的文件描述符错误。
func isBenignSyncError(err error) bool {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTTY) || errors.Is(err, syscall.EBADF) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid argument") ||
		strings.Contains(message, "inappropriate ioctl") ||
		strings.Contains(message, "bad file descriptor")
}
