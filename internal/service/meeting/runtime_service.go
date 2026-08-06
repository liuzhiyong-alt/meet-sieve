package meeting

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	domainfinalization "meet-sieve/internal/domain/finalization"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// DiskSpaceReader 返回指定路径所在卷当前可用字节数。
type DiskSpaceReader func(path string) (uint64, error)

// RuntimeDependencies 描述会议录音纵向流程使用的真实边界。
type RuntimeDependencies struct {
	Meetings          *Service
	Repository        *meetingrepository.Repository
	Transactions      *database.TransactionManager
	Coordinator       *RecordingCoordinator
	Capture           port.AudioCapture
	Clock             clock.Clock
	IDs               identity.Generator
	WorkspaceRoot     string
	AvailableBytes    DiskSpaceReader
	MinimumFreeBytes  uint64
	DeviceTestTimeout time.Duration
	Transcript        MeetingTranscriptRuntime
	// RawRecord 在本地保存提交前从 SQLite 强制刷新原始记录投影。
	RawRecord RawRecordFlusher
	// PersistedPCMObserver 只观察已成功写入本地录音的 PCM，不得阻塞采集链路。
	PersistedPCMObserver PersistedPCMFrameHandler
	// LAN 在首帧安全提交后启动，并在录音收尾前停止访客写入。
	LAN LANMeetingLifecycle
	// Agent 在录音安全提交后异步初始化，并在会议收尾时关闭本场 session。
	Agent MeetingAgentLifecycle
	// AgentTurns 在核心收尾开始时中断会中任务，但不关闭 provider。
	AgentTurns MeetingAgentTurnLifecycle
	// PostMeeting 仅在 ended/saved 提交后异步接收处理信号。
	PostMeeting PostMeetingTrigger
	// FinalizationEvents 发布不含路径与底层错误的收尾状态。
	FinalizationEvents FinalizationEventSink
}

// MeetingTranscriptRuntime 是录音运行时使用的会议级实时转写边界。
type MeetingTranscriptRuntime interface {
	Start(ctx context.Context, meetingID string, mode string) error
	TryAcceptFrame(frame port.AudioFrame) bool
	Pause(ctx context.Context, meetingID string) (int64, error)
	Resume(ctx context.Context, meetingID string) (int64, error)
	Stop(ctx context.Context, meetingID string, recordingEndSample int64) error
}

// RawRecordFlusher 是核心收尾消费的原始记录强制刷新边界。
type RawRecordFlusher interface {
	Flush(context.Context, string) error
}

// LANMeetingLifecycle 是会议录音主流程消费的窄 LAN 生命周期边界。
type LANMeetingLifecycle interface {
	StartMeeting(context.Context, string, string) error
	StopMeeting(context.Context, string) error
}

// MeetingAgentLifecycle 是录音主流程使用的窄智能体生命周期边界。
type MeetingAgentLifecycle interface {
	Initialize(context.Context, string) error
	Shutdown(context.Context) error
}

// MeetingAgentTurnLifecycle 是核心收尾用于中断会中任务的窄边界。
type MeetingAgentTurnLifecycle interface {
	InterruptMeeting(context.Context, string) error
}

// PostMeetingTrigger 接收本地保存成功后的后台处理信号。
type PostMeetingTrigger interface {
	Trigger(string) bool
}

// FinalizationEventSink 发布可恢复的核心收尾状态。
type FinalizationEventSink interface {
	PublishFinalizationChanged(FinalizationSnapshot)
}

// FinalizationEventSinkFunc 让装配层以函数发布核心收尾状态。
type FinalizationEventSinkFunc func(FinalizationSnapshot)

// PublishFinalizationChanged 发布一条不含路径与底层错误的状态。
func (publisher FinalizationEventSinkFunc) PublishFinalizationChanged(snapshot FinalizationSnapshot) {
	if publisher != nil {
		publisher(snapshot)
	}
}

// StartMeetingInput 包含会议快照输入和本次选择的稳定设备 ID。
type StartMeetingInput struct {
	CreatePreparingInput
	DeviceID       string
	ASRMode        string
	LANEnabled     bool
	LANInterfaceID string
}

