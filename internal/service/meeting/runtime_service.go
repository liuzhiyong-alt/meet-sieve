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

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"
)

// DiskSpaceReader 返回指定路径所在卷当前可用字节数。
type DiskSpaceReader func(path string) (uint64, error)

// RuntimeDependencies 描述会议录音纵向流程使用的真实边界。
type RuntimeDependencies struct {
	Meetings          *Service
	Repository        *meetingrepository.Repository
	Coordinator       *RecordingCoordinator
	Capture           port.AudioCapture
	Clock             clock.Clock
	IDs               identity.Generator
	WorkspaceRoot     string
	AvailableBytes    DiskSpaceReader
	MinimumFreeBytes  uint64
	DeviceTestTimeout time.Duration
	Transcript        MeetingTranscriptRuntime
	// PersistedPCMObserver 只观察已成功写入本地录音的 PCM，不得阻塞采集链路。
	PersistedPCMObserver PersistedPCMFrameHandler
}

// MeetingTranscriptRuntime 是录音运行时使用的会议级实时转写边界。
type MeetingTranscriptRuntime interface {
	Start(ctx context.Context, meetingID string, mode string) error
	TryAcceptFrame(frame port.AudioFrame) bool
	Stop(ctx context.Context, meetingID string, recordingEndSample int64) error
}

// StartMeetingInput 包含会议快照输入和本次选择的稳定设备 ID。
type StartMeetingInput struct {
	CreatePreparingInput
	DeviceID string
	ASRMode  string
}

// RuntimeService 编排工作目录、设备、文件和短事务，不把音频 I/O 放进数据库事务。
type RuntimeService struct {
	meetings          *Service
	repository        *meetingrepository.Repository
	coordinator       *RecordingCoordinator
	capture           port.AudioCapture
	clock             clock.Clock
	ids               identity.Generator
	workspaceRoot     string
	availableBytes    DiskSpaceReader
	minimumFreeBytes  uint64
	deviceTestTimeout time.Duration
	transcript        MeetingTranscriptRuntime
	persistedPCM      PersistedPCMFrameHandler
	endMu             sync.Mutex
	ending            *endMeetingCall
	lastEnded         *models.Meeting
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
		coordinator: dependencies.Coordinator, capture: dependencies.Capture, clock: dependencies.Clock, ids: dependencies.IDs,
		workspaceRoot: dependencies.WorkspaceRoot, availableBytes: dependencies.AvailableBytes,
		minimumFreeBytes:  dependencies.MinimumFreeBytes,
		persistedPCM:      dependencies.PersistedPCMObserver,
		deviceTestTimeout: deviceTestTimeout,
		transcript:        dependencies.Transcript,
	}
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
	segments, err := service.coordinator.Stop(ctx)
	if err != nil {
		return models.Meeting{}, service.failFinalizing(meetingID, err, apperr.CodeMeetingRecordingWriteFailed, "meeting.end.stop")
	}
	if service.transcript != nil {
		endSample := recordingEndSample(segments)
		if err = service.transcript.Stop(ctx, meetingID, endSample); err != nil {
			return models.Meeting{}, service.failFinalizing(meetingID, err, apperr.CodeASREventPersistFailed, "meeting.end.transcript")
		}
	}
	if err := service.persistSegments(ctx, meetingID, segments); err != nil {
		return models.Meeting{}, service.failFinalizing(meetingID, err, apperr.CodeMeetingRecordingWriteFailed, "meeting.end.segment_asset")
	}
	if err := service.mergeAndPersistFinal(ctx, current, segments); err != nil {
		return models.Meeting{}, service.failFinalizing(meetingID, err, apperr.CodeMeetingRecordingMergeFailed, "meeting.end.merge")
	}
	endedAt := service.clock.Now().UnixMilli()
	if err := service.repository.CompleteMeeting(ctx, meetingID, endedAt); err != nil {
		return models.Meeting{}, service.failFinalizing(meetingID, err, apperr.CodeMeetingRecordingWriteFailed, "meeting.end.state_commit")
	}
	return service.repository.GetMeeting(ctx, meetingID)
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
	return apperr.Dependency(code, cause, apperr.WithOp(operation))
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
