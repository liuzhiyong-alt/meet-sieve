package wails

import (
	appbootstrap "meet-sieve/internal/app/bootstrap"
	domainworkspace "meet-sieve/internal/domain/workspace"
)

// BootstrapStateDTO 是前端启动状态的稳定 JSON 契约。
type BootstrapStateDTO struct {
	Phase            string   `json:"phase"`
	Reason           string   `json:"reason"`
	Message          string   `json:"message"`
	Retryable        bool     `json:"retryable"`
	AvailableActions []string `json:"available_actions"`
}

// WorkspaceCandidateDTO 是目录轻量检查的脱敏结果；不会包含 SQLite 原始错误或堆栈。
type WorkspaceCandidateDTO struct {
	CanonicalPath string   `json:"canonical_path"`
	Kind          string   `json:"kind"`
	Reason        string   `json:"reason"`
	Writable      bool     `json:"writable"`
	LocalVolume   bool     `json:"local_volume"`
	SchemaState   string   `json:"schema_state"`
	FreeBytes     uint64   `json:"free_bytes"`
	Warnings      []string `json:"warnings"`
}

// WorkspaceSettingsDTO 是 General 设置页的当前/下次启动路径投影。
type WorkspaceSettingsDTO struct {
	ActivePath      string `json:"active_path"`
	SavedPath       string `json:"saved_path"`
	RestartRequired bool   `json:"restart_required"`
	Editable        bool   `json:"editable"`
	DisabledReason  string `json:"disabled_reason"`
}

// mapBootstrapState 复制启动状态，避免传输层泄漏内部对象或可变切片。
func mapBootstrapState(state appbootstrap.State) BootstrapStateDTO {
	actions := make([]string, 0, len(state.AvailableActions))
	for _, action := range state.AvailableActions {
		actions = append(actions, string(action))
	}
	return BootstrapStateDTO{Phase: string(state.Phase), Reason: string(state.Reason), Message: state.Message, Retryable: state.Retryable, AvailableActions: actions}
}

// mapWorkspaceCandidate 把 domain 目录检查结果映射到 Wails JSON 字段。
func mapWorkspaceCandidate(candidate domainworkspace.WorkspaceCandidate) WorkspaceCandidateDTO {
	warnings := make([]string, 0, len(candidate.Warnings))
	for _, warning := range candidate.Warnings {
		warnings = append(warnings, string(warning))
	}
	return WorkspaceCandidateDTO{
		CanonicalPath: candidate.Path, Kind: string(candidate.Kind), Reason: string(candidate.Reason), Writable: candidate.Writable,
		LocalVolume: candidate.LocalVolume, SchemaState: string(candidate.SchemaState), FreeBytes: candidate.FreeBytes, Warnings: warnings,
	}
}

// mapWorkspaceSettings 转换 General 设置页字段，不混入数据库身份或运行时配置。
func mapWorkspaceSettings(settings domainworkspace.WorkspaceSettings) WorkspaceSettingsDTO {
	return WorkspaceSettingsDTO{
		ActivePath: settings.ActivePath, SavedPath: settings.SavedPath, RestartRequired: settings.RestartRequired,
		Editable: settings.Editable, DisabledReason: string(settings.DisabledReason),
	}
}
