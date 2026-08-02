package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	volcanoasr "meet-sieve/internal/adapter/asr/volcano"
	appbootstrap "meet-sieve/internal/app/bootstrap"
	transcriptdomain "meet-sieve/internal/domain/transcript"
	domainworkspace "meet-sieve/internal/domain/workspace"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/config"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	meetingrepository "meet-sieve/internal/repository/meeting"
	speakerrepository "meet-sieve/internal/repository/speaker"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	meetingservice "meet-sieve/internal/service/meeting"
	speakerservice "meet-sieve/internal/service/speaker"
	transcriptservice "meet-sieve/internal/service/transcript"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// MeetingServices 保存当前工作目录对应的会议事务服务和唯一录音运行时。
type MeetingServices struct {
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
}

// MeetingModule 延迟装配工作目录相关的 Step 3 服务，并跨 Wails 调用保留唯一录音会话。
type MeetingModule struct {
	workspace       *appbootstrap.Coordinator
	capture         port.AudioCapture
	recordingConfig config.RecordingConfig
	asrConfig       config.ASRConfig
	voice           *VoiceModule

	mu               sync.Mutex
	reader           *gorm.DB
	services         *MeetingServices
	partialPublisher transcriptservice.PartialPublisher
	statePublisher   transcriptservice.RealtimeStatePublisher
	speakerPublisher func(meetingID string, trackID string)
	speakerCancel    context.CancelFunc
}

// NewMeetingModule 创建会议模块；工作目录 ready 前不访问数据库或麦克风。
func NewMeetingModule(workspace *appbootstrap.Coordinator, capture port.AudioCapture, recordingConfig config.RecordingConfig, asrConfig config.ASRConfig) *MeetingModule {
	return &MeetingModule{workspace: workspace, capture: capture, recordingConfig: recordingConfig, asrConfig: asrConfig}
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
	module.mu.Lock()
	defer module.mu.Unlock()
	if module.services != nil && module.reader == reader {
		return module.services, nil
	}
	if module.speakerCancel != nil {
		module.speakerCancel()
		module.speakerCancel = nil
	}
	services, err := module.buildServices(reader, transactions)
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

// Recover 在业务 UI 开放后对遗留活动会议执行一次幂等文件对账。
func (module *MeetingModule) Recover(ctx context.Context) ([]meetingservice.RecoveryResult, error) {
	if module == nil || module.workspace == nil || module.workspace.GetState().Phase != domainworkspace.BootstrapPhaseReady {
		return []meetingservice.RecoveryResult{}, nil
	}
	services, err := module.Current()
	if err != nil {
		return nil, err
	}
	return services.Recovery.RecoverInterruptedMeetings(ctx)
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
func (module *MeetingModule) buildServices(reader *gorm.DB, transactions *database.TransactionManager) (*MeetingServices, error) {
	var metadata models.AppMetadata
	if err := reader.Select("id", "singleton_key", "product", "database_id", "device_code", "created_with_app_version", "created_at", "updated_at").
		Where("singleton_key = 1").Take(&metadata).Error; err != nil {
		return nil, fmt.Errorf("读取会议设备身份失败: %w", err)
	}
	settings := module.workspace.GetWorkspaceSettings()
	if settings.ActivePath == "" {
		return nil, fmt.Errorf("当前会议工作目录不可用")
	}
	repository := meetingrepository.NewRepository(reader)
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
		Repository: transcripts, WorkspaceRoot: settings.ActivePath, Debounce: 2 * time.Second,
	})
	rolling, err := speakerservice.NewRollingBuffer(120 * 16000)
	if err != nil {
		return nil, err
	}
	speakerRepository := speakerrepository.NewRepository(reader)
	meetingAudio, err := speakerservice.NewMeetingAudioReader(settings.ActivePath, repository, rolling, 120*16000)
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
		RawRecord:           rawRecord, WorkspaceRoot: settings.ActivePath,
		PublishPartial: module.partialPublisher, PublishState: module.statePublisher,
	})
	maxSamples := int64(module.recordingConfig.MaxSegmentSeconds * 16000)
	checkpointSamples := int64(module.recordingConfig.CheckpointSeconds * 16000)
	coordinator := meetingservice.NewRecordingCoordinator(
		module.capture, maxSamples, checkpointSamples,
		time.Duration(module.recordingConfig.FirstFrameTimeoutSeconds)*time.Second,
	)
	runtime := meetingservice.NewRuntimeService(meetingservice.RuntimeDependencies{
		Meetings: meetings, Repository: repository, Coordinator: coordinator,
		Capture: module.capture, Clock: currentClock, IDs: ids, WorkspaceRoot: settings.ActivePath,
		AvailableBytes:    filesystem.AvailableBytes,
		MinimumFreeBytes:  uint64(module.recordingConfig.MinimumFreeSpaceGiB) << 30,
		DeviceTestTimeout: time.Duration(module.recordingConfig.FirstFrameTimeoutSeconds) * time.Second,
		Transcript:        transcriptRuntime,
		PersistedPCMObserver: func(frame port.AudioFrame) {
			_ = writeRollingFrame(rolling, frame)
		},
	})
	recovery := meetingservice.NewRecoveryService(meetingservice.RecoveryDependencies{
		Repository: repository, WorkspaceRoot: settings.ActivePath, Clock: currentClock,
		IDs: identity.NewUUIDGenerator(), CheckpointSamples: checkpointSamples, Transcript: transcriptRecovery,
	})
	return &MeetingServices{Meetings: meetings, Runtime: runtime, Recovery: recovery, TranscriptSettings: transcriptSettings, TranscriptRuntime: transcriptRuntime, TranscriptTimeline: transcriptTimeline}, nil
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
