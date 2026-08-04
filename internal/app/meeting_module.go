package app

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	codexagent "meet-sieve/internal/adapter/agent/codex"
	volcanoasr "meet-sieve/internal/adapter/asr/volcano"
	fileflash "meet-sieve/internal/adapter/asr/volcano/fileflash"
	networkadapter "meet-sieve/internal/adapter/network"
	"meet-sieve/internal/adapter/systemopen"
	appbootstrap "meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/app/health"
	transcriptdomain "meet-sieve/internal/domain/transcript"
	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	infraLogger "meet-sieve/internal/infra/logger"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
	contentrepository "meet-sieve/internal/repository/content"
	deletionrepository "meet-sieve/internal/repository/deletion"
	gaprepository "meet-sieve/internal/repository/gap"
	guestrepository "meet-sieve/internal/repository/guest"
	meetingrepository "meet-sieve/internal/repository/meeting"
	minutesrepository "meet-sieve/internal/repository/minutes"
	queryrepository "meet-sieve/internal/repository/query"
	speakerrepository "meet-sieve/internal/repository/speaker"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	agentservice "meet-sieve/internal/service/agent"
	audioservice "meet-sieve/internal/service/audio"
	deletionservice "meet-sieve/internal/service/deletion"
	diagnosticsservice "meet-sieve/internal/service/diagnostics"
	finalizationservice "meet-sieve/internal/service/finalization"
	gapservice "meet-sieve/internal/service/gap"
	guestservice "meet-sieve/internal/service/guest"
	lanservice "meet-sieve/internal/service/lan"
	lifecycleservice "meet-sieve/internal/service/lifecycle"
	meetingservice "meet-sieve/internal/service/meeting"
	minutesservice "meet-sieve/internal/service/minutes"
	queryservice "meet-sieve/internal/service/query"
	resourceservice "meet-sieve/internal/service/resource"
	resourceopenservice "meet-sieve/internal/service/resourceopen"
	speakerservice "meet-sieve/internal/service/speaker"
	transcriptservice "meet-sieve/internal/service/transcript"
	transporthttp "meet-sieve/internal/transport/http"
	guesthttp "meet-sieve/internal/transport/http/guest"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// MeetingServices 保存当前工作目录对应的会议事务服务和唯一录音运行时。
type MeetingServices struct {
	// Query 提供首页、记录、详情和长列表的只读投影。
	Query *queryservice.Service
	// Deletion 执行录音和整场会议的可恢复安全删除。
	Deletion *deletionservice.Service
	// AudioSettings 独立保存默认麦克风并执行真实设备测试。
	AudioSettings *audioservice.SettingsService
	// StorageScan 提供真实磁盘分类与 Top 会议占用。
	StorageScan *diagnosticsservice.StorageScanService
	// Diagnostics 导出固定白名单的二次脱敏 ZIP。
	Diagnostics *diagnosticsservice.ExportService
	// ResourceOpen 是附件和外部链接调用系统默认程序的唯一入口。
	ResourceOpen *resourceopenservice.Service
	// Maintenance 串行同场维护并停止现有后台任务。
	Maintenance *lifecycleservice.Coordinator
	// Meetings 负责会议事实事务。
	Meetings *meetingservice.Service
	// Runtime 负责录音运行时生命周期。
	Runtime *meetingservice.RuntimeService
	// Recovery 负责异常退出后的录音恢复。
	Recovery *meetingservice.RecoveryService
	// TranscriptSettings 负责火山 ASR 凭据设置与连接探测。
	TranscriptSettings *transcriptservice.SettingsService
	// TranscriptRuntime 负责当前活动会议的实时转写生命周期。
	TranscriptRuntime *transcriptservice.MeetingRuntime
	// TranscriptTimeline 提供可按 seq 恢复的 final/gap 快照。
	TranscriptTimeline *transcriptservice.TimelineService
	// LAN 管理网卡、运行时与独立状态轴。
	LAN *lanservice.Manager
	// LANRuntime 持有当前进程的会议令牌和 Listener。
	LANRuntime *lanservice.Runtime
	// GuestPresence 提供宿主 UI 的 45 秒在线人数。
	GuestPresence *guesthttp.Presence
	// GuestUploads 提供宿主取消活动上传的边界。
	GuestUploads *resourceservice.UploadCoordinator
	// RecoverAttachments 清理活动会议中仅由应用拥有的附件暂存和孤儿候选。
	RecoverAttachments func(context.Context) error
	// AgentSettings 提供 Codex 和唤醒词设置。
	AgentSettings *agentservice.SettingsService
	// AgentWakeTest 运行不落盘的真实三次唤醒测试。
	AgentWakeTest *agentservice.WakeWordTestService
	// AgentOrchestrator 持有本场唯一 Codex session。
	AgentOrchestrator *agentservice.Orchestrator
	// AgentTurns 编排主持人问题、审批和停止。
	AgentTurns *agentservice.TurnService
	// AgentRecoveryCommands 投影可信恢复命令。
	AgentRecoveryCommands *agentservice.RecoveryCommandService
	// FinalSync 执行不公开回答的 Codex 结束同步。
	FinalSync *agentservice.FinalSyncService
	// PostMeeting 串行拥有一场会议的后台补转写与结束同步。
	PostMeeting *finalizationservice.PostMeetingProcessor
	// GapRunner 执行一次真实补转写请求。
	GapRunner *gapservice.Runner
	// GapRepository 提供页面状态的 SQLite 重建查询。
	GapRepository *gaprepository.Repository
	// GapConflicts 查询实时冲突双份证据。
	GapConflicts *gapservice.ConflictQueryService
	// GapResolution 原子提交主持人的冲突解决动作。
	GapResolution *gapservice.ResolutionService
	// GapClips 为冲突工作台签发不泄漏路径的短期回放 URL。
	GapClips *gapservice.AudioClipService
	// MinutesGeneration 执行白名单事实驱动的纪要生成。
	MinutesGeneration *minutesservice.GenerationService
	// MinutesVersions 维护人工、确认和恢复版本。
	MinutesVersions *minutesservice.VersionService
	// MinutesRepository 提供当前版本与历史只读查询。
	MinutesRepository *minutesrepository.Repository
	// MinutesProjector 维护 SQLite current 到 Markdown 的独立投影。
	MinutesProjector *minutesservice.MinuteProjector
}

