package workspace

// BootstrapPhase 表示应用启动到可用工作目录前的稳定阶段。
type BootstrapPhase string

const (
	// BootstrapPhaseCheckingInstance 表示正在检查应用单实例。
	BootstrapPhaseCheckingInstance BootstrapPhase = "checking_instance"
	// BootstrapPhaseNeedsWorkspace 表示尚未保存工作目录。
	BootstrapPhaseNeedsWorkspace BootstrapPhase = "needs_workspace"
	// BootstrapPhaseCheckingWorkspace 表示正在轻量校验工作目录。
	BootstrapPhaseCheckingWorkspace BootstrapPhase = "checking_workspace"
	// BootstrapPhaseInitializingWorkspace 表示正在初始化空工作目录。
	BootstrapPhaseInitializingWorkspace BootstrapPhase = "initializing_workspace"
	// BootstrapPhaseUpgradingDatabase 表示正在备份并升级数据库。
	BootstrapPhaseUpgradingDatabase BootstrapPhase = "upgrading_database"
	// BootstrapPhaseWorkspaceUnavailable 表示已保存工作目录当前不可用。
	BootstrapPhaseWorkspaceUnavailable BootstrapPhase = "workspace_unavailable"
	// BootstrapPhaseReady 表示工作目录和数据库已可用。
	BootstrapPhaseReady BootstrapPhase = "ready"
	// BootstrapPhaseFatal 表示基础配置或系统目录发生阻断错误。
	BootstrapPhaseFatal BootstrapPhase = "fatal"
)

// IsValid 判断 BootstrapPhase 是否为技术方案登记的阶段。
func (phase BootstrapPhase) IsValid() bool {
	switch phase {
	case BootstrapPhaseCheckingInstance,
		BootstrapPhaseNeedsWorkspace,
		BootstrapPhaseCheckingWorkspace,
		BootstrapPhaseInitializingWorkspace,
		BootstrapPhaseUpgradingDatabase,
		BootstrapPhaseWorkspaceUnavailable,
		BootstrapPhaseReady,
		BootstrapPhaseFatal:
		return true
	default:
		return false
	}
}

// CandidateKind 表示候选工作目录的高层分类。
type CandidateKind string

const (
	// CandidateKindMissing 表示路径不存在但可能创建。
	CandidateKindMissing CandidateKind = "missing"
	// CandidateKindEmpty 表示目录真正为空，可以初始化。
	CandidateKindEmpty CandidateKind = "empty"
	// CandidateKindMeetSieve 表示目录包含有效 MeetSieve 数据库。
	CandidateKindMeetSieve CandidateKind = "meetsieve"
	// CandidateKindInvalid 表示目录不能作为工作目录使用。
	CandidateKindInvalid CandidateKind = "invalid"
)

// IsValid 判断 CandidateKind 是否为技术方案登记的分类。
func (kind CandidateKind) IsValid() bool {
	switch kind {
	case CandidateKindMissing, CandidateKindEmpty, CandidateKindMeetSieve, CandidateKindInvalid:
		return true
	default:
		return false
	}
}

// CandidateReason 表示候选工作目录被拒绝或需要处理的稳定原因。
type CandidateReason string

const (
	// CandidateReasonNone 表示候选目录没有阻断原因。
	CandidateReasonNone CandidateReason = "none"
	// CandidateReasonInvalidPath 表示路径不是可接受的绝对路径。
	CandidateReasonInvalidPath CandidateReason = "invalid_path"
	// CandidateReasonNotEmpty 表示非空目录不是 MeetSieve 工作目录。
	CandidateReasonNotEmpty CandidateReason = "not_empty"
	// CandidateReasonUnsupportedVolume 表示目录位于不支持的网络卷。
	CandidateReasonUnsupportedVolume CandidateReason = "unsupported_volume"
	// CandidateReasonInstallPathForbidden 表示目录位于应用安装目录范围。
	CandidateReasonInstallPathForbidden CandidateReason = "install_path_forbidden"
	// CandidateReasonNotWritable 表示当前用户无法读写目录。
	CandidateReasonNotWritable CandidateReason = "not_writable"
	// CandidateReasonDatabaseMissing 表示固定位置的数据库不存在。
	CandidateReasonDatabaseMissing CandidateReason = "database_missing"
	// CandidateReasonDatabaseInvalid 表示数据库身份或 SQLite 内容无效。
	CandidateReasonDatabaseInvalid CandidateReason = "database_invalid"
	// CandidateReasonSchemaNewer 表示数据库 schema 高于当前应用支持版本。
	CandidateReasonSchemaNewer CandidateReason = "schema_newer"
)

// IsValid 判断 CandidateReason 是否为技术方案登记的原因。
func (reason CandidateReason) IsValid() bool {
	switch reason {
	case CandidateReasonNone,
		CandidateReasonInvalidPath,
		CandidateReasonNotEmpty,
		CandidateReasonUnsupportedVolume,
		CandidateReasonInstallPathForbidden,
		CandidateReasonNotWritable,
		CandidateReasonDatabaseMissing,
		CandidateReasonDatabaseInvalid,
		CandidateReasonSchemaNewer:
		return true
	default:
		return false
	}
}

// SchemaState 表示候选数据库相对当前应用的 schema 状态。
type SchemaState string

