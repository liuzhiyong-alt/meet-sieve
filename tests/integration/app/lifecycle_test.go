package app_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	application "meet-sieve/internal/app"
	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/apperr"
	infraLogger "meet-sieve/internal/infra/logger"

	"go.uber.org/fx"
)

// TestRuntime_StartStopAndCancelRootContext 验证 Fx 正常启停、重复停止和根 context 取消。
func TestRuntime_StartStopAndCancelRootContext(t *testing.T) {
	var mu sync.Mutex
	var events []string
	var root application.RootContext

	runtime := application.NewRuntime(
		health.NewRegistry(),
		infraLogger.NewNop(),
		fx.Populate(&root),
		fx.Invoke(func(lifecycle fx.Lifecycle) {
			lifecycle.Append(fx.Hook{
				OnStart: func(context.Context) error {
					mu.Lock()
					defer mu.Unlock()
					events = append(events, "start")
					return nil
				},
				OnStop: func(context.Context) error {
					mu.Lock()
					defer mu.Unlock()
					events = append(events, "stop")
					return nil
				},
			})
		}),
	)

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("启动 Runtime 失败：%v", err)
	}
	shutdownContext, cancelShutdown := context.WithCancel(context.Background())
	cancelShutdown()
	if err := runtime.Stop(shutdownContext); err != nil {
		t.Fatalf("停止 Runtime 失败：%v", err)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("重复停止 Runtime 失败：%v", err)
	}

	select {
	case <-root.Done():
	case <-time.After(time.Second):
		t.Fatal("停止 Runtime 后根 context 未取消")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "start" || events[1] != "stop" {
		t.Fatalf("生命周期顺序不正确：got %#v", events)
	}
}

// TestRuntime_StartFailureRollsBackAndUpdatesHealth 验证中途启动失败会回收已启动组件并投影安全健康状态。
func TestRuntime_StartFailureRollsBackAndUpdatesHealth(t *testing.T) {
	registry := health.NewRegistry()
	rolledBack := false
	runtime := application.NewRuntime(
		registry,
		infraLogger.NewNop(),
		fx.Invoke(func(lifecycle fx.Lifecycle) {
			lifecycle.Append(fx.Hook{
				OnStart: func(context.Context) error { return nil },
				OnStop: func(context.Context) error {
					rolledBack = true
					return nil
				},
			})
			lifecycle.Append(fx.Hook{
				OnStart: func(context.Context) error {
					return errors.New("password=secret-value")
				},
			})
		}),
	)

	if err := runtime.Start(context.Background()); err == nil {
		t.Fatal("启动失败场景必须返回错误")
	}
	if !rolledBack {
		t.Fatal("启动失败后已启动组件未回收")
	}
	snapshot := registry.Get()
	if snapshot.Status != health.StatusFailed || snapshot.ErrorCode != apperr.CodeInternal.Value {
		t.Fatalf("失败健康状态不正确：got %#v", snapshot)
	}
	if snapshot.Message != apperr.CodeInternal.Message {
		t.Fatalf("健康状态泄漏内部错误：got %q", snapshot.Message)
	}
}