// MeetingModule 延迟装配工作目录相关的 Step 3 服务，并跨 Wails 调用保留唯一录音会话。
type MeetingModule struct {
	workspace       *appbootstrap.Coordinator
	capture         port.AudioCapture
	recordingConfig config.RecordingConfig
	asrConfig       config.ASRConfig
	voice           *VoiceModule
	logger          *infraLogger.AppLogger
	health          *health.Registry
	guestAssets     fs.FS

	mu                 sync.Mutex
	reader             *gorm.DB
	services           *MeetingServices
	partialPublisher   transcriptservice.PartialPublisher
	statePublisher     transcriptservice.RealtimeStatePublisher
	speakerPublisher   func(meetingID string, trackID string)
	agentPublisher     func(port.AgentEvent)
	wakeTestPublisher  func(agentservice.WakeWordTestState)
	speakerCancel      context.CancelFunc
	postMeetingCancel  context.CancelFunc
	finalizationEvents meetingservice.FinalizationEventSink
	gapEvents          gapservice.EventSink
	minutesEvents      minutesservice.GenerationEventSink
	finalSyncEvents    agentservice.FinalSyncEventSink
}

// SetStep8Publishers 在首次装配前登记四类会后轻量事件出口。
func (module *MeetingModule) SetStep8Publishers(finalization meetingservice.FinalizationEventSink, gap gapservice.EventSink, minutes minutesservice.GenerationEventSink, finalSync agentservice.FinalSyncEventSink) error {
	if module == nil {
		return fmt.Errorf("会议模块不可用")
	}
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil {
		return fmt.Errorf("会议服务已装配，不能更换会后事件发布器")
	}
	module.finalizationEvents, module.gapEvents = finalization, gap
	module.minutesEvents, module.finalSyncEvents = minutes, finalSync
	return nil
}

// SetAgentPublishers 在首次装配前登记 AI 和唤醒测试事件发布边界。
func (module *MeetingModule) SetAgentPublishers(agentPublisher func(port.AgentEvent), wakeTestPublisher func(agentservice.WakeWordTestState)) error {
	if module == nil {
		return fmt.Errorf("会议模块不可用")
	}
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil {
		return fmt.Errorf("会议服务已装配，不能更换智能体事件发布器")
	}
	module.agentPublisher, module.wakeTestPublisher = agentPublisher, wakeTestPublisher
	return nil
}

// SetGuestAssets 在首次服务装配前接入独立 Guest Web 构建产物。
func (module *MeetingModule) SetGuestAssets(assets fs.FS) error {
	if module == nil || assets == nil {
		return fmt.Errorf("访客页面资源不可用")
	}
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil {
		return fmt.Errorf("会议服务已装配，不能更换访客页面资源")
	}
	module.guestAssets = assets
	return nil
}

// NewMeetingModule 创建会议模块；工作目录 ready 前不访问数据库或麦克风。
func NewMeetingModule(
	workspace *appbootstrap.Coordinator,
	capture port.AudioCapture,
	recordingConfig config.RecordingConfig,
	asrConfig config.ASRConfig,
	appLogger *infraLogger.AppLogger,
	registry *health.Registry,
) *MeetingModule {
	return &MeetingModule{
		workspace: workspace, capture: capture, recordingConfig: recordingConfig, asrConfig: asrConfig,
		logger: appLogger, health: registry,
	}
}

