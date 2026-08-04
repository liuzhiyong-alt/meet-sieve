package apperr

// Code 描述集中登记的稳定错误码及其默认对外语义。
type Code struct {
	// Value 是 Wails 与 HTTP 共用的稳定数值码。
	Value int
	// ErrorCode 是供客户端稳定识别错误类型的字符串码。
	ErrorCode string
	// Message 是允许直接展示给用户的安全提示。
	Message string
	// Kind 决定错误分类和默认日志等级。
	Kind Kind
	// Retryable 表示调用方是否可以默认提示重试。
	Retryable bool
}

var (
	// CodeOK 表示调用成功。
	CodeOK = Code{Value: 200, Message: "成功", Kind: KindBusiness}
	// CodeInvalidRequest 表示参数或请求不合法。
	CodeInvalidRequest = Code{Value: 400, ErrorCode: "INVALID_REQUEST", Message: "请求参数不正确", Kind: KindValidation}
	// CodeNotFound 表示目标资源不存在。
	CodeNotFound = Code{Value: 404, ErrorCode: "NOT_FOUND", Message: "目标资源不存在", Kind: KindBusiness}
	// CodeConflict 表示当前状态冲突。
	CodeConflict = Code{Value: 409, ErrorCode: "CONFLICT", Message: "当前状态不允许此操作", Kind: KindBusiness}
	// CodePayloadTooLarge 表示请求或文件超过上限。
	CodePayloadTooLarge = Code{Value: 413, ErrorCode: "PAYLOAD_TOO_LARGE", Message: "请求内容超过大小限制", Kind: KindValidation}
	// CodeCanceled 表示用户主动取消。
	CodeCanceled = Code{Value: 499, ErrorCode: "CANCELED", Message: "操作已取消", Kind: KindCanceled}
	// CodeInternal 表示未预期的内部错误。
	CodeInternal = Code{Value: 500, ErrorCode: "INTERNAL", Message: "系统内部错误", Kind: KindSystem}
	// CodeDependency 表示外部依赖调用失败。
	CodeDependency = Code{Value: 502, ErrorCode: "DEPENDENCY", Message: "外部服务暂不可用", Kind: KindDependency}
	// CodeDependencyTimeout 表示外部依赖调用超时。
	CodeDependencyTimeout = Code{Value: 504, ErrorCode: "DEPENDENCY_TIMEOUT", Message: "外部服务响应超时", Kind: KindDependency, Retryable: true}

	// CodeWorkspacePathInvalid 表示工作目录路径不是合法绝对路径。
	CodeWorkspacePathInvalid = Code{Value: 400, ErrorCode: "WORKSPACE_PATH_INVALID", Message: "工作目录路径无效", Kind: KindValidation}
	// CodeWorkspaceNotEmpty 表示非空目录不是有效 MeetSieve 工作目录。
	CodeWorkspaceNotEmpty = Code{Value: 409, ErrorCode: "WORKSPACE_NOT_EMPTY", Message: "目录不为空，且不是有效的 MeetSieve 工作目录", Kind: KindBusiness}
	// CodeWorkspaceUnsupportedVolume 表示工作目录位于不支持的网络卷。
	CodeWorkspaceUnsupportedVolume = Code{Value: 400, ErrorCode: "WORKSPACE_UNSUPPORTED_VOLUME", Message: "工作目录不能使用网络共享位置", Kind: KindValidation}
	// CodeWorkspaceInstallPathForbidden 表示工作目录位于应用安装目录或其内部。
	CodeWorkspaceInstallPathForbidden = Code{Value: 409, ErrorCode: "WORKSPACE_INSTALL_PATH_FORBIDDEN", Message: "会议工作目录不能放在 MeetSieve 安装目录中，请选择其他位置", Kind: KindBusiness}
	// CodeWorkspaceNotWritable 表示当前用户不能写入工作目录。
	CodeWorkspaceNotWritable = Code{Value: 409, ErrorCode: "WORKSPACE_NOT_WRITABLE", Message: "目录没有写入权限", Kind: KindBusiness, Retryable: true}
	// CodeWorkspaceDatabaseMissing 表示非空工作目录缺少固定位置的数据库。
	CodeWorkspaceDatabaseMissing = Code{Value: 404, ErrorCode: "WORKSPACE_DATABASE_MISSING", Message: "无法找到 data/meetings.db", Kind: KindBusiness}
	// CodeWorkspaceDatabaseInvalid 表示工作目录中的数据库无法作为 MeetSieve 数据库使用。
	CodeWorkspaceDatabaseInvalid = Code{Value: 409, ErrorCode: "WORKSPACE_DATABASE_INVALID", Message: "无法打开工作目录中的数据库", Kind: KindBusiness}
	// CodeWorkspaceSchemaNewer 表示数据库 schema 高于当前应用支持的版本。
	CodeWorkspaceSchemaNewer = Code{Value: 409, ErrorCode: "WORKSPACE_SCHEMA_NEWER", Message: "该工作目录由更高版本的 MeetSieve 创建，请升级应用后重试", Kind: KindBusiness}
	// CodeWorkspaceChangeBlocked 表示当前会议状态禁止修改工作目录。
	CodeWorkspaceChangeBlocked = Code{Value: 409, ErrorCode: "WORKSPACE_CHANGE_BLOCKED", Message: "会议进行中，不能修改工作目录", Kind: KindBusiness, Retryable: true}
	// CodeLocatorInvalid 表示系统目录中的工作目录定位配置无效。
	CodeLocatorInvalid = Code{Value: 409, ErrorCode: "LOCATOR_INVALID", Message: "工作目录配置无效，请重新选择工作目录", Kind: KindBusiness}
	// CodeLocatorWriteFailed 表示工作目录已准备好但定位配置原子写入失败。
	CodeLocatorWriteFailed = Code{Value: 500, ErrorCode: "LOCATOR_WRITE_FAILED", Message: "工作目录已准备好，但配置保存失败，请重试保存", Kind: KindSystem, Retryable: true}
	// CodeDatabaseBusy 表示 SQLite writer 队列满或锁等待超时。
	CodeDatabaseBusy = Code{Value: 409, ErrorCode: "DATABASE_BUSY", Message: "本地数据暂时忙碌，请稍后重试", Kind: KindBusiness, Retryable: true}
	// CodeDatabaseBackupFailed 表示数据库升级前的一致性备份失败。
	CodeDatabaseBackupFailed = Code{Value: 500, ErrorCode: "DATABASE_BACKUP_FAILED", Message: "本地数据备份失败，请重试", Kind: KindSystem, Retryable: true}
	// CodeDatabaseMigrationFailed 表示数据库 staging migration 失败。
	CodeDatabaseMigrationFailed = Code{Value: 500, ErrorCode: "DATABASE_MIGRATION_FAILED", Message: "本地数据升级失败，请重试", Kind: KindSystem, Retryable: true}
	// CodeDatabaseIntegrityFailed 表示 SQLite 完整性校验失败。
	CodeDatabaseIntegrityFailed = Code{Value: 500, ErrorCode: "DATABASE_INTEGRITY_FAILED", Message: "本地数据完整性校验失败", Kind: KindSystem}
	// CodeDatabaseDiskSpaceLow 表示可用空间不足以安全升级本地数据。
	CodeDatabaseDiskSpaceLow = Code{Value: 409, ErrorCode: "DATABASE_DISK_SPACE_LOW", Message: "可用空间不足，无法安全升级本地数据", Kind: KindBusiness, Retryable: true}
	// CodeMemberNotFound 表示请求的成员不存在。
	CodeMemberNotFound = Code{Value: 404, ErrorCode: "MEMBER_NOT_FOUND", Message: "成员不存在", Kind: KindBusiness}
	// CodeMemberNameConflict 表示活动成员的规范化名称重复。
	CodeMemberNameConflict = Code{Value: 409, ErrorCode: "MEMBER_NAME_CONFLICT", Message: "活动成员名称重复", Kind: KindBusiness}
	// CodeMemberHistoricallyReferenced 表示成员已被历史会议引用，只能归档。
	CodeMemberHistoricallyReferenced = Code{Value: 409, ErrorCode: "MEMBER_HISTORICALLY_REFERENCED", Message: "成员已被历史记录引用，只能归档", Kind: KindBusiness}
	// CodeGroupMemberInvalid 表示小组提交中存在重复、不存在或已归档成员。
	CodeGroupMemberInvalid = Code{Value: 400, ErrorCode: "GROUP_MEMBER_INVALID", Message: "小组成员无效", Kind: KindBusiness}
	// CodeGroupNotFound 表示请求的小组不存在。
	CodeGroupNotFound = Code{Value: 404, ErrorCode: "GROUP_NOT_FOUND", Message: "小组不存在", Kind: KindBusiness}
	// CodeGroupNameConflict 表示活动小组的规范化名称重复。
	CodeGroupNameConflict = Code{Value: 409, ErrorCode: "GROUP_NAME_CONFLICT", Message: "小组名称重复", Kind: KindBusiness}
	// CodeVoiceSampleFileInvalid 表示正式声纹样本缺失或哈希不符。
	CodeVoiceSampleFileInvalid = Code{Value: 409, ErrorCode: "VOICE_SAMPLE_FILE_INVALID", Message: "声纹样本文件缺失或已损坏", Kind: KindBusiness}
	// CodeVoiceRecordingBusy 表示已有声纹录制会话占用设备。
	CodeVoiceRecordingBusy = Code{Value: 409, ErrorCode: "VOICE_RECORDING_BUSY", Message: "已有声纹录制正在进行", Kind: KindBusiness, Retryable: true}
	// CodeVoiceDeviceUnavailable 表示指定麦克风当前不可用。
	CodeVoiceDeviceUnavailable = Code{Value: 409, ErrorCode: "VOICE_DEVICE_UNAVAILABLE", Message: "麦克风当前不可用", Kind: KindDependency, Retryable: true}
	// CodeVoicePermissionDenied 表示操作系统拒绝麦克风权限。
	CodeVoicePermissionDenied = Code{Value: 403, ErrorCode: "VOICE_PERMISSION_DENIED", Message: "麦克风权限被拒绝", Kind: KindBusiness, Retryable: true}
	// CodeVoiceWAVInvalid 表示上传内容不是受支持的整数 PCM WAV。
	CodeVoiceWAVInvalid = Code{Value: 400, ErrorCode: "VOICE_WAV_INVALID", Message: "请选择有效的 PCM WAV 文件", Kind: KindValidation}
	// CodeVoiceDurationExceeded 表示声纹音频超过 60 秒硬上限。
	CodeVoiceDurationExceeded = Code{Value: 413, ErrorCode: "VOICE_DURATION_EXCEEDED", Message: "声纹音频不能超过 60 秒", Kind: KindValidation}
	// CodeVoiceQualityRejected 表示样本没有通过当前模型质量门。
	CodeVoiceQualityRejected = Code{Value: 409, ErrorCode: "VOICE_QUALITY_REJECTED", Message: "这段录音不适合作为声纹样本", Kind: KindBusiness, Retryable: true}
	// CodeVoiceModelUnavailable 表示官方声纹模型或运行时不可用。
	CodeVoiceModelUnavailable = Code{Value: 409, ErrorCode: "VOICE_MODEL_UNAVAILABLE", Message: "声纹模型尚不可用", Kind: KindDependency, Retryable: true}
	// CodeVoiceEmbeddingFailed 表示当前样本生成向量失败。
	CodeVoiceEmbeddingFailed = Code{Value: 500, ErrorCode: "VOICE_EMBEDDING_FAILED", Message: "声纹样本处理失败，请重试", Kind: KindDependency, Retryable: true}
	// CodeVoiceSampleDeleteFailed 表示样本文件和数据库记录未能完整删除。
	CodeVoiceSampleDeleteFailed = Code{Value: 500, ErrorCode: "VOICE_SAMPLE_DELETE_FAILED", Message: "声纹样本删除未完成，请重试", Kind: KindSystem, Retryable: true}
	// CodeVoiceRebuildIncomplete 表示当前模型的历史向量仍需重建。
	CodeVoiceRebuildIncomplete = Code{Value: 409, ErrorCode: "VOICE_REBUILD_INCOMPLETE", Message: "声纹向量仍在重建中", Kind: KindBusiness, Retryable: true}
	// CodeMeetingAlreadyActive 表示当前工作目录已有准备、录音或收尾中的会议。
	CodeMeetingAlreadyActive = Code{Value: 409, ErrorCode: "MEETING_ALREADY_ACTIVE", Message: "已有会议正在准备、录音或保存", Kind: KindBusiness}
	// CodeMeetingNumberInvalid 表示会议号不符合已确认的固定格式。
	CodeMeetingNumberInvalid = Code{Value: 400, ErrorCode: "MEETING_NUMBER_INVALID", Message: "会议号格式不正确", Kind: KindValidation}
	// CodeMeetingParticipantsRequired 表示会议没有选择成员或添加临时参会者。
	CodeMeetingParticipantsRequired = Code{Value: 400, ErrorCode: "MEETING_PARTICIPANTS_REQUIRED", Message: "至少选择或添加一位参会者", Kind: KindBusiness}
	// CodeMeetingParticipantInvalid 表示参会成员或临时姓名不符合会议快照约束。
	CodeMeetingParticipantInvalid = Code{Value: 400, ErrorCode: "MEETING_PARTICIPANT_INVALID", Message: "参会者信息无效", Kind: KindBusiness}
	// CodeMeetingNumberConflict 表示会议号已被历史或当前会议使用。
	CodeMeetingNumberConflict = Code{Value: 409, ErrorCode: "MEETING_NUMBER_CONFLICT", Message: "会议号已被使用", Kind: KindBusiness}
	// CodeMeetingDirectoryConflict 表示目标会议目录已存在且不会被覆盖。
	CodeMeetingDirectoryConflict = Code{Value: 409, ErrorCode: "MEETING_DIRECTORY_CONFLICT", Message: "会议目录已存在，未覆盖任何文件", Kind: KindBusiness}
	// CodeMeetingWorkspaceUnavailable 表示会议工作目录不可写或暂时不可访问。
	CodeMeetingWorkspaceUnavailable = Code{Value: 409, ErrorCode: "MEETING_WORKSPACE_UNAVAILABLE", Message: "会议工作目录当前不可用", Kind: KindDependency, Retryable: true}
	// CodeMeetingDiskSpaceLow 表示可用空间不足以安全录音。
	CodeMeetingDiskSpaceLow = Code{Value: 409, ErrorCode: "MEETING_DISK_SPACE_LOW", Message: "可用空间不足，无法安全录音", Kind: KindBusiness, Retryable: true}
	// CodeMeetingAudioPermissionDenied 表示操作系统拒绝会议麦克风权限。
	CodeMeetingAudioPermissionDenied = Code{Value: 403, ErrorCode: "MEETING_AUDIO_PERMISSION_DENIED", Message: "麦克风权限被拒绝", Kind: KindBusiness, Retryable: true}
	// CodeMeetingAudioDeviceUnavailable 表示会议麦克风不存在或无法打开。
	CodeMeetingAudioDeviceUnavailable = Code{Value: 409, ErrorCode: "MEETING_AUDIO_DEVICE_UNAVAILABLE", Message: "选定麦克风不存在或无法打开", Kind: KindDependency, Retryable: true}
	// CodeMeetingAudioStartTimeout 表示限时内未取得真实 PCM 首帧。
	CodeMeetingAudioStartTimeout = Code{Value: 504, ErrorCode: "MEETING_AUDIO_START_TIMEOUT", Message: "限时内未取得麦克风音频", Kind: KindDependency, Retryable: true}
	// CodeMeetingAudioDiscontinuity 表示音频样本已丢失、重叠或队列溢出。
	CodeMeetingAudioDiscontinuity = Code{Value: 409, ErrorCode: "MEETING_AUDIO_DISCONTINUITY", Message: "音频连续性中断，已停止录音", Kind: KindDependency}
	// CodeMeetingRecordingWriteFailed 表示录音文件没有完整写入。
	CodeMeetingRecordingWriteFailed = Code{Value: 500, ErrorCode: "MEETING_RECORDING_WRITE_FAILED", Message: "录音文件未完整写入", Kind: KindSystem, Retryable: true}
	// CodeMeetingRecordingSyncFailed 表示录音检查点未完成持久化。
	CodeMeetingRecordingSyncFailed = Code{Value: 500, ErrorCode: "MEETING_RECORDING_SYNC_FAILED", Message: "录音文件未完成安全同步", Kind: KindSystem, Retryable: true}
	// CodeMeetingRecordingMergeFailed 表示完整录音尚未从分片生成。
	CodeMeetingRecordingMergeFailed = Code{Value: 500, ErrorCode: "MEETING_RECORDING_MERGE_FAILED", Message: "分片已保留，完整录音尚未生成", Kind: KindSystem, Retryable: true}
	// CodeMeetingRecordingIntegrityFailed 表示文件内容与已登记元数据不一致。
	CodeMeetingRecordingIntegrityFailed = Code{Value: 409, ErrorCode: "MEETING_RECORDING_INTEGRITY_FAILED", Message: "录音文件完整性校验失败", Kind: KindSystem}
	// CodeMeetingRecoveryRequired 表示异常中断的原会议不能续录。
	CodeMeetingRecoveryRequired = Code{Value: 409, ErrorCode: "MEETING_RECOVERY_REQUIRED", Message: "上次会议异常中断，不能继续原录音", Kind: KindBusiness, Retryable: true}
	// CodeMeetingRecoveryFailed 表示自动恢复未能完成但原文件仍被保留。
	CodeMeetingRecoveryFailed = Code{Value: 500, ErrorCode: "MEETING_RECOVERY_FAILED", Message: "现有录音已保留，但自动恢复未完成", Kind: KindSystem, Retryable: true}
	// CodeASRFinalInvalid 表示 provider final 事件缺少 Step 4 所需的稳定字段或样本范围非法。
	CodeASRFinalInvalid = Code{Value: 400, ErrorCode: "ASR_FINAL_INVALID", Message: "实时转写结果无效，已保留录音", Kind: KindValidation}
	// CodeASRSettingsInvalid 表示当前鉴权模式缺少必填凭据或字段动作无效。
	CodeASRSettingsInvalid = Code{Value: 400, ErrorCode: "ASR_SETTINGS_INVALID", Message: "实时转写设置不完整", Kind: KindValidation}
	// CodeASRSettingsChangeBlocked 表示会议活动期间禁止改变 ASR 凭据。
	CodeASRSettingsChangeBlocked = Code{Value: 409, ErrorCode: "ASR_SETTINGS_CHANGE_BLOCKED", Message: "会议进行中，不能修改实时转写设置", Kind: KindBusiness, Retryable: true}
	// CodeASRAuthFailed 表示火山拒绝当前凭据或资源权限。
	CodeASRAuthFailed = Code{Value: 403, ErrorCode: "ASR_AUTH_FAILED", Message: "实时转写凭据无效或服务未开通", Kind: KindDependency}
	// CodeASRConnectTimeout 表示实时 ASR WebSocket 未在时限内建立。
	CodeASRConnectTimeout = Code{Value: 504, ErrorCode: "ASR_CONNECT_TIMEOUT", Message: "实时转写连接超时", Kind: KindDependency, Retryable: true}
	// CodeASRConnectionTestFailed 表示设置页独立探测暂时失败，但不会自动重试。
	CodeASRConnectionTestFailed = Code{Value: 502, ErrorCode: "ASR_CONNECTION_TEST_FAILED", Message: "实时转写服务暂不可用，请稍后重试", Kind: KindDependency, Retryable: true}
	// CodeASRProtocolIncompatible 表示服务端响应缺少稳定 final、时间范围或停止语义。
	CodeASRProtocolIncompatible = Code{Value: 502, ErrorCode: "ASR_PROTOCOL_INCOMPATIBLE", Message: "实时转写协议暂不兼容，已保留录音", Kind: KindDependency}
	// CodeASRServiceBusy 表示火山服务限流或暂时不可用。
	CodeASRServiceBusy = Code{Value: 502, ErrorCode: "ASR_SERVICE_BUSY", Message: "实时转写服务繁忙，正在重试", Kind: KindDependency, Retryable: true}
	// CodeASREventPersistFailed 表示实时转写事件未能安全写入本地数据库。
	CodeASREventPersistFailed = Code{Value: 500, ErrorCode: "ASR_EVENT_PERSIST_FAILED", Message: "实时转写记录保存失败，录音仍在继续", Kind: KindSystem, Retryable: true}
	// CodeASREventBackpressure 表示最终转写事件队列已达到安全上限。
	CodeASREventBackpressure = Code{Value: 409, ErrorCode: "ASR_EVENT_BACKPRESSURE", Message: "实时转写处理繁忙，已保留录音", Kind: KindBusiness, Retryable: true}
	// CodeASRStreamInterrupted 表示实时转写流中断，调用方应按既定退避策略重连。
	CodeASRStreamInterrupted = Code{Value: 502, ErrorCode: "ASR_STREAM_INTERRUPTED", Message: "实时转写连接中断，正在重试", Kind: KindDependency, Retryable: true}
	// CodeAgentExecutableInvalid 表示 Codex 可执行文件不可用。
	CodeAgentExecutableInvalid = Code{Value: 409, ErrorCode: "AGENT_EXECUTABLE_INVALID", Message: "Codex 可执行文件不可用，请检查设置", Kind: KindBusiness, Retryable: true}
	// CodeAgentNotLoggedIn 表示用户尚未在 Codex 中登录。
	CodeAgentNotLoggedIn = Code{Value: 502, ErrorCode: "AGENT_NOT_LOGGED_IN", Message: "Codex 尚未登录，请先在外部完成登录", Kind: KindDependency, Retryable: true}
	// CodeAgentProtocolIncompatible 表示必要 app-server 协议契约不兼容。
	CodeAgentProtocolIncompatible = Code{Value: 502, ErrorCode: "AGENT_PROTOCOL_INCOMPATIBLE", Message: "当前 Codex 版本暂不兼容", Kind: KindDependency}
	// CodeAgentApprovalUnsupported 表示原生审批请求无法安全呈现。
	CodeAgentApprovalUnsupported = Code{Value: 502, ErrorCode: "AGENT_APPROVAL_UNSUPPORTED", Message: "Codex 审批请求无法安全处理，当前操作已拒绝", Kind: KindDependency}
	// CodeAgentApprovalExpired 表示原生审批已随 turn 终结或超时失效。
	CodeAgentApprovalExpired = Code{Value: 409, ErrorCode: "AGENT_APPROVAL_EXPIRED", Message: "这项 Codex 审批已经失效", Kind: KindBusiness}
	// CodeAgentInitializeFailed 表示当前会议的智能体初始化失败。
	CodeAgentInitializeFailed = Code{Value: 502, ErrorCode: "AGENT_INITIALIZE_FAILED", Message: "AI 暂不可用，录音和实时转写不受影响", Kind: KindDependency, Retryable: true}
	// CodeAgentBusy 表示当前会议已有正在处理的智能体任务。
	CodeAgentBusy = Code{Value: 409, ErrorCode: "AGENT_BUSY", Message: "AI 正在参与，请等待当前回答结束", Kind: KindBusiness, Retryable: true}
	// CodeAgentQuestionInvalid 表示主持人问题为空或超过上限。
	CodeAgentQuestionInvalid = Code{Value: 400, ErrorCode: "AGENT_QUESTION_INVALID", Message: "问题为空或超过大小限制", Kind: KindValidation}
	// CodeAgentWakeWordInvalid 表示唤醒词为空、包含空白或超过长度上限。
	CodeAgentWakeWordInvalid = Code{Value: 400, ErrorCode: "AGENT_WAKE_WORD_INVALID", Message: "唤醒词格式不正确", Kind: KindValidation}
	// CodeAgentTurnTimeout 表示智能体任务超过总时限。
	CodeAgentTurnTimeout = Code{Value: 504, ErrorCode: "AGENT_TURN_TIMEOUT", Message: "AI 回答超时，未保存当前结果", Kind: KindDependency, Retryable: true}
	// CodeAgentTurnCancelled 表示主持人停止了当前智能体任务。
	CodeAgentTurnCancelled = Code{Value: 499, ErrorCode: "AGENT_TURN_CANCELLED", Message: "AI 回答已停止", Kind: KindCanceled, Retryable: true}
	// CodeAgentOutputInvalid 表示 provider 最终输出未通过本地校验。
	CodeAgentOutputInvalid = Code{Value: 502, ErrorCode: "AGENT_OUTPUT_INVALID", Message: "AI 返回内容无效，未保存当前结果", Kind: KindDependency, Retryable: true}
	// CodeAgentContextFlushFailed 表示原始记录未能在调用 provider 前安全刷新。
	CodeAgentContextFlushFailed = Code{Value: 500, ErrorCode: "AGENT_CONTEXT_FLUSH_FAILED", Message: "会议原始记录尚未刷新，已暂停 AI 请求", Kind: KindSystem, Retryable: true}
	// CodeAgentThreadNotFound 表示 provider 中的既有 thread 已不存在。
	CodeAgentThreadNotFound = Code{Value: 502, ErrorCode: "AGENT_THREAD_NOT_FOUND", Message: "原 Codex 会话不存在，可以从本地会议事实恢复", Kind: KindDependency, Retryable: true}
	// CodeSpeakerModelUnavailable 表示当前声纹模型不能执行说话人自动识别。
	CodeSpeakerModelUnavailable = Code{Value: 503, ErrorCode: "SPEAKER_MODEL_UNAVAILABLE", Message: "说话人自动识别暂不可用", Kind: KindDependency, Retryable: true}
	// CodeSpeakerProfileMissing 表示当前模型尚无经过真实验证的匹配档案。
	CodeSpeakerProfileMissing = Code{Value: 409, ErrorCode: "SPEAKER_PROFILE_MISSING", Message: "当前模型尚无已验证的匹配档案", Kind: KindBusiness}
	// CodeSpeakerProfileMismatch 表示匹配档案与当前模型身份不一致。
	CodeSpeakerProfileMismatch = Code{Value: 409, ErrorCode: "SPEAKER_PROFILE_MISMATCH", Message: "说话人匹配档案与当前模型不一致", Kind: KindBusiness}
	// CodeSpeakerEvidencePending 表示对应音频尚未达到安全可读状态。
	CodeSpeakerEvidencePending = Code{Value: 409, ErrorCode: "SPEAKER_EVIDENCE_PENDING", Message: "说话人音频证据尚未准备好", Kind: KindBusiness, Retryable: true}
	// CodeSpeakerEvidenceInsufficient 表示证据不足以进行可靠的自动判断。
	CodeSpeakerEvidenceInsufficient = Code{Value: 409, ErrorCode: "SPEAKER_EVIDENCE_INSUFFICIENT", Message: "片段不足以可靠识别说话人", Kind: KindBusiness}
	// CodeSpeakerEmbeddingFailed 表示音频证据未能生成声纹特征。
	CodeSpeakerEmbeddingFailed = Code{Value: 502, ErrorCode: "SPEAKER_EMBEDDING_FAILED", Message: "说话人特征提取失败，请重试", Kind: KindDependency, Retryable: true}
	// CodeSpeakerProcessingFailed 表示后台说话人处理未能完成。
	CodeSpeakerProcessingFailed = Code{Value: 500, ErrorCode: "SPEAKER_PROCESSING_FAILED", Message: "说话人处理未完成，请重试", Kind: KindSystem, Retryable: true}
	// CodeCorrectionTargetNotFound 表示校对目标不存在或不属于指定会议。
	CodeCorrectionTargetNotFound = Code{Value: 404, ErrorCode: "CORRECTION_TARGET_NOT_FOUND", Message: "校对目标不存在", Kind: KindBusiness}
	// CodeCorrectionMeetingStateInvalid 表示当前会议状态禁止人工校对。
	CodeCorrectionMeetingStateInvalid = Code{Value: 409, ErrorCode: "CORRECTION_MEETING_STATE_INVALID", Message: "当前会议状态不能校对", Kind: KindBusiness}
	// CodeCorrectionRevisionConflict 表示目标在用户编辑期间已经变化。
	CodeCorrectionRevisionConflict = Code{Value: 409, ErrorCode: "CORRECTION_REVISION_CONFLICT", Message: "内容已变化，请刷新后重试", Kind: KindBusiness, Retryable: true}
	// CodeCorrectionIdempotencyConflict 表示同一请求标识被用于不同提交内容。
	CodeCorrectionIdempotencyConflict = Code{Value: 409, ErrorCode: "CORRECTION_IDEMPOTENCY_CONFLICT", Message: "重复请求的内容不一致", Kind: KindBusiness}
	// CodeCorrectionTextInvalid 表示校对文本为空或超过长度上限。
	CodeCorrectionTextInvalid = Code{Value: 400, ErrorCode: "CORRECTION_TEXT_INVALID", Message: "校对文字为空或过长", Kind: KindValidation}
	// CodeAudioClipUnavailable 表示对应会议录音无法安全回放。
	CodeAudioClipUnavailable = Code{Value: 404, ErrorCode: "AUDIO_CLIP_UNAVAILABLE", Message: "对应录音不可回放", Kind: KindBusiness}
	// CodeAudioClipExpired 表示短期音频片段访问令牌已经失效。
	CodeAudioClipExpired = Code{Value: 410, ErrorCode: "AUDIO_CLIP_EXPIRED", Message: "音频片段已过期，请重新加载", Kind: KindBusiness, Retryable: true}
	// CodeVoiceMeetingClipRejected 表示会议片段未通过永久声纹样本质量门。
	CodeVoiceMeetingClipRejected = Code{Value: 409, ErrorCode: "VOICE_MEETING_CLIP_REJECTED", Message: "会议片段不满足声纹样本质量要求", Kind: KindBusiness, Retryable: true}
	// CodeRawRecordRefreshFailed 表示 SQLite 事实已保存但 Markdown 投影未能安全刷新。
	CodeRawRecordRefreshFailed = Code{Value: 500, ErrorCode: "RAW_RECORD_REFRESH_FAILED", Message: "原始记录文件尚未刷新", Kind: KindSystem, Retryable: true}
	// CodeLANInterfaceUnavailable 表示没有可安全绑定的私有网络接口。
	CodeLANInterfaceUnavailable = Code{Value: 503, ErrorCode: "LAN_INTERFACE_UNAVAILABLE", Message: "没有可用的私有网络，请选择其他网络", Kind: KindBusiness, Retryable: true}
	// CodeLANStartFailed 表示局域网访客页未能启动，但不影响会议录音。
	CodeLANStartFailed = Code{Value: 503, ErrorCode: "LAN_START_FAILED", Message: "访客页启动失败，录音和实时转写不受影响", Kind: KindSystem, Retryable: true}
	// CodeLANGenerationChanged 表示请求属于已经停止的访客页实例。
	CodeLANGenerationChanged = Code{Value: 409, ErrorCode: "LAN_GENERATION_CHANGED", Message: "访客入口已更新，请重新扫码进入", Kind: KindBusiness, Retryable: true}
	// CodeLANSessionInvalid 表示访客会话凭据无效。
	CodeLANSessionInvalid = Code{Value: 401, ErrorCode: "LAN_SESSION_INVALID", Message: "访客会话无效，请重新扫码进入", Kind: KindBusiness}
	// CodeLANSessionExpired 表示访客会话已超过有效期。
	CodeLANSessionExpired = Code{Value: 401, ErrorCode: "LAN_SESSION_EXPIRED", Message: "访客会话已过期，请重新扫码进入", Kind: KindBusiness, Retryable: true}
	// CodeLANMeetingEnded 表示会议已经停止局域网访问。
	CodeLANMeetingEnded = Code{Value: 409, ErrorCode: "LAN_MEETING_ENDED", Message: "本场会议已结束，访客入口已停止", Kind: KindBusiness}
	// CodeLANRateLimited 表示访客请求超过安全频率或并发限制。
	CodeLANRateLimited = Code{Value: 429, ErrorCode: "LAN_RATE_LIMITED", Message: "请求过于频繁，请稍后重试", Kind: KindBusiness, Retryable: true}
	// CodeMessageInvalid 表示会议消息为空、过长或包含非法字符。
	CodeMessageInvalid = Code{Value: 400, ErrorCode: "MESSAGE_INVALID", Message: "会议消息为空、过长或格式无效", Kind: KindValidation}
	// CodeLinkInvalid 表示访客链接不是安全的绝对 HTTP 地址。
	CodeLinkInvalid = Code{Value: 400, ErrorCode: "LINK_INVALID", Message: "请输入有效的 HTTP 或 HTTPS 链接", Kind: KindValidation}
	// CodeAttachmentTooLarge 表示附件超过单文件上限。
	CodeAttachmentTooLarge = Code{Value: 413, ErrorCode: "ATTACHMENT_TOO_LARGE", Message: "单个附件不能超过 500 MB", Kind: KindValidation}
	// CodeAttachmentTypeBlocked 表示附件属于禁止保存的可执行类型。
	CodeAttachmentTypeBlocked = Code{Value: 400, ErrorCode: "ATTACHMENT_TYPE_BLOCKED", Message: "该文件类型不能上传", Kind: KindValidation}
	// CodeAttachmentDiskLow 表示附件会侵占录音所需的安全磁盘余量。
	CodeAttachmentDiskLow = Code{Value: 503, ErrorCode: "ATTACHMENT_DISK_LOW", Message: "主机可用空间不足，附件未上传", Kind: KindBusiness, Retryable: true}
	// CodeAttachmentUploadCancelled 表示附件上传被访客或主持人取消。
	CodeAttachmentUploadCancelled = Code{Value: 409, ErrorCode: "ATTACHMENT_UPLOAD_CANCELLED", Message: "附件上传已取消，没有保留临时文件", Kind: KindBusiness, Retryable: true}
	// CodeAttachmentUploadFailed 表示附件文件或索引提交未完成。
	CodeAttachmentUploadFailed = Code{Value: 500, ErrorCode: "ATTACHMENT_UPLOAD_FAILED", Message: "附件未上传，请重新选择文件", Kind: KindSystem, Retryable: true}
	// CodeAttachmentNotFound 表示附件不存在或不属于当前会议。
	CodeAttachmentNotFound = Code{Value: 404, ErrorCode: "ATTACHMENT_NOT_FOUND", Message: "附件不存在", Kind: KindBusiness}
)
