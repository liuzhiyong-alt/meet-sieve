package apperr

var (
	CodeQueryCursorInvalid       = Code{Value: 400, ErrorCode: "QUERY_CURSOR_INVALID", Message: "会议记录页码无效，已返回第一页", Kind: KindValidation}
	CodeQueryCursorFilterChanged = Code{Value: 409, ErrorCode: "QUERY_CURSOR_FILTER_CHANGED", Message: "筛选条件已变化，已返回第一页", Kind: KindBusiness, Retryable: true}
	CodeMeetingNotFound          = Code{Value: 404, ErrorCode: "MEETING_NOT_FOUND", Message: "会议不存在或已被删除", Kind: KindBusiness}
	CodeMeetingMaintenanceLocked = Code{Value: 409, ErrorCode: "MEETING_MAINTENANCE_LOCKED", Message: "会议正在执行维护操作，请稍后重试", Kind: KindBusiness, Retryable: true}
	CodeRecoveryNotAllowed       = Code{Value: 409, ErrorCode: "RECOVERY_NOT_ALLOWED", Message: "当前会议不需要恢复", Kind: KindBusiness}
	CodeDeleteTaskStopTimeout    = Code{Value: 409, ErrorCode: "DELETE_TASK_STOP_TIMEOUT", Message: "会议任务尚未安全停止，未删除任何文件", Kind: KindBusiness, Retryable: true}
	CodeDeletePreviewStale       = Code{Value: 409, ErrorCode: "DELETE_PREVIEW_STALE", Message: "会议文件已变化，请重新预览后确认", Kind: KindBusiness, Retryable: true}
	CodeDeleteManifestInvalid    = Code{Value: 500, ErrorCode: "DELETE_MANIFEST_INVALID", Message: "删除清单无效，未删除任何文件", Kind: KindSystem}
	CodeDeletePathOutsideMeeting = Code{Value: 409, ErrorCode: "DELETE_PATH_OUTSIDE_MEETING", Message: "删除目标不在会议目录内，操作已阻止", Kind: KindBusiness}
	CodeDeleteSpecialFileBlocked = Code{Value: 409, ErrorCode: "DELETE_SPECIAL_FILE_BLOCKED", Message: "会议目录包含不支持的文件类型，操作已阻止", Kind: KindBusiness}
	CodeDeleteItemBusy           = Code{Value: 409, ErrorCode: "DELETE_ITEM_BUSY", Message: "部分文件正在使用，删除未完成", Kind: KindBusiness, Retryable: true}
	CodeDeleteInterrupted        = Code{Value: 500, ErrorCode: "DELETE_INTERRUPTED", Message: "上次删除被中断，请检查后重试", Kind: KindSystem, Retryable: true}
	CodeDeletePersistTimeout     = Code{Value: 500, ErrorCode: "DELETE_PERSIST_TIMEOUT", Message: "删除进度尚未安全保存，暂时不能退出", Kind: KindSystem, Retryable: true}
	CodeStorageScanRunning       = Code{Value: 409, ErrorCode: "STORAGE_SCAN_RUNNING", Message: "存储扫描正在进行", Kind: KindBusiness}
	CodeStorageScanFailed        = Code{Value: 500, ErrorCode: "STORAGE_SCAN_FAILED", Message: "存储扫描未完成，请重试", Kind: KindSystem, Retryable: true}
	CodeDiagnosticTargetInvalid  = Code{Value: 400, ErrorCode: "DIAGNOSTIC_TARGET_INVALID", Message: "诊断文件保存位置无效", Kind: KindValidation}
	CodeDiagnosticExportFailed   = Code{Value: 500, ErrorCode: "DIAGNOSTIC_EXPORT_FAILED", Message: "诊断文件导出失败，请重试", Kind: KindSystem, Retryable: true}
	CodeResourceMissing          = Code{Value: 404, ErrorCode: "RESOURCE_MISSING", Message: "附件文件不存在", Kind: KindBusiness}
	CodeResourceChanged          = Code{Value: 409, ErrorCode: "RESOURCE_CHANGED", Message: "附件内容已发生变化，已阻止打开", Kind: KindBusiness}
	CodeResourceOutsideWorkspace = Code{Value: 409, ErrorCode: "RESOURCE_OUTSIDE_WORKSPACE", Message: "附件不在工作目录内，已阻止打开", Kind: KindBusiness}
	CodeResourceOpenFailed       = Code{Value: 500, ErrorCode: "RESOURCE_OPEN_FAILED", Message: "无法使用系统应用打开附件", Kind: KindSystem, Retryable: true}
	CodePeopleMemberReferenced   = Code{Value: 409, ErrorCode: "PEOPLE_MEMBER_REFERENCED", Message: "成员已被历史会议引用，只能归档", Kind: KindBusiness}
	CodePeopleRevisionConflict   = Code{Value: 409, ErrorCode: "PEOPLE_REVISION_CONFLICT", Message: "人员资料已变化，请刷新后重试", Kind: KindBusiness, Retryable: true}
)