// RuntimeService 编排工作目录、设备、文件和短事务，不把音频 I/O 放进数据库事务。
type RuntimeService struct {
	meetings           *Service
	repository         *meetingrepository.Repository
	transactions       *database.TransactionManager
	coordinator        *RecordingCoordinator
	capture            port.AudioCapture
	clock              clock.Clock
	ids                identity.Generator
	workspaceRoot      string
	availableBytes     DiskSpaceReader
	minimumFreeBytes   uint64
	deviceTestTimeout  time.Duration
	transcript         MeetingTranscriptRuntime
	rawRecord          RawRecordFlusher
	lan                LANMeetingLifecycle
	agent              MeetingAgentLifecycle
	agentTurns         MeetingAgentTurnLifecycle
	postMeeting        PostMeetingTrigger
	finalizationEvents FinalizationEventSink
	persistedPCM       PersistedPCMFrameHandler
	endMu              sync.Mutex
	ending             *endMeetingCall
	lastEnded          *models.Meeting
	finalizationMu     sync.Mutex
	finalization       map[string]FinalizationSnapshot
}

// FinalizationSnapshot 是详情页可重建的安全核心收尾投影。
type FinalizationSnapshot struct {
	MeetingID string
	State     string
	Stage     domainfinalization.Stage
	ErrorCode string
	Revision  uint64
}

// endMeetingCall 让同一进程内的并发结束请求等待唯一收尾结果。
type endMeetingCall struct {
	meetingID string
	done      chan struct{}
	result    models.Meeting
	err       error
}

// NewRuntimeService 创建单工作目录的会议录音运行时服务。
func NewRuntimeService(dependencies RuntimeDependencies) *RuntimeService {
	deviceTestTimeout := dependencies.DeviceTestTimeout
	if deviceTestTimeout <= 0 {
		deviceTestTimeout = 5 * time.Second
	}
	return &RuntimeService{
		meetings: dependencies.Meetings, repository: dependencies.Repository,
		transactions: dependencies.Transactions,
		coordinator:  dependencies.Coordinator, capture: dependencies.Capture, clock: dependencies.Clock, ids: dependencies.IDs,
		workspaceRoot: dependencies.WorkspaceRoot, availableBytes: dependencies.AvailableBytes,
		minimumFreeBytes:   dependencies.MinimumFreeBytes,
		persistedPCM:       dependencies.PersistedPCMObserver,
		deviceTestTimeout:  deviceTestTimeout,
		transcript:         dependencies.Transcript,
		rawRecord:          dependencies.RawRecord,
		lan:                dependencies.LAN,
		agent:              dependencies.Agent,
		agentTurns:         dependencies.AgentTurns,
		postMeeting:        dependencies.PostMeeting,
		finalizationEvents: dependencies.FinalizationEvents,
		finalization:       make(map[string]FinalizationSnapshot),
	}
}

// PauseForTurn 原子登记 ASR 投递暂停；本地录音继续写入，不得丢弃 PCM。
func (service *RuntimeService) PauseForTurn(ctx context.Context, meetingID string, turnID string) error {
	if service == nil || ctx == nil || meetingID == "" || turnID == "" || service.transactions == nil || service.repository == nil || service.coordinator == nil || service.transcript == nil || service.clock == nil || service.ids == nil {
		return apperr.Dependency(apperr.CodeMeetingMediaPauseFailed, fmt.Errorf("媒体暂停依赖无效"), apperr.WithOp("meeting.media.pause.validate"))
	}
	now := service.clock.Now().UnixMilli()
	pause := models.MeetingMediaPause{
		ID: service.ids.New(), MeetingID: meetingID, AgentTurnID: turnID,
		Reason: "agent_voice_turn", State: "pausing", StartedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.CreateMediaPause(ctx, tx, pause)
	}); err != nil {
		return apperr.Dependency(apperr.CodeMeetingMediaPauseFailed, err, apperr.WithOp("meeting.media.pause.create"))
	}
	boundary, err := service.transcript.Pause(ctx, meetingID)
	if err != nil {
		service.recoverASRAfterPauseFailure(meetingID)
		service.failMediaPause(turnID, apperr.CodeASRPauseDrainFailed.ErrorCode)
		return apperr.Dependency(apperr.CodeASRPauseDrainFailed, err, apperr.WithOp("meeting.media.pause.asr"))
	}
	if err = service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.MarkMediaPaused(ctx, tx, turnID, boundary, boundary, service.clock.Now().UnixMilli())
	}); err != nil {
		service.recoverASRAfterPauseFailure(meetingID)
		service.failMediaPause(turnID, apperr.CodeMeetingMediaPauseFailed.ErrorCode)
		return apperr.Dependency(apperr.CodeMeetingMediaPauseFailed, err, apperr.WithOp("meeting.media.pause.commit"))
	}
	return nil
}