// SetVoiceModule 在首次工作目录服务装配前接入异步模型提供器。
func (module *MeetingModule) SetVoiceModule(voice *VoiceModule) error {
	if module == nil {
		return fmt.Errorf("会议模块不可用")
	}
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil {
		return fmt.Errorf("会议服务已装配，不能更换声纹模块")
	}
	module.voice = voice
	return nil
}

// SetTranscriptPublishers 在首次服务装配前登记 Wails 实时事件发布边界。
func (module *MeetingModule) SetTranscriptPublishers(partial transcriptservice.PartialPublisher, state transcriptservice.RealtimeStatePublisher) error {
	if module == nil {
		return fmt.Errorf("会议模块不可用")
	}
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil {
		return fmt.Errorf("会议服务已装配，不能更换实时事件发布器")
	}
	module.partialPublisher, module.statePublisher = partial, state
	return nil
}

// SetSpeakerPublisher 在首次装配前登记自动 speaker 事实提交后的轻量刷新通知。
func (module *MeetingModule) SetSpeakerPublisher(publisher func(meetingID string, trackID string)) error {
	if module == nil {
		return fmt.Errorf("会议模块不可用")
	}
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil {
		return fmt.Errorf("会议服务已装配，不能更换 speaker 发布器")
	}
	module.speakerPublisher = publisher
	return nil
}

// Current 返回当前工作目录唯一的一组会议服务。
func (module *MeetingModule) Current() (*MeetingServices, error) {
	if module == nil || module.workspace == nil || module.capture == nil {
		return nil, fmt.Errorf("会议模块依赖不可用")
	}
	reader, transactions, err := module.workspace.BusinessDatabase()
	if err != nil {
		return nil, err
	}
	// 工作目录设置会调用会议阻断器，必须在获取模块锁之前读取，避免反向获取同一把锁。
	workspacePath := module.workspace.GetWorkspaceSettings().ActivePath
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil && module.reader == reader {
		return module.services, nil
	}
	if module.speakerCancel != nil {
		module.speakerCancel()
		module.speakerCancel = nil
	}
	if module.postMeetingCancel != nil {
		module.postMeetingCancel()
		module.postMeetingCancel = nil
	}
	services, err := module.buildServices(reader, transactions, workspacePath)
	if err != nil {
		return nil, err
	}
	module.reader = reader
	module.services = services
	return services, nil
}

// StopSpeakerAutomation 停止当前工作目录的固定 worker 与恢复轮询。
func (module *MeetingModule) StopSpeakerAutomation() {
	if module == nil {
		return
	}
	module.mu.Lock()
	if module.speakerCancel != nil {
		module.speakerCancel()
		module.speakerCancel = nil
	}
	module.mu.Unlock()
}

// StopAgentRuntime 停止设置页临时采集和当前会议 Codex 子进程。
func (module *MeetingModule) StopAgentRuntime(ctx context.Context) {
	if module == nil {
		return
	}
	module.mu.Lock()
	services := module.services
	module.mu.Unlock()
	if services == nil {
		return
	}
	if services.AgentWakeTest != nil {
		_ = services.AgentWakeTest.Stop(ctx)
	}
	if services.PostMeeting != nil {
		_ = services.PostMeeting.Stop(ctx)
	}
	if services.AgentOrchestrator != nil {
		_ = services.AgentOrchestrator.Shutdown(ctx)
	}
}

// PrepareDeletionExit 在限定时间内等待当前删除原子项完成持久化。
func (module *MeetingModule) PrepareDeletionExit(ctx context.Context) bool {
	if module == nil {
		return true
	}
	module.mu.Lock()
	services := module.services
	module.mu.Unlock()
	return services == nil || services.Deletion == nil || services.Deletion.PrepareExit(ctx)
}

// HasUnsafeWorkspaceChange 返回录音、收尾或活动删除是否阻止切换下次工作目录。
func (module *MeetingModule) HasUnsafeWorkspaceChange(ctx context.Context) bool {
	if module == nil {
		return false
	}
	module.mu.Lock()
	reader := module.reader
	module.mu.Unlock()
	if reader == nil {
		return false
	}
	var count int64
	err := reader.WithContext(ctx).Raw(`
		SELECT
			(SELECT COUNT(*) FROM meetings WHERE lifecycle_state IN ('preparing','recording','finalizing')) +
			(SELECT COUNT(*) FROM deletion_jobs WHERE state IN ('pending','running'))`).Scan(&count).Error
	return err != nil || count > 0
}

