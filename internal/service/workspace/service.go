package workspace

import (
	"fmt"
	"path/filepath"
	"sync"

	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"
)

// LocatorStore 定义工作目录 locator 的最小持久化边界。
type LocatorStore interface {
	Load() (config.Locator, bool, error)
	Save(config.Locator) error
}

// MeetingInProgress 用于服务端再次确认当前会议是否阻止更改工作目录。
type MeetingInProgress func() bool

// WorkspaceService 编排候选检查、初始化、locator 保存与当前/下次启动路径语义。
type WorkspaceService struct {
	inspector         *Inspector
	initializer       *Initializer
	locatorStore      LocatorStore
	meetingInProgress MeetingInProgress
	mu                sync.RWMutex
	activePath        string
	savedPath         string
}

// NewWorkspaceService 创建工作目录服务；active 与 saved 路径由 bootstrap 从真实运行状态传入。
func NewWorkspaceService(
	inspector *Inspector,
	initializer *Initializer,
	locatorStore LocatorStore,
	activePath string,
	savedPath string,
	meetingInProgress MeetingInProgress,
) *WorkspaceService {
	return &WorkspaceService{
		inspector:         inspector,
		initializer:       initializer,
		locatorStore:      locatorStore,
		meetingInProgress: meetingInProgress,
		activePath:        activePath,
		savedPath:         savedPath,
	}
}

// UseWorkspace 在首次或故障恢复流程中接入路径；成功后当前进程立即使用该工作目录。
func (service *WorkspaceService) UseWorkspace(path string) (domainworkspace.WorkspaceSettings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	candidate, err := service.prepareUsableCandidate(path)
	if err != nil {
		return service.currentSettingsLocked(), err
	}
	if err := service.saveLocator(candidate.Path); err != nil {
		return service.currentSettingsLocked(), err
	}
	service.activePath = candidate.Path
	service.savedPath = candidate.Path
	return service.currentSettingsLocked(), nil
}

// SaveWorkspacePath 保存下次启动使用的路径，绝不在正常设置流程切换当前数据库。
func (service *WorkspaceService) SaveWorkspacePath(path string) (domainworkspace.WorkspaceSettings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.isMeetingInProgress() {
		return service.currentSettingsLocked(), apperr.Biz(apperr.CodeWorkspaceChangeBlocked, apperr.WithOp("workspace.save.meeting"))
	}
	candidate, err := service.prepareUsableCandidate(path)
	if err != nil {
		return service.currentSettingsLocked(), err
	}
	if err := service.saveLocator(candidate.Path); err != nil {
		return service.currentSettingsLocked(), err
	}
	service.savedPath = candidate.Path
	return service.currentSettingsLocked(), nil
}

// GetSettings 返回当前进程与下次启动路径的独立投影，不触发文件系统或数据库副作用。
func (service *WorkspaceService) GetSettings() domainworkspace.WorkspaceSettings {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.currentSettingsLocked()
}

// AdoptActiveWorkspace 由 bootstrap 在目录和 SQLite runtime 均已验证后登记当前进程实际使用的路径。
func (service *WorkspaceService) AdoptActiveWorkspace(path string) {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.activePath = path
	service.savedPath = path
	service.mu.Unlock()
}

// SetChangeBlocker 接入会议、收尾和删除中的运行时安全门。
func (service *WorkspaceService) SetChangeBlocker(blocker MeetingInProgress) {
	if service == nil {
		return
	}
	service.mu.Lock()
	service.meetingInProgress = blocker
	service.mu.Unlock()
}

// prepareUsableCandidate 只初始化 missing/empty，接入合法当前 schema，并拒绝其他所有状态。
func (service *WorkspaceService) prepareUsableCandidate(path string) (domainworkspace.WorkspaceCandidate, error) {
	if service == nil || service.inspector == nil || service.initializer == nil || service.locatorStore == nil {
		return domainworkspace.WorkspaceCandidate{}, fmt.Errorf("工作目录服务依赖不完整")
	}
	candidate := service.inspector.Inspect(path)
	switch candidate.Kind {
	case domainworkspace.CandidateKindMissing, domainworkspace.CandidateKindEmpty:
		return service.initializer.Initialize(path)
	case domainworkspace.CandidateKindMeetSieve:
		if candidate.SchemaState != domainworkspace.SchemaStateCurrent {
			return candidate, apperr.Biz(apperr.CodeDatabaseMigrationFailed, apperr.WithOp("workspace.prepare.schema"))
		}
		if err := ensureExistingWorkspaceDirectories(candidate.Path); err != nil {
			return candidate, err
		}
		return candidate, nil
	default:
		return candidate, candidateAppError(candidate.Reason)
	}
}

// ensureExistingWorkspaceDirectories 只在合法 identity 确认后补建缺失的登记目录。
func ensureExistingWorkspaceDirectories(workspacePath string) error {
	for _, directory := range []string{
		filepath.Join(workspacePath, "data", "backups"),
		filepath.Join(workspacePath, "data", "voice-samples"),
		filepath.Join(workspacePath, "meetings"),
	} {
		if err := createRegisteredDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

// saveLocator 按最小 locator 格式持久化路径；失败时不改内存 active/saved 状态。
func (service *WorkspaceService) saveLocator(path string) error {
	if err := service.locatorStore.Save(config.Locator{SchemaVersion: config.LocatorSchemaVersion, WorkspacePath: path}); err != nil {
		return apperr.Dependency(apperr.CodeLocatorWriteFailed, err, apperr.WithOp("workspace.locator.save"))
	}
	return nil
}

// currentSettingsLocked 在持锁调用点构造设置页 DTO，准确表达当前与下次启动路径。
func (service *WorkspaceService) currentSettingsLocked() domainworkspace.WorkspaceSettings {
	editable := !service.isMeetingInProgress()
	settings := domainworkspace.WorkspaceSettings{
		ActivePath:      service.activePath,
		SavedPath:       service.savedPath,
		RestartRequired: service.activePath != service.savedPath,
		Editable:        editable,
	}
	if !editable {
		settings.DisabledReason = domainworkspace.WorkspaceEditDisabledReasonMeetingInProgress
	}
	return settings
}

// isMeetingInProgress 在未注入会议状态服务的 Step 1 环境中默认允许修改。
func (service *WorkspaceService) isMeetingInProgress() bool {
	return service.meetingInProgress != nil && service.meetingInProgress()
}