// ResumeAfterTurn 在 turn 终态后恢复新的 ASR session；本地录音在整个期间持续写入。
func (service *RuntimeService) ResumeAfterTurn(ctx context.Context, meetingID string, turnID string) error {
	if service == nil || ctx == nil || meetingID == "" || turnID == "" {
		return apperr.Dependency(apperr.CodeMeetingMediaResumeFailed, fmt.Errorf("媒体恢复参数无效"), apperr.WithOp("meeting.media.resume.validate"))
	}
	meeting, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return apperr.Dependency(apperr.CodeMeetingMediaResumeFailed, err, apperr.WithOp("meeting.media.resume.meeting"))
	}
	if meeting.LifecycleState != "recording" {
		return service.FinalizePausedTurn(ctx, meetingID, turnID)
	}
	if _, boundaryErr := service.currentPauseBoundary(ctx, turnID); boundaryErr != nil {
		service.failMediaPause(turnID, apperr.CodeMeetingMediaResumeFailed.ErrorCode)
		return apperr.Dependency(apperr.CodeMeetingMediaResumeFailed, boundaryErr, apperr.WithOp("meeting.media.resume.pause_fact"))
	}
	resumeBoundary, asrErr := service.transcript.Resume(ctx, meetingID)
	if asrErr != nil {
		service.failMediaPause(turnID, apperr.CodeMeetingMediaResumeFailed.ErrorCode)
		return apperr.Dependency(apperr.CodeMeetingMediaResumeFailed, asrErr, apperr.WithOp("meeting.media.resume.asr"))
	}
	completeErr := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.CompleteMediaPause(ctx, tx, turnID, resumeBoundary, 0, service.clock.Now().UnixMilli())
	})
	if completeErr != nil {
		return apperr.Dependency(apperr.CodeMeetingMediaResumeFailed, completeErr, apperr.WithOp("meeting.media.resume.commit"))
	}
	return nil
}

// FinalizePausedTurn 在会议收尾竞态下关闭暂停事实但不重新打开录音门。
func (service *RuntimeService) FinalizePausedTurn(ctx context.Context, _ string, turnID string) error {
	if service == nil || service.transactions == nil || turnID == "" {
		return nil
	}
	now := service.clock.Now().UnixMilli()
	return service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.repository.FailMediaPause(ctx, tx, turnID, apperr.CodeAgentTurnCancelled.ErrorCode, now)
	})
}

// currentPauseBoundary 读取已确认逻辑边界；读取失败时返回 0 并让后续恢复返回稳定错误。
func (service *RuntimeService) currentPauseBoundary(ctx context.Context, turnID string) (int64, error) {
	pause, err := service.repository.GetMediaPause(ctx, turnID)
	if err != nil {
		return 0, fmt.Errorf("读取媒体暂停边界失败：%w", err)
	}
	if pause.LogicalSample == nil {
		return 0, fmt.Errorf("媒体暂停边界尚未确认")
	}
	return *pause.LogicalSample, nil
}

// recoverASRAfterPauseFailure 在 Codex 启动前尽力恢复实时转写；本地录音从未暂停。
func (service *RuntimeService) recoverASRAfterPauseFailure(meetingID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = service.transcript.Resume(ctx, meetingID)
}

