package app

import (
	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/filesystem"
	infraLogger "meet-sieve/internal/infra/logger"

	"go.uber.org/fx"
)

// Bootstrap 保存 Wails 壳启动前必须可用的基础对象。
type Bootstrap struct {
	// Config 是已经严格校验的内嵌技术配置，供 Step 1 module 显式装配使用。
	Config config.Config
	// Health 向 Wails 壳投影基础组件启动状态。
	Health *health.Registry
	// Logger 是应用全局结构化文件日志器。
	Logger *infraLogger.AppLogger
	// Runtime 管理 Fx 组件启停和根 context。
	Runtime *Runtime
	// Dependencies 保证高风险基础适配器进入同一个桌面构建链。
	Dependencies *FoundationDependencies
}

// NewBootstrap 加载内嵌配置并创建日志、健康状态和 Fx Runtime。
// 初始化失败时返回可启动 UI 的降级对象，由 Health 向用户展示安全错误。
func NewBootstrap(expectedONNXVersion string) *Bootstrap {
	registry := health.NewRegistry()
	cfg, err := config.LoadDefault(expectedONNXVersion)
	if err != nil {
		return failedBootstrap(registry, err, "app.bootstrap.config")
	}

	logDir, err := filesystem.CurrentLogDir()
	if err != nil {
		return failedBootstrap(registry, err, "app.bootstrap.log_path")
	}
	appLogger, err := infraLogger.New(cfg.Log, logDir, buildinfo.Current())
	if err != nil {
		return failedBootstrap(registry, err, "app.bootstrap.logger")
	}
	return &Bootstrap{
		Config:       cfg,
		Health:       registry,
		Logger:       appLogger,
		Runtime:      NewRuntime(registry, appLogger, infraLogger.Module, fx.Supply(cfg)),
		Dependencies: NewFoundationDependencies(),
	}
}

// failedBootstrap 构造不依赖文件日志的安全降级启动对象。
func failedBootstrap(registry *health.Registry, cause error, op string) *Bootstrap {
	appErr := apperr.Sys(cause, apperr.WithOp(op))
	registry.Set(health.Snapshot{
		Status:    health.StatusFailed,
		ErrorCode: appErr.Code,
		Message:   appErr.Message,
	})
	appLogger := infraLogger.NewNop()
	return &Bootstrap{
		Health:       registry,
		Logger:       appLogger,
		Runtime:      NewRuntime(registry, appLogger),
		Dependencies: NewFoundationDependencies(),
	}
}
