package wails_test

import (
	"testing"

	appbootstrap "meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/infra/config"
	infraLogger "meet-sieve/internal/infra/logger"
	wailstransport "meet-sieve/internal/transport/wails"
)

// TestBootstrapBinding_ExposesNeedsWorkspaceDTO 验证 Wails 只暴露稳定 phase/动作，不暴露 locator 或 SQLite 内部错误。
func TestBootstrapBinding_ExposesNeedsWorkspaceDTO(t *testing.T) {
	coordinator := appbootstrap.NewCoordinator(appbootstrap.Dependencies{Locator: missingWorkspaceLocator{}})
	coordinator.Start()
	binding := wailstransport.NewBootstrapBinding(coordinator, wailstransport.NewBoundary(infraLogger.NewNop()))

	result := binding.GetBootstrapState()
	if result.Code != 200 || result.Data == nil {
		t.Fatalf("bootstrap binding 响应不正确：%+v", result)
	}
	if result.Data.Phase != "needs_workspace" || len(result.Data.AvailableActions) != 1 || result.Data.AvailableActions[0] != "select_workspace" {
		t.Fatalf("bootstrap DTO 不正确：%+v", result.Data)
	}
}

// missingWorkspaceLocator 仅表达未配置系统 locator 的边界状态。
type missingWorkspaceLocator struct{}

// Load 返回 locator 不存在；不创建文件或工作目录。
func (missingWorkspaceLocator) Load() (config.Locator, bool, error) {
	return config.Locator{}, false, nil
}
