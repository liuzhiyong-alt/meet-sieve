package wails

import appbootstrap "meet-sieve/internal/app/bootstrap"

// BootstrapBinding 暴露启动状态、首次接入和升级重试，不直接读写 SQLite 或 locator。
type BootstrapBinding struct {
	coordinator *appbootstrap.Coordinator
	boundary    *Boundary
}

// NewBootstrapBinding 创建启动状态 binding。
func NewBootstrapBinding(coordinator *appbootstrap.Coordinator, boundary *Boundary) *BootstrapBinding {
	return &BootstrapBinding{coordinator: coordinator, boundary: boundary}
}

// GetBootstrapState 返回当前启动阶段，不触发重试、文件系统或数据库副作用。
func (binding *BootstrapBinding) GetBootstrapState() Result[BootstrapStateDTO] {
	return Invoke(binding.boundary, "wails.bootstrap.get_state", func(_ string) (BootstrapStateDTO, error) {
		return mapBootstrapState(binding.coordinator.GetState()), nil
	})
}

// UseWorkspace 在首次或故障页面初始化/验证选择的路径，并继续当前进程的启动。
func (binding *BootstrapBinding) UseWorkspace(path string) Result[BootstrapStateDTO] {
	return Invoke(binding.boundary, "wails.bootstrap.use_workspace", func(_ string) (BootstrapStateDTO, error) {
		state, err := binding.coordinator.UseWorkspace(path)
		return mapBootstrapState(state), err
	})
}

// RetryDatabaseUpgrade 重新从 locator 和数据库状态执行升级检查。
func (binding *BootstrapBinding) RetryDatabaseUpgrade() Result[BootstrapStateDTO] {
	return Invoke(binding.boundary, "wails.bootstrap.retry_database_upgrade", func(_ string) (BootstrapStateDTO, error) {
		return mapBootstrapState(binding.coordinator.RetryDatabaseUpgrade()), nil
	})
}
