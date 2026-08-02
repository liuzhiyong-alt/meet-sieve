// Package app 提供 Fx composition root 和应用生命周期管理。
package app

import (
	"context"
	"sync"
	"time"

	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/apperr"
	infraLogger "meet-sieve/internal/infra/logger"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

const lifecycleTimeout = 15 * time.Second

// RootContext 是所有长生命周期组件共享的根取消信号。
type RootContext struct {
	// Context 在 Runtime 停止前保持有效。
	context.Context
}

// Runtime 保存 Fx App、根 context 和幂等启停状态。
type Runtime struct {
	// app 是应用唯一的 Fx composition root。
	app *fx.App
	// health 保存可供 UI 查询的启动状态。
	health *health.Registry
	// logger 记录生命周期边界错误。
	logger *infraLogger.AppLogger
	// cancel 停止所有持有 RootContext 的后台组件。
	cancel context.CancelFunc
	// startOnce 保证 Fx 只启动一次。
	startOnce sync.Once
	// startErr 保存首次启动结果。
	startErr error
	// stopOnce 保证停止和资源回收幂等。
	stopOnce sync.Once
	// stopErr 保存首次停止结果。
	stopErr error
}

// NewRuntime 创建 MeetSieve 的 Fx composition root。
// options 用于后续基础设施模块扩展，以及生命周期集成测试注入。
func NewRuntime(registry *health.Registry, appLogger *infraLogger.AppLogger, options ...fx.Option) *Runtime {
	rootContext, cancel := context.WithCancel(context.Background())
	baseOptions := []fx.Option{
		fx.NopLogger,
		fx.Supply(registry, appLogger, RootContext{Context: rootContext}),
		fx.StartTimeout(lifecycleTimeout),
		fx.StopTimeout(lifecycleTimeout),
	}
	baseOptions = append(baseOptions, options...)
	return &Runtime{
		app:    fx.New(baseOptions...),
		health: registry,
		logger: appLogger,
		cancel: cancel,
	}
}

// Start 在 15 秒超时内启动 Fx，并将失败投影为安全健康状态。
func (runtime *Runtime) Start(ctx context.Context) error {
	runtime.startOnce.Do(func() {
		startContext, cancel := newLifecycleContext(ctx)
		defer cancel()
		runtime.startErr = runtime.app.Start(startContext)
		if runtime.startErr != nil {
			appErr := apperr.Sys(runtime.startErr, apperr.WithOp("app.runtime.start"))
			runtime.health.Set(health.Snapshot{
				Status:    health.StatusFailed,
				ErrorCode: appErr.Code,
				Message:   appErr.Message,
			})
			runtime.logger.LogError(
				"应用基础组件启动失败",
				"app-startup",
				appErr,
				zap.String("component", "app.lifecycle"),
			)
		}
	})
	return runtime.startErr
}

// Stop 先取消根 context，再在 15 秒超时内幂等停止 Fx。
func (runtime *Runtime) Stop(ctx context.Context) error {
	runtime.stopOnce.Do(func() {
		runtime.cancel()
		stopContext, cancel := newLifecycleContext(ctx)
		defer cancel()
		runtime.stopErr = runtime.app.Stop(stopContext)
		if runtime.stopErr != nil {
			appErr := apperr.Sys(runtime.stopErr, apperr.WithOp("app.runtime.stop"))
			runtime.logger.LogError(
				"应用基础组件停止失败",
				"app-shutdown",
				appErr,
				zap.String("component", "app.lifecycle"),
			)
		}
	})
	return runtime.stopErr
}

// newLifecycleContext 为已取消的 Wails 回调 context 提供独立的回收超时窗口。
func newLifecycleContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, lifecycleTimeout)
}