// failMediaPause 尽力收口失败事实，原始 cause 由调用方返回并记录。
func (service *RuntimeService) failMediaPause(turnID string, errorCode string) {
	if service == nil || service.transactions == nil {
		return
	}
	now := service.clock.Now().UnixMilli()
	_ = service.transactions.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return service.repository.FailMediaPause(context.Background(), tx, turnID, errorCode, now)
	})
}

// MicrophoneState 返回录音运行时的真实麦克风状态，不根据页面计时猜测。
func (service *RuntimeService) MicrophoneState() string {
	if service == nil || service.coordinator == nil {
		return "unavailable"
	}
	if service.coordinator.IsCapturing() {
		return "capturing"
	}
	return "stopped"
}

// RealtimeASRState 用持久化媒体暂停事实覆盖瞬时 ASR 状态，供页面刷新后稳定重建。
func (service *RuntimeService) RealtimeASRState(ctx context.Context, meetingID string, fallback string) string {
	if service == nil || service.repository == nil || ctx == nil || meetingID == "" {
		return fallback
	}
	paused, err := service.repository.HasActiveMediaPause(ctx, meetingID)
	if err == nil && paused {
		return "paused_for_ai"
	}
	return fallback
}

// EndMeeting 幂等执行唯一安全收尾；并发调用复用同一结果。
func (service *RuntimeService) EndMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	call, owner, cached := service.reserveEnd(meetingID)
	if cached != nil {
		return *cached, nil
	}
	if !owner {
		select {
		case <-ctx.Done():
			return models.Meeting{}, ctx.Err()
		case <-call.done:
			return call.result, call.err
		}
	}
	call.result, call.err = service.finishMeeting(ctx, meetingID)
	service.completeEnd(call)
	return call.result, call.err
}

// reserveEnd 决定当前调用是唯一收尾者、等待者还是已完成结果读取者。
func (service *RuntimeService) reserveEnd(meetingID string) (*endMeetingCall, bool, *models.Meeting) {
	service.endMu.Lock()
	defer service.endMu.Unlock()
	if service.lastEnded != nil && service.lastEnded.ID == meetingID {
		cached := *service.lastEnded
		return nil, false, &cached
	}
	if service.ending != nil && service.ending.meetingID == meetingID {
		return service.ending, false, nil
	}
	call := &endMeetingCall{meetingID: meetingID, done: make(chan struct{})}
	service.ending = call
	return call, true, nil
}

// completeEnd 发布唯一收尾结果；仅成功结果进入长期幂等缓存。
func (service *RuntimeService) completeEnd(call *endMeetingCall) {
	service.endMu.Lock()
	if call.err == nil {
		result := call.result
		service.lastEnded = &result
	}
	if service.ending == call {
		service.ending = nil
	}
	close(call.done)
	service.endMu.Unlock()
}

// finishMeeting 完成状态抢占、尾片关闭、资产登记、合并校验和终态提交。
func (service *RuntimeService) finishMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	if service == nil || service.repository == nil || service.coordinator == nil || service.clock == nil || service.ids == nil {
		return models.Meeting{}, fmt.Errorf("会议结束运行时依赖无效")
	}
	service.setFinalizationState(meetingID, "running", domainfinalization.StageStopLAN, "")
	if service.lan != nil {
		// LAN 停止失败不阻断录音安全收尾；Runtime 已先清除内存令牌和 Listener。
		_ = service.lan.StopMeeting(ctx, meetingID)
	}
	current, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return models.Meeting{}, err
	}
	if current.LifecycleState == "ended" {
		return current, nil
	}
	if current.LifecycleState == "interrupted" {
		return models.Meeting{}, apperr.Biz(apperr.CodeMeetingRecoveryRequired, apperr.WithOp("meeting.end.interrupted"))
	}
	if current.LifecycleState == "recording" {
		if err := service.repository.BeginFinalizing(ctx, meetingID, service.clock.Now().UnixMilli()); err != nil {
			return models.Meeting{}, err
		}
	} else if current.LifecycleState == "finalizing" && current.LocalSaveState == "failed" {
		if err := service.repository.ResumeFinalizing(ctx, meetingID, service.clock.Now().UnixMilli()); err != nil {
			return models.Meeting{}, err
		}
	} else if current.LifecycleState != "finalizing" {
		return models.Meeting{}, meetingrepository.ErrMeetingStateConflict
	}
	result, err := (&CoreFinalizer{service: service, meeting: current}).Run(ctx)
	if err == nil && service.postMeeting != nil {
		service.postMeeting.Trigger(meetingID)
	}
	return result, err
}

