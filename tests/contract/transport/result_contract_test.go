package transport_test

import (
	"encoding/json"
	"errors"
	"testing"

	"meet-sieve/internal/infra/apperr"
	wailstransport "meet-sieve/internal/transport/wails"
)

// TestFailureResult_UsesSafeAppErrorFields 验证 Wails 失败响应不序列化内部 cause。
func TestFailureResult_UsesSafeAppErrorFields(t *testing.T) {
	t.Parallel()

	appErr := apperr.Dependency(
		apperr.CodeDependency,
		errors.New("codex unavailable"),
		apperr.WithOp("codex.initialize"),
	)
	result := wailstransport.Failure[struct{}]("request-1", appErr)

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if payload["code"] != float64(apperr.CodeDependency.Value) {
		t.Fatalf("错误码不正确：got %#v", payload["code"])
	}
	if payload["message"] != apperr.CodeDependency.Message {
		t.Fatalf("用户提示不正确：got %#v", payload["message"])
	}
	if payload["errorCode"] != apperr.CodeDependency.ErrorCode {
		t.Fatalf("稳定字符串错误码不正确：got %#v", payload["errorCode"])
	}
	if _, exists := payload["cause"]; exists {
		t.Fatal("响应不能暴露内部 cause")
	}
}

// TestSuccessResult_OmitsErrorCode 验证成功响应不携带只属于失败语义的稳定错误码。
func TestSuccessResult_OmitsErrorCode(t *testing.T) {
	t.Parallel()

	result := wailstransport.Success("request-2", struct{}{})
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if _, exists := payload["errorCode"]; exists {
		t.Fatal("成功响应不能携带 errorCode")
	}
}
