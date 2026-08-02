// Package bootstrap 编排工作目录、数据库升级与运行时 SQLite 生命周期。
package bootstrap

import domainworkspace "meet-sieve/internal/domain/workspace"

// State 是由 locator、目录检查和 SQLite 真实状态重建的启动状态投影。
type State struct {
	Phase            domainworkspace.BootstrapPhase
	Reason           domainworkspace.CandidateReason
	Message          string
	Retryable        bool
	AvailableActions []domainworkspace.BootstrapAction
}

// cloneState 防止调用方修改 coordinator 保存的 actions 切片。
func cloneState(state State) State {
	state.AvailableActions = append([]domainworkspace.BootstrapAction(nil), state.AvailableActions...)
	return state
}
