package logger_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"
	infraLogger "meet-sieve/internal/infra/logger"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// TestNew_WritesStructuredRotatingLog 验证应用日志写入带公共字段的 JSON 文件。
func TestNew_WritesStructuredRotatingLog(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	appLogger, err := infraLogger.New(testLogConfig(), logDir, testBuildInfo())
	if err != nil {
		t.Fatalf("创建日志器失败：%v", err)
	}
	t.Cleanup(func() {
		_ = appLogger.SyncAndClose()
	})

	appLogger.Component("test").Info("应用已启动", zap.String("request_id", "request-1"))
	if err := appLogger.Sync(); err != nil {
		t.Fatalf("刷新日志失败：%v", err)
	}

	content := readLog(t, filepath.Join(logDir, "app.log"))
	for _, expected := range []string{
		`"msg":"应用已启动"`,
		`"component":"test"`,
		`"request_id":"request-1"`,
		`"app_version":"1.0.0-test"`,
		`"commit":"test-commit"`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("日志缺少字段 %q：%s", expected, content)
		}
	}
}

// TestLogError_RedactsSensitiveCauseAndPath 验证统一错误日志会脱敏凭证和完整用户路径。
func TestLogError_RedactsSensitiveCauseAndPath(t *testing.T) {
	t.Parallel()

	logDir := t.TempDir()
	appLogger, err := infraLogger.New(testLogConfig(), logDir, testBuildInfo())
	if err != nil {
		t.Fatalf("创建日志器失败：%v", err)
	}
	t.Cleanup(func() {
		_ = appLogger.SyncAndClose()
	})

	appErr := apperr.Sys(
		errors.New("打开 /Users/alice/private/meeting.db 失败 password=secret-value"),
		apperr.WithOp("database.open"),
	)
	appLogger.LogError("操作失败", "request-2", appErr)
	if err := appLogger.Sync(); err != nil {
		t.Fatalf("刷新日志失败：%v", err)
	}

	content := readLog(t, filepath.Join(logDir, "app.log"))
	for _, forbidden := range []string{"secret-value", "/Users/alice/private/meeting.db"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("日志泄漏敏感内容 %q：%s", forbidden, content)
		}
	}
	for _, expected := range []string{
		`"request_id":"request-2"`,
		`"operation":"database.open"`,
		`"error_code":500`,
		`[REDACTED]`,
		`[PATH]`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("日志缺少脱敏或错误字段 %q：%s", expected, content)
		}
	}
}

// TestSyncAndClose_IsIdempotent 验证日志关闭可以安全重复调用。
func TestSyncAndClose_IsIdempotent(t *testing.T) {
	t.Parallel()

	appLogger, err := infraLogger.New(testLogConfig(), t.TempDir(), testBuildInfo())
	if err != nil {
		t.Fatalf("创建日志器失败：%v", err)
	}
	if err := appLogger.SyncAndClose(); err != nil {
		t.Fatalf("首次关闭失败：%v", err)
	}
	if err := appLogger.SyncAndClose(); err != nil {
		t.Fatalf("重复关闭失败：%v", err)
	}
}

// TestModule_FxLifecycleFlushesWithoutClosing 验证 Fx 停止阶段能刷新日志并由 bootstrap 随后关闭。
func TestModule_FxLifecycleFlushesWithoutClosing(t *testing.T) {
	t.Parallel()

	appLogger, err := infraLogger.New(testLogConfig(), t.TempDir(), testBuildInfo())
	if err != nil {
		t.Fatalf("创建日志器失败：%v", err)
	}
	fxApp := fx.New(
		fx.NopLogger,
		fx.Supply(appLogger),
		infraLogger.Module,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fxApp.Start(ctx); err != nil {
		t.Fatalf("启动 Fx 失败：%v", err)
	}
	if err := fxApp.Stop(ctx); err != nil {
		t.Fatalf("停止 Fx 失败：%v", err)
	}
	if err := appLogger.SyncAndClose(); err != nil {
		t.Fatalf("Fx 停止后关闭日志失败：%v", err)
	}
}

// testLogConfig 返回日志测试使用的最小配置。
func testLogConfig() config.LogConfig {
	return config.LogConfig{
		Level:      "info",
		MaxSizeMB:  20,
		MaxBackups: 10,
		MaxAgeDays: 14,
		Compress:   true,
	}
}

// testBuildInfo 返回日志公共字段测试使用的构建信息。
func testBuildInfo() buildinfo.Info {
	return buildinfo.Info{Version: "1.0.0-test", Commit: "test-commit"}
}

// readLog 读取日志文件内容。
func readLog(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志失败：%v", err)
	}
	return string(content)
}
