package http_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"
	infraLogger "meet-sieve/internal/infra/logger"
	transporthttp "meet-sieve/internal/transport/http"
	httpmiddleware "meet-sieve/internal/transport/http/middleware"

	"github.com/gin-gonic/gin"
)

// TestHealth_ReturnsSetupRequiredWithRequestID 验证 Gin 不监听 LAN 时仍可提供契约化 health。
func TestHealth_ReturnsSetupRequiredWithRequestID(t *testing.T) {
	t.Parallel()

	engine, _, _ := newTestEngine(t)
	recorder := performRequest(engine, http.MethodGet, "/health", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码不正确：got %d", recorder.Code)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("响应缺少 request ID")
	}
	assertResult(t, recorder, http.StatusOK, "成功")
}

// TestRequestID_PreservesValidHeaderAndReplacesInvalidValue 验证请求标识只接受受控字符。
func TestRequestID_PreservesValidHeaderAndReplacesInvalidValue(t *testing.T) {
	t.Parallel()

	engine, _, _ := newTestEngine(t)
	valid := performRequest(engine, http.MethodGet, "/health", "client-request_1")
	if got := valid.Header().Get("X-Request-ID"); got != "client-request_1" {
		t.Fatalf("合法 request ID 未贯穿：got %q", got)
	}

	invalid := performRequest(engine, http.MethodGet, "/health", "invalid request id")
	if got := invalid.Header().Get("X-Request-ID"); got == "" || got == "invalid request id" {
		t.Fatalf("非法 request ID 未被替换：got %q", got)
	}
}

// TestErrorHandler_WritesRegisteredBusinessError 验证业务错误的 HTTP status 与 body code 一致。
func TestErrorHandler_WritesRegisteredBusinessError(t *testing.T) {
	t.Parallel()

	engine, _, _ := newTestEngine(t)
	engine.GET("/conflict", func(ctx *gin.Context) {
		httpmiddleware.AbortWithError(ctx, apperr.Biz(
			apperr.CodeConflict,
			apperr.WithOp("test.conflict"),
		))
	})

	recorder := performRequest(engine, http.MethodGet, "/conflict", "")
	assertResult(t, recorder, http.StatusConflict, apperr.CodeConflict.Message)
}

// TestErrorHandler_HidesUnknownCause 验证未知错误不会通过 HTTP 响应泄漏内部信息。
func TestErrorHandler_HidesUnknownCause(t *testing.T) {
	t.Parallel()

	engine, appLogger, logPath := newTestEngine(t)
	engine.GET("/unknown", func(ctx *gin.Context) {
		httpmiddleware.AbortWithError(ctx, errors.New("password=secret-value"))
	})

	recorder := performRequest(engine, http.MethodGet, "/unknown", "request-unknown")
	assertResult(t, recorder, http.StatusInternalServerError, apperr.CodeInternal.Message)
	if strings.Contains(recorder.Body.String(), "secret-value") {
		t.Fatalf("响应泄漏内部错误：%s", recorder.Body.String())
	}

	syncLogger(t, appLogger)
	content := readFile(t, logPath)
	if strings.Contains(content, "secret-value") || !strings.Contains(content, "[REDACTED]") {
		t.Fatalf("错误日志未正确脱敏：%s", content)
	}
}

// TestRecovery_Returns500AndEngineContinuesServing 验证 panic 被转换为 500 且不会中断后续请求。
func TestRecovery_Returns500AndEngineContinuesServing(t *testing.T) {
	t.Parallel()

	engine, appLogger, logPath := newTestEngine(t)
	engine.GET("/panic", func(_ *gin.Context) {
		panic("boom")
	})

	panicRecorder := performRequest(engine, http.MethodGet, "/panic", "request-panic")
	assertResult(t, panicRecorder, http.StatusInternalServerError, apperr.CodeInternal.Message)

	healthRecorder := performRequest(engine, http.MethodGet, "/health", "")
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("panic 后 engine 未继续服务：got %d", healthRecorder.Code)
	}

	syncLogger(t, appLogger)
	content := readFile(t, logPath)
	for _, expected := range []string{
		`"component":"http"`,
		`"component":"http.access"`,
		`"request_id":"request-panic"`,
		`"status":500`,
		`"operation":"http.recovery"`,
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("panic 日志缺少字段 %q：%s", expected, content)
		}
	}
}

// TestErrorHandler_DoesNotAppendFailureAfterResponseWritten 验证已写响应后只记录错误。
func TestErrorHandler_DoesNotAppendFailureAfterResponseWritten(t *testing.T) {
	t.Parallel()

	engine, _, _ := newTestEngine(t)
	engine.GET("/written", func(ctx *gin.Context) {
		ctx.JSON(http.StatusAccepted, gin.H{"accepted": true})
		_ = ctx.Error(apperr.Biz(apperr.CodeConflict, apperr.WithOp("test.written")))
	})

	recorder := performRequest(engine, http.MethodGet, "/written", "")
	if recorder.Code != http.StatusAccepted || strings.TrimSpace(recorder.Body.String()) != `{"accepted":true}` {
		t.Fatalf("已写响应被破坏：status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// newTestEngine 创建使用临时文件日志的 Gin Engine。
func newTestEngine(t *testing.T) (*gin.Engine, *infraLogger.AppLogger, string) {
	t.Helper()
	logDir := t.TempDir()
	appLogger, err := infraLogger.New(config.LogConfig{
		Level:      "info",
		MaxSizeMB:  20,
		MaxBackups: 2,
		MaxAgeDays: 1,
	}, logDir, buildinfo.Info{Version: "test", Commit: "test"})
	if err != nil {
		t.Fatalf("创建测试日志器失败：%v", err)
	}
	t.Cleanup(func() {
		_ = appLogger.SyncAndClose()
	})
	return transporthttp.NewEngine(health.NewRegistry(), appLogger), appLogger, filepath.Join(logDir, "app.log")
}

// performRequest 对测试 Engine 发起请求。
func performRequest(engine http.Handler, method string, path string, requestID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

// assertResult 断言统一响应的 HTTP 状态、body code、message 和 request ID。
func assertResult(t *testing.T, recorder *httptest.ResponseRecorder, code int, message string) {
	t.Helper()
	if recorder.Code != code {
		t.Fatalf("HTTP 状态不正确：got %d, want %d, body=%s", recorder.Code, code, recorder.Body.String())
	}
	var result struct {
		Code      int    `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("响应不是合法 JSON：%v，body=%s", err, recorder.Body.String())
	}
	if result.Code != code || result.Message != message || result.RequestID == "" {
		t.Fatalf("响应内容不正确：got %#v", result)
	}
}

// syncLogger 刷新测试日志。
func syncLogger(t *testing.T, appLogger *infraLogger.AppLogger) {
	t.Helper()
	if err := appLogger.Sync(); err != nil {
		t.Fatalf("刷新测试日志失败：%v", err)
	}
}

// readFile 读取测试文件内容。
func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败：%v", err)
	}
	return string(content)
}
