package main

import (
	"context"
	"sync"

	application "meet-sieve/internal/app"
	appbootstrap "meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/apperr"
	infraLogger "meet-sieve/internal/infra/logger"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.uber.org/zap"
)

// App 将 Wails 生命周期回调委托给 Fx Runtime。
type App struct {
	// runtime 管理所有长生命周期基础组件。
	runtime *application.Runtime
	// logger 记录 Wails 生命周期边界错误。
	logger *infraLogger.AppLogger
	// health 向前端投影安全失败状态。
	health *health.Registry
	// dependencies 保持 Step 0 基础适配器处于桌面应用同一链接链。
	dependencies *application.FoundationDependencies
	// workspace 管理 Step 1 数据库启动、停止和状态重建。
	workspace *appbootstrap.Coordinator
	// voice 管理独立于工作目录的声纹模型与 ONNX Runtime 生命周期。
	voice *application.VoiceModule
	// meeting 管理当前工作目录唯一的会议录音运行时与启动恢复。
	meeting *application.MeetingModule
	// voiceStartDone 协调异步模型初始化与应用退出，避免模型加载阻塞成员和工作目录。
	voiceStartDone chan struct{}
	voiceStartMu   sync.Mutex
}

// AttachVoiceModule 在 Wails 启动前接入声纹模型与运行时模块。
func (app *App) AttachVoiceModule(module *application.VoiceModule) {
	app.voice = module
}

// AttachWorkspaceCoordinator 在 Wails 启动前接入 Step 1 工作目录协调器。
func (app *App) AttachWorkspaceCoordinator(coordinator *appbootstrap.Coordinator) {
	app.workspace = coordinator
}

// AttachMeetingModule 在 Wails 启动前接入会议录音与恢复模块。
func (app *App) AttachMeetingModule(module *application.MeetingModule) {
	app.meeting = module
}

// NewApp 创建桌面应用生命周期桥接对象。
func NewApp(
	runtime *application.Runtime,
	appLogger *infraLogger.AppLogger,
	registry *health.Registry,
	dependencies *application.FoundationDependencies,
) *App {
	return &App{
		runtime:      runtime,
		logger:       appLogger,
		health:       registry,
		dependencies: dependencies,
	}
}

// startup 启动 Fx 管理的基础组件，并捕获生命周期回调中的 panic。
func (app *App) startup(ctx context.Context) {
	defer app.recoverLifecyclePanic("wails.lifecycle.startup")
	if !app.dependencies.Linked() {
		app.health.Set(health.Snapshot{
			Status:    health.StatusFailed,
			ErrorCode: apperr.CodeInternal.Value,
			Message:   apperr.CodeInternal.Message,
		})
		return
	}
	_ = app.runtime.Start(ctx)
	if app.workspace != nil {
		app.workspace.Start()
	}
	if app.meeting != nil {
		if _, err := app.meeting.Recover(ctx); err != nil {
			app.logger.LogError(
				"会议录音启动恢复未完成", "meeting-recovery",
				apperr.Dependency(apperr.CodeMeetingRecoveryFailed, err, apperr.WithOp("app.meeting.recover")),
				zap.String("component", "meeting"),
			)
		}
	}
	app.startVoiceAsync(ctx)
}

// shutdown 停止 Fx 管理的基础组件，并捕获生命周期回调中的 panic。
func (app *App) shutdown(ctx context.Context) {
	defer app.recoverLifecyclePanic("wails.lifecycle.shutdown")
	if app.meeting != nil {
		if err := app.meeting.FinishActiveMeeting(ctx); err != nil {
			app.logger.LogError(
				"应用退出前会议收尾未完成，将在下次启动恢复", "meeting-shutdown",
				apperr.Dependency(apperr.CodeMeetingRecoveryRequired, err, apperr.WithOp("app.meeting.shutdown")),
				zap.String("component", "meeting"),
			)
		}
		app.meeting.StopSpeakerAutomation()
	}
	if app.workspace != nil {
		_ = app.workspace.Stop()
	}
	if app.voice != nil && app.waitForVoiceStart(ctx) {
		_ = app.voice.Stop()
	}
	_ = app.runtime.Stop(ctx)
}

// startVoiceAsync 让模型和 ONNX Runtime 初始化不阻塞工作目录与成员页面。
func (app *App) startVoiceAsync(ctx context.Context) {
	if app.voice == nil {
		return
	}
	done := make(chan struct{})
	app.voiceStartMu.Lock()
	app.voiceStartDone = done
	app.voiceStartMu.Unlock()
	go func() {
		defer func() {
			close(done)
			// 模型初始化完成后通知仍停留在成员页或设置页的前端刷新真实状态。
			runtime.EventsEmit(ctx, "voice.model.changed")
		}()
		if err := app.voice.Start(); err != nil {
			app.logger.LogError(
				"声纹组件启动失败，其他功能继续可用", "voice-startup",
				apperr.Dependency(apperr.CodeVoiceModelUnavailable, err, apperr.WithOp("app.voice.start")),
				zap.String("component", "voice"),
			)
		}
	}()
}

// waitForVoiceStart 等待已开始的初始化完成；退出超时后不与初始化并发销毁运行时。
func (app *App) waitForVoiceStart(ctx context.Context) bool {
	app.voiceStartMu.Lock()
	done := app.voiceStartDone
	app.voiceStartMu.Unlock()
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// activateWindow 将收到二次启动请求的主窗口恢复并显示在桌面上。
func (app *App) activateWindow(ctx context.Context) {
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

// beforeClose 在活动会议期间取消直接关窗，并让前端提供继续或结束并退出选择。
func (app *App) beforeClose(ctx context.Context) bool {
	if app.meeting == nil {
		return false
	}
	active, err := app.meeting.HasActiveMeeting(ctx)
	if err != nil {
		app.logger.LogError(
			"检查活动会议失败，已阻止直接关闭窗口", "meeting-before-close",
			apperr.Sys(err, apperr.WithOp("app.meeting.before_close")),
			zap.String("component", "meeting"),
		)
		return true
	}
	if !active {
		return false
	}
	runtime.EventsEmit(ctx, "meeting.close.requested")
	return true
}

// recoverLifecyclePanic 将 Wails 生命周期 panic 转为安全健康状态和边界日志。
func (app *App) recoverLifecyclePanic(op string) {
	recovered := recover()
	if recovered == nil {
		return
	}
	appErr := apperr.RecoveredPanic(recovered, op)
	app.health.Set(health.Snapshot{
		Status:    health.StatusFailed,
		ErrorCode: appErr.Code,
		Message:   appErr.Message,
	})
	app.logger.LogError(
		"Wails 生命周期异常",
		op,
		appErr,
		zap.String("component", "wails.lifecycle"),
	)
}
