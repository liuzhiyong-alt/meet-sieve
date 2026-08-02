package health_test

import (
	"testing"

	"meet-sieve/internal/app/health"
)

// TestNewRegistry_StartsSetupRequired 验证 Step 0 不创建默认工作目录。
func TestNewRegistry_StartsSetupRequired(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry()
	if snapshot := registry.Get(); snapshot.Status != health.StatusSetupRequired {
		t.Fatalf("初始状态不正确：got %q", snapshot.Status)
	}
}
