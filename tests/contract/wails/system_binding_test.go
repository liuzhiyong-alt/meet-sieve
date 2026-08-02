package wails_test

import (
	"testing"

	"meet-sieve/internal/app/health"
	infraLogger "meet-sieve/internal/infra/logger"
	wailstransport "meet-sieve/internal/transport/wails"
)

// TestSystemBinding_GetHealthReturnsSetupRequired 验证 binding 以统一 Result 返回健康状态。
func TestSystemBinding_GetHealthReturnsSetupRequired(t *testing.T) {
	t.Parallel()

	binding := wailstransport.NewSystemBinding(
		health.NewRegistry(),
		wailstransport.NewBoundary(infraLogger.NewNop()),
	)
	result := binding.GetHealth()
	if result.Code != 200 {
		t.Fatalf("响应码不正确：got %d", result.Code)
	}
	if result.Data == nil || result.Data.Status != health.StatusSetupRequired {
		t.Fatalf("健康状态不正确：got %#v", result.Data)
	}
}

// TestSystemBinding_RequestIDIsUniquePerInvocation 验证 Wails 调用不再复用固定请求标识。
func TestSystemBinding_RequestIDIsUniquePerInvocation(t *testing.T) {
	t.Parallel()

	binding := wailstransport.NewSystemBinding(
		health.NewRegistry(),
		wailstransport.NewBoundary(infraLogger.NewNop()),
	)
	first := binding.GetHealth()
	second := binding.GetHealth()
	if first.RequestID == "" || second.RequestID == "" || first.RequestID == second.RequestID {
		t.Fatalf("request ID 必须非空且每次唯一：first=%q second=%q", first.RequestID, second.RequestID)
	}
}
