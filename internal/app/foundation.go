package app

import (
	"meet-sieve/internal/adapter/agent/codex"
	"meet-sieve/internal/adapter/audio/malgo"
	"meet-sieve/internal/adapter/voice/onnx"
	"meet-sieve/internal/infra/assets"
	"meet-sieve/internal/infra/database"

	"gorm.io/gorm"
)

// FoundationDependencies 保存 Step 0 已确认基础适配器的无副作用工厂。
// 工厂进入桌面主程序的同一链接链，真实连接和资源初始化仍只在显式 smoke 中执行。
type FoundationDependencies struct {
	AudioEnumerator *malgo.Enumerator
	OpenDatabase    func(string) (*gorm.DB, error)
	NewONNXRuntime  func(assets.Asset, string) *onnx.Runtime
	NewCodexClient  func(codex.Config) *codex.Client
}

// NewFoundationDependencies 创建不会启动设备、数据库、动态库或子进程的基础依赖集合。
func NewFoundationDependencies() *FoundationDependencies {
	return &FoundationDependencies{
		AudioEnumerator: malgo.NewEnumerator(),
		OpenDatabase:    database.Open,
		NewONNXRuntime:  onnx.NewRuntime,
		NewCodexClient:  codex.NewClient,
	}
}

// Linked 验证所有已确认基础依赖都已进入当前应用装配。
func (d *FoundationDependencies) Linked() bool {
	return d != nil &&
		d.AudioEnumerator != nil &&
		d.OpenDatabase != nil &&
		d.NewONNXRuntime != nil &&
		d.NewCodexClient != nil
}