// GetFinalizationState 返回内存阶段；不存在时根据会议持久状态重建稳定投影。
func (service *RuntimeService) GetFinalizationState(ctx context.Context, meetingID string) (FinalizationSnapshot, error) {
	service.finalizationMu.Lock()
	state, exists := service.finalization[meetingID]
	service.finalizationMu.Unlock()
	if exists {
		return state, nil
	}
	meeting, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return FinalizationSnapshot{}, err
	}
	value := "idle"
	if meeting.LifecycleState == "finalizing" {
		value = "failed"
	}
	if (meeting.LifecycleState == "ended" || meeting.LifecycleState == "interrupted") && meeting.LocalSaveState == "saved" {
		value = "completed"
	}
	return FinalizationSnapshot{MeetingID: meetingID, State: value}, nil
}

// setFinalizationState 更新阶段后在锁外发布有限状态。
func (service *RuntimeService) setFinalizationState(meetingID string, state string, stage domainfinalization.Stage, errorCode string) {
	service.finalizationMu.Lock()
	previous := service.finalization[meetingID]
	snapshot := FinalizationSnapshot{MeetingID: meetingID, State: state, Stage: stage, ErrorCode: errorCode, Revision: previous.Revision + 1}
	service.finalization[meetingID] = snapshot
	service.finalizationMu.Unlock()
	if service.finalizationEvents != nil {
		service.finalizationEvents.PublishFinalizationChanged(snapshot)
	}
}

// persistSegments 把每个已完成 WAV 的真实元数据登记为 ready microphone 资产。
func (service *RuntimeService) persistSegments(ctx context.Context, meetingID string, segments []CompletedSegment) error {
	for _, segment := range segments {
		if err := service.persistSegment(ctx, meetingID, segment); err != nil {
			return err
		}
	}
	return nil
}

// persistSegment 将单个完成 WAV 登记为不可变的 ready microphone 资产。
func (service *RuntimeService) persistSegment(ctx context.Context, meetingID string, segment CompletedSegment) error {
	relativePath, err := service.relativeWorkspacePath(segment.Path)
	if err != nil {
		return err
	}
	assetID := service.ids.New()
	if !isUUIDv4(assetID) {
		return fmt.Errorf("生成音频资产 UUID 失败")
	}
	now := service.clock.Now().UnixMilli()
	return service.repository.CreateReadyAudioAsset(ctx, models.AudioAsset{
		ID: assetID, MeetingID: meetingID, Kind: "microphone", SequenceNo: segment.SequenceNo,
		RelativePath: relativePath, StartSample: segment.StartSample, EndSample: segment.EndSample,
		SampleRate: recordingSampleRate, BitDepth: recordingBitDepth, Channels: recordingChannels,
		SizeBytes: segment.SizeBytes, SHA256: segment.SHA256, State: "ready", CreatedAt: now, UpdatedAt: now,
	})
}

