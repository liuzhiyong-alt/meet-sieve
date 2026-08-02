package bootstrap_test

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/domain/metadata"
	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
	"meet-sieve/internal/service/workspace"
)

// TestCoordinator_StartWithoutLocatorDoesNotOpenWorkspace 验证 locator 缺失仅进入首次选择状态，不打开 SQLite 或创建目录。
func TestCoordinator_StartWithoutLocatorDoesNotOpenWorkspace(t *testing.T) {
	coordinator := bootstrap.NewCoordinator(bootstrap.Dependencies{
		Locator: missingLocator{},
	})
	state := coordinator.Start()
	if state.Phase != domainworkspace.BootstrapPhaseNeedsWorkspace {
		t.Fatalf("缺少 locator 应进入 needs_workspace：%+v", state)
	}
	if len(state.AvailableActions) != 1 || state.AvailableActions[0] != domainworkspace.BootstrapActionSelectWorkspace {
		t.Fatalf("首次启动只应允许选择工作目录：%+v", state)
	}
}

// TestCoordinator_StartCurrentWorkspaceOpensRuntime 验证合法当前工作目录通过完整检查后才打开运行时 SQLite 并进入 ready。
func TestCoordinator_StartCurrentWorkspaceOpensRuntime(t *testing.T) {
	locatorPath := filepath.Join(t.TempDir(), "config.json")
	locator := config.NewLocatorStore(locatorPath)
	workspacePath := filepath.Join(t.TempDir(), "workspace")
	service, finalizer := newWorkspaceServiceForBootstrapTest(t, locator)
	if _, err := service.UseWorkspace(workspacePath); err != nil {
		t.Fatalf("准备当前工作目录失败：%v", err)
	}
	coordinator := bootstrap.NewCoordinator(bootstrap.Dependencies{
		Locator:        locator,
		Inspector:      workspaceInspectorForBootstrapTest(t),
		Workspace:      service,
		Migration:      migration.NewMigrationCoordinator(migration.NewBackupService(clock.NewSystem()), finalizer, clock.NewSystem(), func(string) (uint64, error) { return math.MaxUint64, nil }),
		DatabaseConfig: config.DatabaseConfig{BusyTimeoutMS: 5000, ReadMaxOpenConns: 2, ReadMaxIdleConns: 1, WriteQueueCapacity: config.Step1WriteQueueCapacity},
		JournalPath:    filepath.Join(t.TempDir(), "database-migration.json"),
		OperationID:    identity.NewUUIDGenerator(),
	})
	t.Cleanup(func() { _ = coordinator.Stop() })

	state := coordinator.Start()
	if state.Phase != domainworkspace.BootstrapPhaseReady {
		t.Fatalf("当前工作目录应进入 ready：%+v", state)
	}
}

// missingLocator 是 locator 文件不存在时的系统边界 fixture，不涉及工作目录或 SQLite。
type missingLocator struct{}

// Load 模拟系统 locator 尚未配置。
func (missingLocator) Load() (config.Locator, bool, error) {
	return config.Locator{}, false, nil
}

// newWorkspaceServiceForBootstrapTest 用真实 Inspector、Initializer 与 locator 准备独立工作目录服务。
func newWorkspaceServiceForBootstrapTest(t *testing.T, locator *config.LocatorStore) (*workspace.WorkspaceService, *migration.FoundationFinalizer) {
	t.Helper()
	inspector := workspaceInspectorForBootstrapTest(t)
	currentClock := clock.NewFixed(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	finalizer := migration.NewFoundationFinalizer(identity.NewUUIDGenerator(), metadata.NewSecureDeviceCodeGenerator(), currentClock, "0.1.0-test")
	initializer := workspace.NewInitializer(inspector, finalizer, identity.NewUUIDGenerator())
	return workspace.NewWorkspaceService(inspector, initializer, locator, "", "", nil), finalizer
}

// workspaceInspectorForBootstrapTest 构造只依赖临时目录与固定本地卷/空间边界的真实目录检查器。
func workspaceInspectorForBootstrapTest(t *testing.T) *workspace.Inspector {
	t.Helper()
	installPath := filepath.Join(t.TempDir(), "install")
	if err := filesystem.ProbeWritable(filepath.Dir(installPath)); err != nil {
		t.Fatalf("测试安装目录父级不可写：%v", err)
	}
	canonicalInstall, err := filesystem.CanonicalizePath(installPath)
	if err != nil {
		t.Fatalf("规范化测试安装目录失败：%v", err)
	}
	policy := workspace.NewPathPolicy(canonicalInstall, func(filesystem.CanonicalPath) (filesystem.VolumeKind, error) {
		return filesystem.VolumeLocal, nil
	})
	return workspace.NewInspector(policy, func(string) (uint64, error) { return math.MaxUint64, nil })
}
