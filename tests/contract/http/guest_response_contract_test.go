package http_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"meet-sieve/internal/infra/apperr"
	guesthttp "meet-sieve/internal/transport/http/guest"

	"github.com/gin-gonic/gin"
)

// TestGuestResponse_SuccessUsesGuestEnvelope 验证 Guest API 成功响应不复用健康检查整数契约。
func TestGuestResponse_SuccessUsesGuestEnvelope(t *testing.T) {
	t.Parallel()

	recorder, ctx := newGuestResponseContext()
	guesthttp.Success(ctx, "request-success", map[string]string{"meeting_id": "public-meeting"})

	payload := decodeGuestEnvelope(t, recorder)
	if recorder.Code != http.StatusOK || !payload.Success || payload.Code != "OK" {
		t.Fatalf("成功响应契约不正确：status=%d payload=%#v", recorder.Code, payload)
	}
	if payload.Message != apperr.CodeOK.Message || payload.RequestID != "request-success" || payload.Data == nil {
		t.Fatalf("成功响应字段不完整：%#v", payload)
	}
}

// TestGuestResponse_FailureMapsStableHTTPStatus 验证 Guest 失败响应的 HTTP status 和字符串 code。
func TestGuestResponse_FailureMapsStableHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code apperr.Code
	}{
		{name: "bad request", code: apperr.CodeMessageInvalid},
		{name: "unauthorized", code: apperr.CodeLANSessionInvalid},
		{name: "not found", code: apperr.CodeAttachmentNotFound},
		{name: "conflict", code: apperr.CodeLANGenerationChanged},
		{name: "too large", code: apperr.CodeAttachmentTooLarge},
		{name: "rate limited", code: apperr.CodeLANRateLimited},
		{name: "internal", code: apperr.CodeInternal},
		{name: "unavailable", code: apperr.CodeLANStartFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder, ctx := newGuestResponseContext()
			guesthttp.Failure(ctx, "request-failure", apperr.Biz(tt.code))
			payload := decodeGuestEnvelope(t, recorder)

			if recorder.Code != tt.code.Value || payload.Success || payload.Code != tt.code.ErrorCode {
				t.Fatalf("失败响应契约不正确：status=%d payload=%#v", recorder.Code, payload)
			}
			if payload.Message != tt.code.Message || payload.RequestID != "request-failure" || payload.Data != nil {
				t.Fatalf("失败响应泄漏或缺失字段：%#v", payload)
			}
		})
	}
}

// TestGuestResponse_HidesUnknownCause 验证未知底层错误不进入 Guest JSON。
func TestGuestResponse_HidesUnknownCause(t *testing.T) {
	t.Parallel()

	recorder, ctx := newGuestResponseContext()
	guesthttp.Failure(ctx, "request-secret", apperr.Normalize(errors.New("token=secret-value")))
	payload := decodeGuestEnvelope(t, recorder)

	if payload.Code != apperr.CodeInternal.ErrorCode || payload.Message != apperr.CodeInternal.Message {
		t.Fatalf("未知错误未归一为安全内部错误：%#v", payload)
	}
	if payload.Data != nil {
		t.Fatalf("失败响应 data 必须显式为 null：%#v", payload.Data)
	}
}

type guestEnvelope struct {
	Success   bool             `json:"success"`
	Code      string           `json:"code"`
	Message   string           `json:"message"`
	Data      *json.RawMessage `json:"data"`
	RequestID string           `json:"request_id"`
}

// newGuestResponseContext 创建不启动网络监听的 Gin 响应上下文。
func newGuestResponseContext() (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	return recorder, ctx
}

// decodeGuestEnvelope 解析 Guest 响应并保留 data 的 null 语义。
func decodeGuestEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) guestEnvelope {
	t.Helper()
	var payload guestEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("反序列化 Guest 响应失败：%v，body=%s", err, recorder.Body.String())
	}
	return payload
}
