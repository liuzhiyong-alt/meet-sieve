package wails

import appbootstrap "meet-sieve/internal/app/bootstrap"

// WorkspaceBinding 暴露目录轻量检查和 General 设置保存，不提供目录迁移或备份管理接口。
type WorkspaceBinding struct {
	coordinator *appbootstrap.Coordinator
	boundary    *Boundary
}

// NewWorkspaceBinding 创建工作目录 binding。
func NewWorkspaceBinding(coordinator *appbootstrap.Coordinator, boundary *Boundary) *WorkspaceBinding {
	return &WorkspaceBinding{coordinator: coordinator, boundary: boundary}
}

// InspectWorkspaceCandidate 只返回候选分类，空输入不会创建目录、数据库或 locator。
func (binding *WorkspaceBinding) InspectWorkspaceCandidate(path string) Result[WorkspaceCandidateDTO] {
	return Invoke(binding.boundary, "wails.workspace.inspect_candidate", func(_ string) (WorkspaceCandidateDTO, error) {
		return mapWorkspaceCandidate(binding.coordinator.InspectWorkspaceCandidate(path)), nil
	})
}

// GetWorkspaceSettings 返回 General 页面所需的 active/saved 路径状态。
func (binding *WorkspaceBinding) GetWorkspaceSettings() Result[WorkspaceSettingsDTO] {
	return Invoke(binding.boundary, "wails.workspace.get_settings", func(_ string) (WorkspaceSettingsDTO, error) {
		return mapWorkspaceSettings(binding.coordinator.GetWorkspaceSettings()), nil
	})
}

// SaveWorkspacePath 初始化或验证目标后原子保存 locator，当前进程数据库不会切换。
func (binding *WorkspaceBinding) SaveWorkspacePath(path string) Result[WorkspaceSettingsDTO] {
	return Invoke(binding.boundary, "wails.workspace.save_path", func(_ string) (WorkspaceSettingsDTO, error) {
		settings, err := binding.coordinator.SaveWorkspacePath(path)
		return mapWorkspaceSettings(settings), err
	})
}