// Recover 在业务 UI 开放后对遗留活动会议执行一次幂等文件对账。
func (module *MeetingModule) Recover(ctx context.Context) ([]meetingservice.RecoveryResult, error) {
	if module == nil || module.workspace == nil || module.workspace.GetState().Phase != domainworkspace.BootstrapPhaseReady {
		return []meetingservice.RecoveryResult{}, nil
	}
	services, err := module.Current()
	if err != nil {
		return nil, err
	}
	if services.Deletion != nil {
		// 上次进程中的 running 删除只收敛为 failed，绝不自动继续领取文件项。
		if err := services.Deletion.RecoverInterrupted(ctx); err != nil {
			return nil, err
		}
	}
	if services.RecoverAttachments != nil {
		if err := services.RecoverAttachments(ctx); err != nil {
			return nil, err
		}
	}
	results, err := services.Recovery.RecoverInterruptedMeetings(ctx)
	if err != nil {
		return results, err
	}
	if services.PostMeeting != nil {
		if err := services.PostMeeting.Recover(ctx); err != nil {
			return results, err
		}
	}
	return results, nil
}

// FinishActiveMeeting 在应用关闭前复用正常结束流程，不静默丢弃活动录音。
func (module *MeetingModule) FinishActiveMeeting(ctx context.Context) error {
	if module == nil || module.workspace == nil || module.workspace.GetState().Phase != domainworkspace.BootstrapPhaseReady {
		return nil
	}
	services, err := module.Current()
	if err != nil {
		return err
	}
	active, err := services.Meetings.GetActiveMeeting(ctx)
	if err != nil || active == nil {
		return err
	}
	if active.LifecycleState == "recording" || active.LifecycleState == "finalizing" {
		_, err = services.Runtime.EndMeeting(ctx, active.ID)
		return err
	}
	_, err = services.Recovery.RecoverInterruptedMeetings(ctx)
	return err
}

// HasActiveMeeting 只读判断窗口关闭是否必须被会议确认流程拦截。
func (module *MeetingModule) HasActiveMeeting(ctx context.Context) (bool, error) {
	if module == nil || module.workspace == nil || module.workspace.GetState().Phase != domainworkspace.BootstrapPhaseReady {
		return false, nil
	}
	services, err := module.Current()
	if err != nil {
		return false, err
	}
	active, err := services.Meetings.GetActiveMeeting(ctx)
	return active != nil, err
}

