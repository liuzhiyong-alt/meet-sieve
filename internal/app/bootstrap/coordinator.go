package bootstrap

import (
	"fmt"
	"path/filepath"
	"sync"

	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/service/migration"
	"meet-sieve/internal/service/workspace"

	"gorm.io/gorm"
)

// LocatorReader 定义 bootstrap 所需的只读 locator 边界。
type LocatorReader interface {
	Load() (config.Locator, bool, error)
}

// BusinessDatabase 返回当前 ready 工作目录的 reader 与单 writer 事务入口。
func (coordinator *Coordinator) BusinessDatabase() (*gorm.DB, *database.TransactionManager, error) {
	if coordinator == nil {
		return nil, nil, fmt.Errorf("工作目录协调器不可用")
	}
	coordinator.mu.RLock()
	runtime := coordinator.runtime
	dispatcher := coordinator.dispatcher
	state := coordinator.state
	coordinator.mu.RUnlock()
	if state.Phase != domainworkspace.BootstrapPhaseReady || runtime == nil || dispatcher == nil {
		return nil, nil, fmt.Errorf("工作目录尚未就绪")
	}
	return runtime.Reader(), database.NewDispatchedTransactionManager(dispatcher), nil
}

// Dependencies 是 bootstrap 的显式依赖集合，单实例所有权由桌面入口在构造前取得。
type Dependencies struct {
	Locator        LocatorReader
	Inspector      *workspace.Inspector
	Workspace      *workspace.WorkspaceService
	Migration      *migration.MigrationCoordinator
	DatabaseConfig config.DatabaseConfig
	JournalPath    string
	OperationID    identity.Generator
}

// Coordinator 编排 locator、目录检查、数据库升级和 SQLite runtime 生命周期。
type Coordinator struct {
	locator        LocatorReader
	inspector      *workspace.Inspector
	workspace      *workspace.WorkspaceService
	migration      *migration.MigrationCoordinator
	databaseConfig config.DatabaseConfig
	journalPath    string
	operationID    identity.Generator
	runtime        *database.Runtime
	dispatcher     *database.WriteDispatcher

	mu    sync.RWMutex
	state State
}

// NewCoordinator 创建启动协调器，构造过程不读写 locator、工作目录或数据库。
func NewCoordinator(dependencies Dependencies) *Coordinator {
	return &Coordinator{
		locator:        dependencies.Locator,
		inspector:      dependencies.Inspector,
		workspace:      dependencies.Workspace,
		migration:      dependencies.Migration,
		databaseConfig: dependencies.DatabaseConfig,
		journalPath:    dependencies.JournalPath,
		operationID:    dependencies.OperationID,
		state:          State{Phase: domainworkspace.BootstrapPhaseCheckingWorkspace},
	}
}

// Start 从 journal 和 locator 重建启动状态；不存在 locator 时不打开 SQLite 或创建目录。
func (coordinator *Coordinator) Start() State {
	if coordinator == nil || coordinator.locator == nil {
		return State{Phase: domainworkspace.BootstrapPhaseFatal, Message: "工作目录配置不可用"}
	}
	if state, blocked := coordinator.recoverPendingSwitch(); blocked {
		return state
	}
	locator, configured, err := coordinator.locator.Load()
	if err != nil {
		return coordinator.setUnavailableState(domainworkspace.CandidateReasonInvalidPath, false)
	}
	if !configured {
		return coordinator.setState(State{
			Phase:            domainworkspace.BootstrapPhaseNeedsWorkspace,
			AvailableActions: []domainworkspace.BootstrapAction{domainworkspace.BootstrapActionSelectWorkspace},
		})
	}
	return coordinator.startConfiguredWorkspace(locator.WorkspacePath)
}

// UseWorkspace 在首次或故障状态接入路径，并在同一进程继续打开真实 SQLite runtime。
func (coordinator *Coordinator) UseWorkspace(path string) (State, error) {
	if coordinator == nil || coordinator.workspace == nil || coordinator.inspector == nil || coordinator.migration == nil || coordinator.operationID == nil {
		return State{}, fmt.Errorf("工作目录服务不可用")
	}
	if candidate := coordinator.inspector.Inspect(path); candidate.SchemaState == domainworkspace.SchemaStateUpgradeRequired {
		coordinator.setState(State{Phase: domainworkspace.BootstrapPhaseUpgradingDatabase})
		if _, err := coordinator.migration.Upgrade(migration.MigrationRequest{
			WorkspacePath: candidate.Path,
			JournalPath:   coordinator.journalPath,
			OperationID:   coordinator.operationID.New(),
		}); err != nil {
			return coordinator.setUnavailableState(domainworkspace.CandidateReasonDatabaseInvalid, true), err
		}
	}
	coordinator.setState(State{Phase: domainworkspace.BootstrapPhaseInitializingWorkspace})
	if _, err := coordinator.workspace.UseWorkspace(path); err != nil {
		return coordinator.GetState(), err
	}
	return coordinator.Start(), nil
}

