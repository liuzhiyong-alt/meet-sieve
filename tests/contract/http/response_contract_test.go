package http_test

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/transport/http/response"

	"github.com/gin-gonic/gin"
)

// TestResultContract_UsesStableSnakeCaseFields 验证 HTTP 统一响应字段和错误隔离保持稳定。
func TestResultContract_UsesStableSnakeCaseFields(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	response.Failure(
		ctx,
		"request-1",
		apperr.Sys(errors.New("password=secret-value"), apperr.WithOp("test.contract")),
	)

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("反序列化 HTTP 响应失败：%v", err)
	}
	if payload["code"] != float64(apperr.CodeInternal.Value) {
		t.Fatalf("错误码不正确：got %#v", payload["code"])
	}
	if payload["message"] != apperr.CodeInternal.Message {
		t.Fatalf("用户提示不正确：got %#v", payload["message"])
	}
	if payload["request_id"] != "request-1" {
		t.Fatalf("request_id 不正确：got %#v", payload["request_id"])
	}
	for _, forbidden := range []string{"cause", "stack", "operation"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("HTTP 响应不能包含内部字段 %q", forbidden)
		}
	}
}