// buildServices 从已验证的当前数据库身份与工作目录构造 Step 3 服务。
func (module *MeetingModule) buildServices(reader *gorm.DB, transactions *database.TransactionManager, workspacePath string) (*MeetingServices, error) {
	var metadata models.AppMetadata
	if err := reader.Select("id", "singleton_key", "product", "database_id", "device_code", "created_with_app_version", "created_at", "updated_at").
		Where("singleton_key = 1").Take(&metadata).Error; err != nil {
		return nil, fmt.Errorf("读取会议设备身份失败: %w", err)
	}
	if workspacePath == "" {
		return nil, fmt.Errorf("当前会议工作目录不可用")
	}
	repository := meetingrepository.NewRepository(reader)
	queryRepository := queryrepository.NewRepository(reader)
	queryService := queryservice.NewService(queryRepository)
	currentClock := clock.NewSystem()
	ids := identity.NewUUIDGenerator()
	meetings := meetingservice.NewService(meetingservice.Dependencies{
		Repository: repository, Transactions: transactions, IDs: ids,
		Clock: currentClock, DeviceCode: metadata.DeviceCode,
	})
	transcripts := transcriptrepository.NewRepository(reader)
	transcriberFactory := func(credentials transcriptdomain.Credentials) port.RealtimeTranscriber {
		return volcanoasr.NewAdapter(volcanoasr.AdapterConfig{
			Endpoint: module.asrConfig.Endpoint, ResourceID: module.asrConfig.ResourceID,
			Credentials: credentials, ConnectTimeout: time.Duration(module.asrConfig.ConnectTimeoutSeconds) * time.Second,
		}, identity.NewUUIDGenerator())
	}
	transcriptSettings := transcriptservice.NewSettingsService(transcriptservice.SettingsServiceDependencies{
		Repository: transcripts, Transactions: transactions, Clock: currentClock, Transcriber: transcriberFactory,
	})
	transcriptTimeline := transcriptservice.NewTimelineService(transcripts)
	rawRecord := transcriptservice.NewRawRecordProjector(transcriptservice.RawRecordProjectorDependencies{
		Repository: transcripts, WorkspaceRoot: workspacePath, Debounce: 2 * time.Second,
	})
	agentRepository := agentrepository.NewRepository(reader, transactions)
	agentProvider := codexagent.NewProvider("codex")
	agentContext := agentservice.NewContextBuilder(agentRepository)
	agentTurns := agentservice.NewTurnService(agentservice.TurnServiceDependencies{
		Repository: agentRepository, Context: agentContext, Provider: agentProvider, RawRecord: rawRecord,
		IDs: identity.NewUUIDGenerator(), Clock: currentClock, Events: agentservice.TurnEventSinkFunc(module.agentPublisher),
	})
	agentOrchestrator := agentservice.NewOrchestrator(agentservice.OrchestratorDependencies{
		Repository: agentRepository, Context: agentContext, Provider: agentProvider, RawRecord: rawRecord,
		IDs: identity.NewUUIDGenerator(), Clock: currentClock, WorkspaceRoot: workspacePath,
		Executable: func(ctx context.Context) (string, error) {
			current, err := agentRepository.GetSettings(ctx)
			if err != nil || current.CodexExecutablePath == nil {
				return "", err
			}
			return *current.CodexExecutablePath, nil
		},
	})
	agentSettings := agentservice.NewSettingsService(agentRepository, agentProvider, currentClock)
	agentWakeTest := agentservice.NewWakeWordTestService(agentservice.WakeWordTestDependencies{
		Guard: agentRepository, Capture: module.capture, Credentials: transcriptSettings.CurrentCredentials,
		Transcriber: transcriberFactory,
		WakeWord: func(ctx context.Context) (string, error) {
			current, err := agentRepository.GetSettings(ctx)
			return current.WakeWord, err
		},
		DeviceID: agentRepository.GetDefaultMicrophoneID, Publish: module.wakeTestPublisher,
	})
	wakeObserver := agentservice.NewWakeObserver(agentRepository, agentTurns)
	agentRecoveryCommands := agentservice.NewRecoveryCommandService(agentRepository, workspacePath)
	gapRepository := gaprepository.NewRepository(reader, transactions)
	gapExtractor := gapservice.NewExtractor(gapRepository, workspacePath)
	gapCommitter := gapservice.NewCompensationCommitter(gapservice.CommitterDependencies{
		Repository: gapRepository, RawRecord: rawRecord, IDs: identity.NewUUIDGenerator(), Clock: currentClock,
	})
	gapRunner := gapservice.NewRunner(gapservice.RunnerDependencies{
		Repository: gapRepository, Extractor: gapExtractor,
		Transcriber: fileflash.NewDynamicAdapter(transcriptSettings.CurrentCredentials), Committer: gapCommitter,
		IDs: identity.NewUUIDGenerator(), Clock: currentClock, Events: module.gapEvents,
	})
	gapConflicts := gapservice.NewConflictQueryService(gapRepository)
	gapResolution := gapservice.NewResolutionService(gapRepository, gapExtractor, rawRecord, identity.NewUUIDGenerator(), currentClock)
	gapResolution.SetEventSink(module.gapEvents)
	gapClips, err := gapservice.NewAudioClipService(gapservice.AudioClipDependencies{
		Repository: gapRepository, WorkspaceRoot: workspacePath, Clock: currentClock,
	})
	if err != nil {
		return nil, err
	}
	minutesRepository := minutesrepository.NewRepository(reader, transactions)
	minutesProjector := minutesservice.NewMinuteProjector(minutesRepository, workspacePath)
	minutesGeneration := minutesservice.NewGenerationService(minutesservice.GenerationDependencies{
		Repository: minutesRepository, AgentRepository: agentRepository, Facts: minutesRepository,
		Provider: agentProvider, RawRecord: rawRecord, Projector: minutesProjector,
		IDs: identity.NewUUIDGenerator(), Clock: currentClock, Sessions: agentOrchestrator, Events: module.minutesEvents,
	})
	minutesVersions := minutesservice.NewVersionService(minutesRepository, repository, minutesProjector, identity.NewUUIDGenerator(), currentClock)
	minutesVersions.SetEventSink(module.minutesEvents)
	finalSync := agentservice.NewFinalSyncService(agentservice.FinalSyncDependencies{
		Repository: agentRepository, Context: agentContext, Provider: agentProvider, RawRecord: rawRecord,
		Sessions: agentOrchestrator, IDs: identity.NewUUIDGenerator(), Clock: currentClock, Events: module.finalSyncEvents,
	})
	recoveryCoordinator := finalizationservice.NewRecoveryCoordinator(gapRepository, minutesRepository, agentRepository, currentClock)
	postMeeting := finalizationservice.NewPostMeetingProcessor(finalizationservice.ProcessorDependencies{
		Gaps: gapRunner, Syncer: finalSync, Recovery: recoveryCoordinator,
	})
	contentRepository := contentrepository.NewRepository(reader)
	guestRepository := guestrepository.NewRepository(reader, transactions)
	if err := guestRepository.RevokeAllActive(context.Background()); err != nil {
		return nil, fmt.Errorf("撤销上一进程访客会话：%w", err)
	}
	uploadCoordinator := resourceservice.NewUploadCoordinator()
	handlerProxy := &atomicHTTPHandler{}
	lanRuntime := lanservice.NewRuntime(lanservice.Dependencies{
		IDs: identity.NewUUIDGenerator(), Handler: handlerProxy,
		Sessions: guestRepository, Uploads: uploadCoordinator,
	})
	networkResolver := networkadapter.NewResolver()
	lanManager := lanservice.NewManager(networkResolver, lanRuntime, repository, currentClock)
	sessionService := guestservice.NewSessionService(guestservice.SessionDependencies{
		Repository: guestRepository, Access: lanRuntime, Clock: currentClock,
		IDs: identity.NewUUIDGenerator(),
	})
	contentService := guestservice.NewContentService(guestservice.ContentDependencies{
		Repository: contentRepository, Transactions: transactions, Clock: currentClock, IDs: identity.NewUUIDGenerator(),
		OnPersisted: func(meetingID string) { _ = rawRecord.MarkDirty(meetingID) },
	})
	directoryResolver := &meetingDirectoryResolver{repository: repository, workspaceRoot: workspacePath}
	attachmentService := resourceservice.NewAttachmentService(resourceservice.AttachmentDependencies{
		Repository: contentRepository, Transactions: transactions, Coordinator: uploadCoordinator,
		Policy: resourceservice.NewFilePolicy(), Directories: directoryResolver, Clock: currentClock,
		IDs: identity.NewUUIDGenerator(), AvailableBytes: filesystem.AvailableBytes,
		MinimumFreeBytes: uint64(module.recordingConfig.MinimumFreeSpaceGiB) << 30,
		OnPersisted:      func(meetingID string) { _ = rawRecord.MarkDirty(meetingID) },
	})
	downloadService := resourceservice.NewDownloadService(contentRepository, directoryResolver, resourceservice.NewFileStore())
	attachmentRecovery := resourceservice.NewRecovery(contentRepository.ListReferencedSafeNames)
	recoverAttachments := func(ctx context.Context) error {
		activeMeetings, err := repository.ListActiveMeetings(ctx)
		if err != nil {
			return err
		}
		for _, activeMeeting := range activeMeetings {
			directory, resolveErr := directoryResolver.ResolveMeetingDirectory(ctx, activeMeeting.ID)
			if resolveErr != nil {
				return resolveErr
			}
			if recoverErr := attachmentRecovery.RecoverMeeting(ctx, activeMeeting.ID, directory); recoverErr != nil {
				return recoverErr
			}
		}
		return nil
	}
	presence := guesthttp.NewPresence()
	limiter := guesthttp.NewLimiter()
	appLogger := module.logger
	if appLogger == nil {
		appLogger = infraLogger.NewNop()
	}
	registry := module.health
	if registry == nil {
		registry = health.NewRegistry()
	}
	guestEngine := transporthttp.NewGuestEngine(registry, appLogger, guesthttp.RouteDependencies{
		Sessions: sessionService, Content: contentService, Timeline: guestservice.NewTimelineService(contentRepository),
		Attachments: attachmentService, Downloads: downloadService, Limiter: limiter, Presence: presence,
		ExpectedOrigin: func() string { return guesthttp.ExpectedOriginFromJoinURL(lanRuntime.Snapshot().JoinURL) },
		Generation:     func() string { return lanRuntime.Snapshot().Generation },
		WebAssets:      module.guestAssets,
	})
	handlerProxy.Store(guestEngine)
	rolling, err := speakerservice.NewRollingBuffer(120 * 16000)
	if err != nil {
		return nil, err
	}
	speakerRepository := speakerrepository.NewRepository(reader)
	meetingAudio, err := speakerservice.NewMeetingAudioReader(workspacePath, repository, rolling, 120*16000)
	if err != nil {
		return nil, err
	}
	speakerWake := make(chan string, 64)
	speakerObserver := speakerservice.NewObserver(speakerservice.ObserverDependencies{
		Repository: speakerRepository, Transactions: transactions,
		IDs: identity.NewUUIDGenerator(), Clock: currentClock, Queue: speakerWake,
	})
	unknown := speakerservice.NewUnknownAssigner(speakerservice.UnknownAssignerDependencies{
		Repository: speakerRepository, Transactions: transactions, IDs: identity.NewUUIDGenerator(), Clock: currentClock,
	})
	processor := &lazySpeakerProcessor{
		voice: module.voice, repository: speakerRepository, transactions: transactions,
		evidence: speakerservice.NewEvidenceBuilder(meetingAudio), unknown: unknown, clock: currentClock,
		onChanged: func(meetingID string, trackID string) {
			_ = rawRecord.MarkDirty(meetingID)
			if module.speakerPublisher != nil {
				module.speakerPublisher(meetingID, trackID)
			}
		},
	}
	pool := speakerservice.NewRunnerPool(speakerservice.RunnerPoolDependencies{
		Processor: processor, Recovery: speakerRepository,
		Config: speakerservice.RunnerPoolConfig{WorkerCount: 2, QueueCapacity: 64, RecoveryBatch: 64},
	})
	poolContext, cancelSpeaker := context.WithCancel(context.Background())
	module.speakerCancel = cancelSpeaker
	go runSpeakerPool(poolContext, pool, speakerWake)
	transcriptEvents := transcriptservice.NewEventService(transcriptservice.EventServiceDependencies{
		Repository: transcripts, Transactions: transactions, IDs: identity.NewUUIDGenerator(), Clock: currentClock,
		OnPersisted: func(meetingID string, event transcriptservice.PersistedEvent) {
			_ = rawRecord.MarkDirty(meetingID)
			// final 提交后留下可恢复的 speaker 事实；模型门禁只影响后续自动匹配。
			if event.Kind == "utterance.final" && event.EntityID != "" {
				_, _ = speakerObserver.Observe(context.Background(), event.EntityID)
				_, _ = wakeObserver.Observe(context.Background(), event.EntityID)
			}
		},
	})
	transcriptRecovery := transcriptservice.NewRecoveryService(transcriptservice.RecoveryServiceDependencies{Repository: transcripts, Transactions: transactions, Events: transcriptEvents, Clock: currentClock})
	backoff := make([]time.Duration, 0, len(module.asrConfig.ReconnectBackoffSeconds))
	for _, seconds := range module.asrConfig.ReconnectBackoffSeconds {
		backoff = append(backoff, time.Duration(seconds)*time.Second)
	}
	transcriptRuntime := transcriptservice.NewMeetingRuntime(transcriptservice.MeetingRuntimeDependencies{
		Settings: transcriptSettings, Repository: transcripts, Transactions: transactions, Events: transcriptEvents,
		Transcriber: transcriberFactory, IDs: identity.NewUUIDGenerator(), Clock: currentClock, Backoff: backoff,
		FinalPersistTimeout: time.Duration(module.asrConfig.FinalPersistTimeoutSeconds) * time.Second,
		FinalQueueCapacity:  module.asrConfig.FinalQueueCapacity,
		RawRecord:           rawRecord, WorkspaceRoot: workspacePath,
		PublishPartial: module.partialPublisher, PublishState: module.statePublisher,
	})
	maxSamples := int64(module.recordingConfig.MaxSegmentSeconds * 16000)
	checkpointSamples := int64(module.recordingConfig.CheckpointSeconds * 16000)
	coordinator := meetingservice.NewRecordingCoordinator(
		module.capture, maxSamples, checkpointSamples,
		time.Duration(module.recordingConfig.FirstFrameTimeoutSeconds)*time.Second,
	)
	postMeetingContext, cancelPostMeeting := context.WithCancel(context.Background())
	if err := postMeeting.Start(postMeetingContext); err != nil {
		cancelPostMeeting()
		return nil, err
	}
	module.postMeetingCancel = cancelPostMeeting
	runtime := meetingservice.NewRuntimeService(meetingservice.RuntimeDependencies{
		Meetings: meetings, Repository: repository, Coordinator: coordinator,
		Capture: module.capture, Clock: currentClock, IDs: ids, WorkspaceRoot: workspacePath,
		AvailableBytes:     filesystem.AvailableBytes,
		MinimumFreeBytes:   uint64(module.recordingConfig.MinimumFreeSpaceGiB) << 30,
		DeviceTestTimeout:  time.Duration(module.recordingConfig.FirstFrameTimeoutSeconds) * time.Second,
		Transcript:         transcriptRuntime,
		RawRecord:          rawRecord,
		LAN:                lanManager,
		Agent:              agentOrchestrator,
		AgentTurns:         agentTurns,
		PostMeeting:        postMeeting,
		FinalizationEvents: module.finalizationEvents,
		PersistedPCMObserver: func(frame port.AudioFrame) {
			_ = writeRollingFrame(rolling, frame)
		},
	})
	recovery := meetingservice.NewRecoveryService(meetingservice.RecoveryDependencies{
		Repository: repository, WorkspaceRoot: workspacePath, Clock: currentClock,
		IDs: identity.NewUUIDGenerator(), CheckpointSamples: checkpointSamples, Transcript: transcriptRecovery,
	})
	maintenance := lifecycleservice.NewCoordinatorWithStopper(nil, &meetingMaintenanceStopper{
		postMeeting: postMeeting, minutes: minutesGeneration, agentTurns: agentTurns,
		uploads: uploadCoordinator, gapClips: gapClips,
	})
	deletionRepository := deletionrepository.NewRepository(reader, transactions)
	deletion := deletionservice.NewService(deletionservice.Dependencies{
		Repository: deletionRepository, Maintenance: maintenance, IDs: identity.NewUUIDGenerator(),
		Clock: currentClock, WorkspaceRoot: workspacePath,
	})
	logRoot, _ := filesystem.CurrentLogDir()
	appDataRoot, _ := filesystem.CurrentAppDataDir()
	storageScan := diagnosticsservice.NewStorageScanService(reader, workspacePath, logRoot, filepath.Join(appDataRoot, "models"))
	diagnostics := diagnosticsservice.NewExportService(diagnosticsservice.ExportDependencies{
		Reader: reader, Health: module.health, WorkspaceRoot: workspacePath, LogRoot: logRoot,
	})
	resourceOpen := resourceopenservice.NewService(reader, transactions, workspacePath, systemopen.NewLauncher())
	audioSettings := audioservice.NewSettingsService(reader, transactions, module.capture)
	return &MeetingServices{
		Query: queryService, Deletion: deletion, AudioSettings: audioSettings, StorageScan: storageScan, Diagnostics: diagnostics,
		ResourceOpen: resourceOpen, Maintenance: maintenance,
		Meetings: meetings, Runtime: runtime, Recovery: recovery,
		TranscriptSettings: transcriptSettings, TranscriptRuntime: transcriptRuntime, TranscriptTimeline: transcriptTimeline,
		LAN: lanManager, LANRuntime: lanRuntime, GuestPresence: presence, GuestUploads: uploadCoordinator,
		RecoverAttachments: recoverAttachments,
		AgentSettings:      agentSettings, AgentWakeTest: agentWakeTest, AgentOrchestrator: agentOrchestrator,
		AgentTurns: agentTurns, AgentRecoveryCommands: agentRecoveryCommands,
		FinalSync: finalSync, PostMeeting: postMeeting, GapRunner: gapRunner, GapRepository: gapRepository,
		GapConflicts: gapConflicts, GapResolution: gapResolution, GapClips: gapClips,
		MinutesGeneration: minutesGeneration, MinutesVersions: minutesVersions,
		MinutesRepository: minutesRepository, MinutesProjector: minutesProjector,
	}, nil
}