// mergeAndPersistFinal 合并连续分片，计算最终哈希并登记 mixed ready 资产。
func (service *RuntimeService) mergeAndPersistFinal(ctx context.Context, meeting models.Meeting, segments []CompletedSegment) error {
	if len(segments) == 0 {
		return fmt.Errorf("没有可合并的录音分片")
	}
	paths := make([]string, 0, len(segments))
	for _, segment := range segments {
		paths = append(paths, segment.Path)
	}
	audioDirectory := filepath.Join(service.workspaceRoot, filepath.FromSlash(meeting.RelativeDir), "audio")
	readyPath := filepath.Join(audioDirectory, "recording.wav")
	expectedSamples := segments[len(segments)-1].EndSample
	result := WAVWriteResult{SampleCount: expectedSamples}
	if info, statErr := os.Stat(readyPath); statErr == nil {
		pcm, readErr := readCanonicalWAV(readyPath)
		if readErr != nil || int64(len(pcm)/2) != expectedSamples {
			return fmt.Errorf("已有完整录音与分片不一致")
		}
		result.SizeBytes = info.Size()
	} else {
		var mergeErr error
		result, mergeErr = MergeWAVSegments(paths, readyPath, service.coordinator.checkpointSamples)
		if mergeErr != nil {
			return mergeErr
		}
	}
	digest, err := filesystem.SHA256File(readyPath)
	if err != nil {
		return err
	}
	relativePath, err := service.relativeWorkspacePath(readyPath)
	if err != nil {
		return err
	}
	assetID := service.ids.New()
	if !isUUIDv4(assetID) {
		return fmt.Errorf("生成最终音频资产 UUID 失败")
	}
	now := service.clock.Now().UnixMilli()
	return service.repository.CreateReadyAudioAsset(ctx, models.AudioAsset{
		ID: assetID, MeetingID: meeting.ID, Kind: "mixed", SequenceNo: 1, RelativePath: relativePath,
		StartSample: 0, EndSample: result.SampleCount, SampleRate: recordingSampleRate,
		BitDepth: recordingBitDepth, Channels: recordingChannels, SizeBytes: result.SizeBytes,
		SHA256: digest, State: "ready", CreatedAt: now, UpdatedAt: now,
	})
}

// relativeWorkspacePath 将已确认位于工作目录内的绝对路径转换为数据库相对路径。
func (service *RuntimeService) relativeWorkspacePath(path string) (string, error) {
	relative, err := filepath.Rel(service.workspaceRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("录音文件不在会议工作目录内")
	}
	return filepath.ToSlash(relative), nil
}

// failFinalizing 标记准确失败状态并返回保留底层 cause 的安全错误。
func (service *RuntimeService) failFinalizing(meetingID string, cause error, code apperr.Code, operation string) error {
	_ = service.repository.MarkFinalizingFailed(context.Background(), meetingID, service.clock.Now().UnixMilli())
	service.setFinalizationState(meetingID, "failed", service.currentFinalizationStage(meetingID), code.ErrorCode)
	return apperr.Dependency(code, cause, apperr.WithOp(operation))
}

// currentFinalizationStage 返回失败发生时最后发布的阶段。
func (service *RuntimeService) currentFinalizationStage(meetingID string) domainfinalization.Stage {
	service.finalizationMu.Lock()
	defer service.finalizationMu.Unlock()
	return service.finalization[meetingID].Stage
}

