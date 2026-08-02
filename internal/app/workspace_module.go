package app

import (
	"fmt"
	"path/filepath"

	appbootstrap "meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/domain/metadata"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
	"meet-sieve/internal/service/workspace"
)

// WorkspaceModule 是 Step 1 工作目录、升级与 SQLite runtime 的应用装配结果。
type WorkspaceModule struct {
	Coordinator *appbootstrap.Coordinator
}

// NewWorkspaceModule 显式装配 Step 1 组件；构造不创建工作目录或打开 SQLite。
func NewWorkspaceModule(cfg config.Config, appVersion string) (*WorkspaceModule, error) {
	locator, err := config.NewSystemLocatorStore()
	if err != nil {
		return nil, fmt.Errorf("创建系统 locator 失败: %w", err)
	}
	installRoot, err := filesystem.CurrentInstallRoot()
	if err != nil {
		return nil, fmt.Errorf("读取应用安装目录失败: %w", err)
	}
	configDirectory, err := filesystem.CurrentAppConfigDir()
	if err != nil {
		return nil, fmt.Errorf("读取系统应用目录失败: %w", err)
	}

	currentClock := clock.NewSystem()
	finalizer := migration.NewFoundationFinalizer(
		identity.NewUUIDGenerator(),
		metadata.NewSecureDeviceCodeGenerator(),
		currentClock,
		appVersion,
	)
	pathPolicy := workspace.NewPathPolicy(installRoot, nil)
	inspector := workspace.NewInspector(pathPolicy, nil)
	initializer := workspace.NewInitializer(inspector, finalizer, identity.NewUUIDGenerator())
	workspaceService := workspace.NewWorkspaceService(inspector, initializer, locator, "", "", nil)
	migrationCoordinator := migration.NewMigrationCoordinator(
		migration.NewBackupService(currentClock), finalizer, currentClock, nil,
	)

	return &WorkspaceModule{Coordinator: appbootstrap.NewCoordinator(appbootstrap.Dependencies{
		Locator:        locator,
		Inspector:      inspector,
		Workspace:      workspaceService,
		Migration:      migrationCoordinator,
		DatabaseConfig: cfg.Database,
		JournalPath:    filepath.Join(configDirectory, "operations", "database-migration.json"),
		OperationID:    identity.NewUUIDGenerator(),
	})}, nil
}
