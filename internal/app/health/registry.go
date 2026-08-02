// Package health 管理应用启动阶段的可观察健康状态。
package health

import "sync"

// Status 表示桌面应用可展示的启动状态。
type Status string

const (
	// StatusStarting 表示依赖仍在启动。
	StatusStarting Status = "starting"
	// StatusSetupRequired 表示尚未完成首次工作目录设置。
	StatusSetupRequired Status = "setup_required"
	// StatusReady 表示应用已可提供业务能力。
	StatusReady Status = "ready"
	// StatusFailed 表示启动失败。
	StatusFailed Status = "failed"
)

// Snapshot 是前端读取的健康状态投影。
type Snapshot struct {
	Status    Status `json:"status"`
	ErrorCode int    `json:"errorCode,omitempty"`
	Message   string `json:"message,omitempty"`
}

// Registry 并发安全地保存当前健康状态。
type Registry struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

// NewRegistry 创建初始为 setup_required 的状态注册表。
func NewRegistry() *Registry {
	return &Registry{snapshot: Snapshot{Status: StatusSetupRequired}}
}

// Get 返回当前健康状态的值副本。
func (r *Registry) Get() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

// Set 更新当前健康状态。
func (r *Registry) Set(snapshot Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshot = snapshot
}