// StartMeeting 只有首帧文件与 recording/saving 状态均提交后才返回成功。
func (service *RuntimeService) StartMeeting(ctx context.Context, input StartMeetingInput) (models.Meeting, error) {
	if err := service.preflight(ctx, input.DeviceID); err != nil {
		return models.Meeting{}, err
	}
	created, err := service.meetings.CreatePreparing(ctx, input.CreatePreparingInput)
	if err != nil {
		return models.Meeting{}, err
	}
	segmentsDirectory, err := service.createMeetingDirectories(created)
	if err != nil {
		service.compensateEmptyPreparing(ctx, created.ID, "")
		return models.Meeting{}, err
	}
	if err := service.coordinator.SetCompletedSegmentHandler(func(handlerContext context.Context, segment CompletedSegment) error {
		return service.persistSegment(handlerContext, created.ID, segment)
	}); err != nil {
		service.compensateEmptyPreparing(ctx, created.ID, segmentsDirectory)
		return models.Meeting{}, err
	}
	if err := service.coordinator.SetFailureHandler(func(handlerContext context.Context, _ error) {
		if service.lan != nil {
			_ = service.lan.StopMeeting(handlerContext, created.ID)
		}
		_ = service.repository.InterruptMeeting(handlerContext, created.ID, service.clock.Now().UnixMilli())
	}); err != nil {
		service.compensateEmptyPreparing(ctx, created.ID, segmentsDirectory)
		return models.Meeting{}, err
	}
	if service.transcript != nil {
		if err = service.transcript.Start(ctx, created.ID, input.ASRMode); err != nil {
			service.compensateEmptyPreparing(ctx, created.ID, segmentsDirectory)
			return models.Meeting{}, err
		}
	}
	if service.transcript != nil || service.persistedPCM != nil {
		if err = service.coordinator.SetPersistedPCMFrameHandler(func(frame port.AudioFrame) {
			if service.transcript != nil {
				service.transcript.TryAcceptFrame(frame)
			}
			if service.persistedPCM != nil {
				service.persistedPCM(frame)
			}
		}); err != nil {
			if service.transcript != nil {
				_ = service.transcript.Stop(context.Background(), created.ID, 0)
			}
			service.compensateEmptyPreparing(ctx, created.ID, segmentsDirectory)
			return models.Meeting{}, err
		}
	}
	startedAt := service.clock.Now().UnixMilli()
	if err := service.coordinator.Start(ctx, input.DeviceID, segmentsDirectory); err != nil {
		if service.transcript != nil {
			_ = service.transcript.Stop(context.Background(), created.ID, 0)
		}
		service.compensateFailedStart(ctx, created.ID, segmentsDirectory)
		return models.Meeting{}, err
	}
	if err := service.repository.MarkRecordingStarted(ctx, created.ID, startedAt); err != nil {
		_, _ = service.coordinator.Stop(context.Background())
		if service.transcript != nil {
			_ = service.transcript.Stop(context.Background(), created.ID, recordingEndSample(nil))
		}
		_ = service.repository.InterruptMeeting(context.Background(), created.ID, service.clock.Now().UnixMilli())
		return models.Meeting{}, apperr.Dependency(apperr.CodeMeetingRecordingWriteFailed, err, apperr.WithOp("meeting.start.state_commit"))
	}
	if err := service.coordinator.Activate(); err != nil {
		_ = service.repository.InterruptMeeting(context.Background(), created.ID, service.clock.Now().UnixMilli())
		return models.Meeting{}, apperr.Dependency(apperr.CodeMeetingRecordingWriteFailed, err, apperr.WithOp("meeting.start.runner_activate"))
	}
	if input.LANEnabled && service.lan != nil {
		// LAN 是独立能力；启动失败只记录在 LAN 状态轴，会议录音仍成功。
		_ = service.lan.StartMeeting(ctx, created.ID, input.LANInterfaceID)
	}
	if service.agent != nil {
		// AI 是独立状态轴；初始化失败由其自身事务收敛，绝不回滚已开始的录音。
		go func(meetingID string) { _ = service.agent.Initialize(context.Background(), meetingID) }(created.ID)
	}
	created.LifecycleState = "recording"
	created.LocalSaveState = "saving"
	created.StartedAt = &startedAt
	created.UpdatedAt = startedAt
	return service.repository.GetMeeting(ctx, created.ID)
}

// recordingEndSample 返回连续录音分片的最终样本边界。
func recordingEndSample(segments []CompletedSegment) int64 {
	if len(segments) == 0 {
		return 0
	}
	return segments[len(segments)-1].EndSample
}

// preflight 在产生会议、序号或录音文件前验证目录、空间和设备。
func (service *RuntimeService) preflight(ctx context.Context, deviceID string) error {
	if service == nil || service.meetings == nil || service.repository == nil || service.coordinator == nil ||
		service.capture == nil || service.clock == nil || service.availableBytes == nil ||
		!filepath.IsAbs(service.workspaceRoot) || service.minimumFreeBytes == 0 {
		return fmt.Errorf("会议录音运行时依赖无效")
	}
	if err := filesystem.ProbeWritable(service.workspaceRoot); err != nil {
		return apperr.Dependency(apperr.CodeMeetingWorkspaceUnavailable, err, apperr.WithOp("meeting.start.workspace"))
	}
	available, err := service.availableBytes(service.workspaceRoot)
	if err != nil {
		return apperr.Dependency(apperr.CodeMeetingWorkspaceUnavailable, err, apperr.WithOp("meeting.start.disk_query"))
	}
	if available < service.minimumFreeBytes {
		return apperr.Biz(apperr.CodeMeetingDiskSpaceLow, apperr.WithOp("meeting.start.disk_space"))
	}
	return service.testInputDevice(ctx, deviceID)
}