// SaveWorkspacePath 保存下次启动路径，运行中的数据库不会切换。
func (coordinator *Coordinator) SaveWorkspacePath(path string) (domainworkspace.WorkspaceSettings, error) {
	if coordinator == nil || coordinator.workspace == nil {
		return domainworkspace.WorkspaceSettings{}, fmt.Errorf("工作目录服务不可用")
	}
	return coordinator.workspace.SaveWorkspacePath(path)
}

// GetWorkspaceSettings 返回当前/下次启动路径，不检查目录或打开数据库。
func (coordinator *Coordinator) GetWorkspaceSettings() domainworkspace.WorkspaceSettings {
	if coordinator == nil || coordinator.workspace == nil {
		return domainworkspace.WorkspaceSettings{}
	}
	return coordinator.workspace.GetSettings()
}

// InspectWorkspaceCandidate 仅执行轻量只读候选检查。
func (coordinator *Coordinator) InspectWorkspaceCandidate(path string) domainworkspace.WorkspaceCandidate {
	if coordinator == nil || coordinator.inspector == nil {
		return domainworkspace.WorkspaceCandidate{Kind: domainworkspace.CandidateKindInvalid, Reason: domainworkspace.CandidateReasonInvalidPath, SchemaState: domainworkspace.SchemaStateNone}
	}
	return coordinator.inspector.Inspect(path)
}

// RetryDatabaseUpgrade 从持久 locator 重新开始状态构建，避免使用内存旧路径或旧连接。
func (coordinator *Coordinator) RetryDatabaseUpgrade() State {
	return coordinator.Start()
}

// Stop 先拒绝新写并排空队列，再关闭 SQLite writer/read 池。
func (coordinator *Coordinator) Stop() error {
	if coordinator == nil {
		return nil
	}
	coordinator.mu.Lock()
	dispatcher := coordinator.dispatcher
	runtime := coordinator.runtime
	coordinator.dispatcher = nil
	coordinator.runtime = nil
	coordinator.mu.Unlock()
	if dispatcher != nil {
		if err := dispatcher.Close(); err != nil {
			return err
		}
	}
	if runtime != nil {
		return runtime.Close()
	}
	return nil
}

// GetState 返回不产生文件、数据库或迁移副作用的当前启动状态值副本。
func (coordinator *Coordinator) GetState() State {
	if coordinator == nil {
		return State{Phase: domainworkspace.BootstrapPhaseFatal, Message: "工作目录配置不可用"}
	}
	coordinator.mu.RLock()
	defer coordinator.mu.RUnlock()
	return cloneState(coordinator.state)
}

// startConfiguredWorkspace 按检查、升级、完整性检查、启动 runtime 的固定顺序推进已保存目录。
func (coordinator *Coordinator) startConfiguredWorkspace(path string) State {
	if coordinator.inspector == nil || coordinator.workspace == nil || coordinator.migration == nil || coordinator.operationID == nil {
		return coordinator.setFatalState("工作目录启动依赖不完整")
	}
	coordinator.setState(State{Phase: domainworkspace.BootstrapPhaseCheckingWorkspace})
	candidate := coordinator.inspector.Inspect(path)
	if candidate.Kind == domainworkspace.CandidateKindInvalid {
		return coordinator.setUnavailableState(candidate.Reason, false)
	}
	if candidate.Kind != domainworkspace.CandidateKindMeetSieve {
		return coordinator.setUnavailableState(domainworkspace.CandidateReasonDatabaseInvalid, false)
	}
	if candidate.SchemaState == domainworkspace.SchemaStateUpgradeRequired {
		coordinator.setState(State{Phase: domainworkspace.BootstrapPhaseUpgradingDatabase})
		if _, err := coordinator.migration.Upgrade(migration.MigrationRequest{
			WorkspacePath: candidate.Path,
			JournalPath:   coordinator.journalPath,
			OperationID:   coordinator.operationID.New(),
		}); err != nil {
			return coordinator.setUnavailableState(domainworkspace.CandidateReasonDatabaseInvalid, true)
		}
		candidate = coordinator.inspector.Inspect(candidate.Path)
	}
	if candidate.SchemaState != domainworkspace.SchemaStateCurrent {
		return coordinator.setUnavailableState(candidate.Reason, false)
	}
	if err := coordinator.openWorkspaceRuntime(candidate.Path); err != nil {
		return coordinator.setUnavailableState(domainworkspace.CandidateReasonDatabaseInvalid, true)
	}
	coordinator.workspace.AdoptActiveWorkspace(candidate.Path)
	return coordinator.setState(State{Phase: domainworkspace.BootstrapPhaseReady})
}

