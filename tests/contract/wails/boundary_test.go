package wails_test

import (
	"errors"
	"strings"
	"testing"

	"meet-sieve/internal/infra/apperr"
	infraLogger "meet-sieve/internal/infra/logger"
	wailstransport "meet-sieve/internal/transport/wails"
)

// TestInvoke_NormalizesUnknownError 验证 Wails 边界统一隐藏未知内部错误。
func TestInvoke_NormalizesUnknownError(t *testing.T) {
	t.Parallel()

	result := wailstransport.Invoke(
		wailstransport.NewBoundary(infraLogger.NewNop()),
		"wails.test.error",
		func(_ string) (struct{}, error) {
			return struct{}{}, errors.New("password=secret-value")
		},
	)

	if result.Code != apperr.CodeInternal.Value || result.Message != apperr.CodeInternal.Message {
		t.Fatalf("未知错误映射不正确：got %#v", result)
	}
	if result.Data != nil || strings.Contains(result.Message, "secret-value") {
		t.Fatalf("失败响应泄漏内部信息：got %#v", result)
	}
}

// TestInvoke_RecoversPanic 验证 Wails binding panic 不会退出进程。
func TestInvoke_RecoversPanic(t *testing.T) {
	t.Parallel()

	result := wailstransport.Invoke(
		wailstransport.NewBoundary(infraLogger.NewNop()),
		"wails.test.panic",
		func(_ string) (struct{}, error) {
			panic("boom")
		},
	)

	if result.Code != apperr.CodeInternal.Value || result.Message != apperr.CodeInternal.Message {
		t.Fatalf("panic 映射不正确：got %#v", result)
	}
}