const (
	// SchemaStateNone 表示当前候选没有可读取的 MeetSieve 数据库。
	SchemaStateNone SchemaState = "none"
	// SchemaStateCurrent 表示数据库 schema 已是当前版本。
	SchemaStateCurrent SchemaState = "current"
	// SchemaStateUpgradeRequired 表示数据库可以安全升级。
	SchemaStateUpgradeRequired SchemaState = "upgrade_required"
	// SchemaStateNewer 表示数据库版本高于当前应用。
	SchemaStateNewer SchemaState = "newer"
)

// IsValid 判断 SchemaState 是否为技术方案登记的状态。
func (state SchemaState) IsValid() bool {
	switch state {
	case SchemaStateNone, SchemaStateCurrent, SchemaStateUpgradeRequired, SchemaStateNewer:
		return true
	default:
		return false
	}
}

// CandidateWarning 表示不阻断使用的候选目录提示。
type CandidateWarning string

const (
	// CandidateWarningLowDiskSpace 表示可用空间低于推荐值但不阻断初始化。
	CandidateWarningLowDiskSpace CandidateWarning = "low_disk_space"
)

// IsValid 判断 CandidateWarning 是否为技术方案登记的提示。
func (warning CandidateWarning) IsValid() bool {
	return warning == CandidateWarningLowDiskSpace
}

// WorkspaceCandidate 是对单个用户路径执行轻量检查后的完整分类结果。
type WorkspaceCandidate struct {
	// Path 是通过路径策略后保存的规范化绝对路径；路径无效时为空。
	Path string
	// Kind 表示目标目录的高层类型。
	Kind CandidateKind
	// Reason 表示阻断原因；可继续使用时为 none。
	Reason CandidateReason
	// SchemaState 表示固定位置数据库相对当前应用的 schema 状态。
	SchemaState SchemaState
	// DatabaseID 仅在完整 MeetSieve 数据库中返回，不用于副本拒绝判断。
	DatabaseID string
	// Writable 表示轻量检查已确认当前用户可写；无效候选为 false。
	Writable bool
	// LocalVolume 表示路径已通过本地卷检查；无效候选为 false。
	LocalVolume bool
	// FreeBytes 是当前卷可读取时的可用空间；读取失败时为 0，不作为初始化硬阻断。
	FreeBytes uint64
	// Warnings 是不阻断使用的环境提示。
	Warnings []CandidateWarning
}

// IsValid 判断候选结果中的枚举与分类是否相互一致。
func (candidate WorkspaceCandidate) IsValid() bool {
	if !candidate.Kind.IsValid() || !candidate.Reason.IsValid() || !candidate.SchemaState.IsValid() {
		return false
	}
	for _, warning := range candidate.Warnings {
		if !warning.IsValid() {
			return false
		}
	}
	if candidate.Kind == CandidateKindInvalid {
		return candidate.Reason != CandidateReasonNone
	}
	return candidate.Reason == CandidateReasonNone
}

// BootstrapAction 表示阻断启动状态下允许的用户操作。
type BootstrapAction string

const (
	// BootstrapActionSelectWorkspace 表示选择或重新选择工作目录。
	BootstrapActionSelectWorkspace BootstrapAction = "select_workspace"
	// BootstrapActionRetryDatabaseUpgrade 表示重试自动数据库升级。
	BootstrapActionRetryDatabaseUpgrade BootstrapAction = "retry_database_upgrade"
	// BootstrapActionQuit 表示退出应用。
	BootstrapActionQuit BootstrapAction = "quit"
)

// IsValid 判断 BootstrapAction 是否为技术方案登记的操作。
func (action BootstrapAction) IsValid() bool {
	switch action {
	case BootstrapActionSelectWorkspace, BootstrapActionRetryDatabaseUpgrade, BootstrapActionQuit:
		return true
	default:
		return false
	}
}

// WorkspaceEditDisabledReason 表示设置页禁止修改工作目录的原因。
type WorkspaceEditDisabledReason string

const (
	// WorkspaceEditDisabledReasonNone 表示当前允许编辑工作目录。
	WorkspaceEditDisabledReasonNone WorkspaceEditDisabledReason = ""
	// WorkspaceEditDisabledReasonMeetingInProgress 表示会议进行中禁止修改工作目录。
	WorkspaceEditDisabledReasonMeetingInProgress WorkspaceEditDisabledReason = "meeting_in_progress"
)

// IsValid 判断 WorkspaceEditDisabledReason 是否为技术方案登记的原因。
func (reason WorkspaceEditDisabledReason) IsValid() bool {
	return reason == WorkspaceEditDisabledReasonNone || reason == WorkspaceEditDisabledReasonMeetingInProgress
}

// WorkspaceSettings 表示设置页需要的当前与下次启动工作目录状态。
type WorkspaceSettings struct {
	// ActivePath 是当前进程正在使用的工作目录。
	ActivePath string
	// SavedPath 是下一次启动将读取的工作目录。
	SavedPath string
	// RestartRequired 表示已保存路径与当前工作目录不同。
	RestartRequired bool
	// Editable 表示当前是否允许修改工作目录。
	Editable bool
	// DisabledReason 是不可编辑时的稳定原因。
	DisabledReason WorkspaceEditDisabledReason
}

// IsValid 判断工作目录设置的可编辑状态与禁用原因是否一致。
func (settings WorkspaceSettings) IsValid() bool {
	if settings.Editable {
		return settings.DisabledReason == WorkspaceEditDisabledReasonNone
	}
	return settings.DisabledReason == WorkspaceEditDisabledReasonMeetingInProgress
}