// openWorkspaceRuntime 仅在当前 schema/identity 确认后打开 reader、writer 并执行 SQLite 完整性检查。
func (coordinator *Coordinator) openWorkspaceRuntime(workspacePath string) error {
	if err := coordinator.Stop(); err != nil {
		return err
	}
	runtime, err := database.OpenRuntime(filepath.Join(workspacePath, "data", "meetings.db"), coordinator.databaseConfig)
	if err != nil {
		return err
	}
	if _, err := database.CheckSQLite(runtime.Reader()); err != nil {
		_ = runtime.Close()
		return err
	}
	dispatcher, err := database.NewWriteDispatcher(runtime.Writer(), coordinator.databaseConfig.WriteQueueCapacity)
	if err != nil {
		_ = runtime.Close()
		return err
	}
	coordinator.mu.Lock()
	coordinator.runtime = runtime
	coordinator.dispatcher = dispatcher
	coordinator.mu.Unlock()
	return nil
}

// recoverPendingSwitch 在读取 locator 前处理系统目录内已登记的数据库文件切换现场。
func (coordinator *Coordinator) recoverPendingSwitch() (State, bool) {
	if coordinator.migration == nil || coordinator.journalPath == "" {
		return State{}, false
	}
	if _, err := coordinator.migration.Recover(coordinator.journalPath); err != nil {
		return coordinator.setUnavailableState(domainworkspace.CandidateReasonDatabaseInvalid, false), true
	}
	return State{}, false
}

// setUnavailableState 统一投影脱敏的工作目录阻断状态。
func (coordinator *Coordinator) setUnavailableState(reason domainworkspace.CandidateReason, retryable bool) State {
	actions := []domainworkspace.BootstrapAction{domainworkspace.BootstrapActionSelectWorkspace, domainworkspace.BootstrapActionQuit}
	if retryable {
		actions = append([]domainworkspace.BootstrapAction{domainworkspace.BootstrapActionRetryDatabaseUpgrade}, actions...)
	}
	return coordinator.setState(State{
		Phase:            domainworkspace.BootstrapPhaseWorkspaceUnavailable,
		Reason:           reason,
		Message:          unavailableMessage(reason),
		Retryable:        retryable,
		AvailableActions: actions,
	})
}

// setFatalState 用于缺少基础依赖等不能通过重新选择目录恢复的错误。
func (coordinator *Coordinator) setFatalState(message string) State {
	return coordinator.setState(State{Phase: domainworkspace.BootstrapPhaseFatal, Message: message})
}

// setState 在一次状态转换后返回安全副本。
func (coordinator *Coordinator) setState(state State) State {
	coordinator.mu.Lock()
	coordinator.state = cloneState(state)
	coordinator.mu.Unlock()
	return state
}

// unavailableMessage 保持技术方案规定的用户文案，不暴露路径、SQL 或驱动错误。
func unavailableMessage(reason domainworkspace.CandidateReason) string {
	switch reason {
	case domainworkspace.CandidateReasonSchemaNewer:
		return "该工作目录由更高版本的 MeetSieve 创建，请升级应用后重试"
	case domainworkspace.CandidateReasonNotWritable:
		return "目录没有写入权限"
	case domainworkspace.CandidateReasonDatabaseMissing:
		return "无法找到 data/meetings.db"
	case domainworkspace.CandidateReasonInvalidPath:
		return "工作目录配置无效，请重新选择工作目录"
	default:
		return "无法打开工作目录中的数据库"
	}
}
