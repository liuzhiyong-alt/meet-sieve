package app_test

import (
	"context"
	"testing"
	"time"

	application "meet-sieve/internal/app"
	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/config"
	infraLogger "meet-sieve/internal/infra/logger"
	"meet-sieve/internal/port"
)

// TestMeetingModuleCurrentDoesNotDeadlockWithWorkspaceBlocker 验证服务构建不会反向获取自身锁。
func TestMeetingModuleCurrentDoesNotDeadlockWithWorkspaceBlocker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := config.LoadDefault("1.26.0")
	if err != nil {
		t.Fatalf("读取默认配置失败：%v", err)
	}
	workspaceModule, err := application.NewWorkspaceModule(cfg, "test")
	if err != nil {
		t.Fatalf("创建工作目录模块失败：%v", err)
	}
	workspacePath := t.TempDir()
	if _, err := workspaceModule.Coordinator.UseWorkspace(workspacePath); err != nil {
		t.Fatalf("初始化临时工作目录失败：%v", err)
	}

	meetingModule := application.NewMeetingModule(
		workspaceModule.Coordinator,
		stubAudioCapture{},
		cfg.Recording,
		cfg.ASR,
		infraLogger.NewNop(),
		health.NewRegistry(),
	)
	workspaceModule.Coordinator.SetWorkspaceChangeBlocker(func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return meetingModule.HasUnsafeWorkspaceChange(ctx)
	})

	done := make(chan error, 1)
	go func() {
		_, currentErr := meetingModule.Current()
		done <- currentErr
	}()
	select {
	case currentErr := <-done:
		if currentErr != nil {
			t.Fatalf("构建会议服务失败：%v", currentErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("会议服务构建与工作目录阻断回调发生死锁")
	}

	meetingModule.StopAgentRuntime(context.Background())
	meetingModule.StopSpeakerAutomation()
	if err := workspaceModule.Coordinator.Stop(); err != nil {
		t.Fatalf("停止临时工作目录失败：%v", err)
	}
}

// stubAudioCapture 只满足服务装配，不启动真实音频设备。
type stubAudioCapture struct{}

// ListInputDevices 返回空设备列表。
func (stubAudioCapture) ListInputDevices(context.Context) ([]port.InputDevice, error) {
	return []port.InputDevice{}, nil
}

// TestInputDevice 不执行真实设备探测。
func (stubAudioCapture) TestInputDevice(context.Context, string) error { return nil }

// Start 不应在只读服务装配测试中被调用。
func (stubAudioCapture) Start(context.Context, string, port.AudioFormat) (port.AudioStream, error) {
	return nil, nil
}