// meetingMaintenanceStopper 把既有会后、纪要、Codex、上传和短片段运行时接入统一维护交接。
type meetingMaintenanceStopper struct {
	postMeeting *finalizationservice.PostMeetingProcessor
	minutes     *minutesservice.GenerationService
	agentTurns  *agentservice.TurnService
	uploads     *resourceservice.UploadCoordinator
	gapClips    *gapservice.AudioClipService
}

// StopMeeting 按固定顺序停止同场运行时，并等待会后 owner 到达安全终点。
func (stopper *meetingMaintenanceStopper) StopMeeting(ctx context.Context, meetingID string) error {
	if stopper == nil {
		return nil
	}
	if stopper.postMeeting != nil {
		if err := stopper.postMeeting.StopMeetingAndWait(ctx, meetingID); err != nil {
			return err
		}
	}
	if stopper.minutes != nil {
		if err := stopper.minutes.StopMeeting(ctx, meetingID); err != nil {
			return err
		}
	}
	if stopper.agentTurns != nil {
		if err := stopper.agentTurns.InterruptMeeting(ctx, meetingID); err != nil {
			return err
		}
	}
	if stopper.uploads != nil {
		stopper.uploads.CancelMeeting(meetingID)
	}
	if stopper.gapClips != nil {
		stopper.gapClips.RevokeAll()
	}
	return nil
}

