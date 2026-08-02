package logger

import (
	"context"

	"go.uber.org/fx"
)

// Module 将日志刷新挂入 Fx 停止流程。
// 文件关闭仍由 bootstrap 在 Fx 完全停止后执行，以便记录停止阶段错误。
var Module = fx.Options(
	fx.Invoke(RegisterLifecycle),
)

// RegisterLifecycle 在其他 Fx 组件停止后刷新日志缓冲。
func RegisterLifecycle(lifecycle fx.Lifecycle, appLogger *AppLogger) {
	lifecycle.Append(fx.Hook{
		OnStop: func(context.Context) error {
			return appLogger.Sync()
		},
	})
}