// testInputDevice 为可能阻塞的原生设备打开设置硬超时，且不在超时后创建任何会议事实。
func (service *RuntimeService) testInputDevice(ctx context.Context, deviceID string) error {
	testContext, cancel := context.WithTimeout(ctx, service.deviceTestTimeout)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- service.capture.TestInputDevice(testContext, deviceID) }()
	select {
	case err := <-result:
		if err == nil {
			return nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return apperr.Dependency(apperr.CodeMeetingAudioStartTimeout, err, apperr.WithOp("meeting.start.device_test"))
		}
		return mapMeetingAudioDeviceError(err, "meeting.start.device_test")
	case <-testContext.Done():
		if errors.Is(testContext.Err(), context.DeadlineExceeded) {
			return apperr.Dependency(apperr.CodeMeetingAudioStartTimeout, testContext.Err(), apperr.WithOp("meeting.start.device_test"))
		}
		return testContext.Err()
	}
}

// mapMeetingAudioDeviceError 区分系统权限拒绝与设备不可用，并保留会议页语义。
func mapMeetingAudioDeviceError(cause error, operation string) error {
	if errors.Is(cause, port.ErrAudioPermissionDenied) {
		return apperr.Dependency(apperr.CodeMeetingAudioPermissionDenied, cause, apperr.WithOp(operation))
	}
	return apperr.Dependency(apperr.CodeMeetingAudioDeviceUnavailable, cause, apperr.WithOp(operation))
}

// createMeetingDirectories 独占创建当前会议目录，绝不覆盖同名历史文件。
func (service *RuntimeService) createMeetingDirectories(meeting models.Meeting) (string, error) {
	meetingDirectory := filepath.Join(service.workspaceRoot, filepath.FromSlash(meeting.RelativeDir))
	if err := os.MkdirAll(filepath.Dir(meetingDirectory), 0o700); err != nil {
		return "", apperr.Dependency(apperr.CodeMeetingWorkspaceUnavailable, err, apperr.WithOp("meeting.start.meetings_directory"))
	}
	if err := os.Mkdir(meetingDirectory, 0o700); err != nil {
		code := apperr.CodeMeetingWorkspaceUnavailable
		if errors.Is(err, os.ErrExist) {
			code = apperr.CodeMeetingDirectoryConflict
		}
		return "", apperr.Dependency(code, err, apperr.WithOp("meeting.start.meeting_directory"))
	}
	audioDirectory := filepath.Join(meetingDirectory, "audio")
	segmentsDirectory := filepath.Join(audioDirectory, "segments")
	if err := os.MkdirAll(segmentsDirectory, 0o700); err != nil {
		return "", apperr.Dependency(apperr.CodeMeetingWorkspaceUnavailable, err, apperr.WithOp("meeting.start.audio_directory"))
	}
	return segmentsDirectory, nil
}

// compensateFailedStart 仅清理完全空的本次目录；已有 PCM 时保留并转入恢复。
func (service *RuntimeService) compensateFailedStart(ctx context.Context, meetingID string, segmentsDirectory string) {
	entries, err := os.ReadDir(segmentsDirectory)
	if err == nil && len(entries) == 0 {
		service.compensateEmptyPreparing(ctx, meetingID, segmentsDirectory)
		return
	}
	_ = service.repository.InterruptMeeting(context.Background(), meetingID, service.clock.Now().UnixMilli())
}

// compensateEmptyPreparing 删除数据库 preparing 事实和本次创建的空目录层级。
func (service *RuntimeService) compensateEmptyPreparing(ctx context.Context, meetingID string, segmentsDirectory string) {
	if err := service.repository.DeletePreparing(ctx, meetingID); err != nil {
		return
	}
	if segmentsDirectory == "" {
		return
	}
	_ = os.Remove(segmentsDirectory)
	_ = os.Remove(filepath.Dir(segmentsDirectory))
	_ = os.Remove(filepath.Dir(filepath.Dir(segmentsDirectory)))
}