// atomicHTTPHandler 在 LANRuntime 构造后、Start 前原子接入完整 Guest Engine。
type atomicHTTPHandler struct {
	target atomic.Value
}

// Store 设置后续 generation 共用的 Guest HTTP handler。
func (handler *atomicHTTPHandler) Store(target http.Handler) {
	if handler != nil && target != nil {
		handler.target.Store(target)
	}
}

// ServeHTTP 把请求转发给已完成装配的 Guest Engine。
func (handler *atomicHTTPHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if handler == nil {
		http.NotFound(writer, request)
		return
	}
	target := handler.target.Load()
	if target == nil {
		http.NotFound(writer, request)
		return
	}
	target.(http.Handler).ServeHTTP(writer, request)
}

// meetingDirectoryResolver 只从 SQLite 会议相对目录和已验证工作根构建绝对路径。
type meetingDirectoryResolver struct {
	repository    *meetingrepository.Repository
	workspaceRoot string
}

// ResolveMeetingDirectory 返回指定会议的可信绝对目录。
func (resolver *meetingDirectoryResolver) ResolveMeetingDirectory(ctx context.Context, meetingID string) (string, error) {
	if resolver == nil || resolver.repository == nil || !filepath.IsAbs(resolver.workspaceRoot) {
		return "", fmt.Errorf("会议目录解析器不可用")
	}
	meeting, err := resolver.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolver.workspaceRoot, filepath.FromSlash(meeting.RelativeDir)), nil
}

// runSpeakerPool 以固定轮询补拉 SQLite 任务；取消时释放 ticker 和全部 worker。
func runSpeakerPool(ctx context.Context, pool *speakerservice.RunnerPool, wake <-chan string) {
	poll := make(chan struct{}, 1)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case poll <- struct{}{}:
				default:
				}
			}
		}
	}()
	_ = pool.Run(ctx, wake, poll)
}
