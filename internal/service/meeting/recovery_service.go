package meeting

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	meetingrepository "meet-sieve/internal/repository/meeting"
	"meet-sieve/models"
)

// RecoveryDependencies 描述启动录音恢复需要的持久化与文件边界。
type RecoveryDependencies struct {
	Repository        *meetingrepository.Repository
	WorkspaceRoot     string
	Clock             clock.Clock
	IDs               identity.Generator
	CheckpointSamples int64
	Transcript        TranscriptRecovery
}

// TranscriptRecovery 收敛遗留 ASR session，并按最终录音样本生成 recovery gap。
type TranscriptRecovery interface {
	Recover(ctx context.Context, meetingID string, recordingEndSample int64) error
}

// RecoveryResult 是供 UI 展示的单场会议恢复事实，不允许恢复原录音会话。
type RecoveryResult struct {
	MeetingID          string
	RepairedParts      int
	SegmentsRecovered  int
	RecordingRecovered bool
	ErrorCode          string
}

// RecoveryService 按文件事实修复遗留录音，并把活动会议收敛为 interrupted。
type RecoveryService struct {
	repository        *meetingrepository.Repository
	workspaceRoot     string
	clock             clock.Clock
	ids               identity.Generator
	checkpointSamples int64
	transcript        TranscriptRecovery
}

// NewRecoveryService 创建不会在构造阶段扫描目录或访问数据库的恢复服务。
func NewRecoveryService(dependencies RecoveryDependencies) *RecoveryService {
	return &RecoveryService{
		repository: dependencies.Repository, workspaceRoot: dependencies.WorkspaceRoot,
		clock: dependencies.Clock, ids: dependencies.IDs, checkpointSamples: dependencies.CheckpointSamples,
		transcript: dependencies.Transcript,
	}
}

// RecoverInterruptedMeetings 扫描所有遗留活动会议；单场失败保留材料并继续处理其他会议。
func (service *RecoveryService) RecoverInterruptedMeetings(ctx context.Context) ([]RecoveryResult, error) {
	if service == nil || service.repository == nil || service.clock == nil || service.ids == nil ||
		!filepath.IsAbs(service.workspaceRoot) || service.checkpointSamples <= 0 {
		return nil, fmt.Errorf("会议恢复服务依赖无效")
	}
	meetings, err := service.repository.ListActiveMeetings(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]RecoveryResult, 0, len(meetings))
	for _, meeting := range meetings {
		result, recoveryErr := service.recoverMeeting(ctx, meeting)
		if recoveryErr != nil {
			result.ErrorCode = apperr.CodeMeetingRecoveryFailed.ErrorCode
			_ = service.repository.FinishRecovery(ctx, meeting.ID, false, service.clock.Now().UnixMilli())
		} else if err := service.repository.FinishRecovery(ctx, meeting.ID, true, service.clock.Now().UnixMilli()); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// RetryInterruptedMeeting 对指定 interrupted 会议重试文件对账和合并，但绝不恢复原录音会话。
func (service *RecoveryService) RetryInterruptedMeeting(ctx context.Context, meetingID string) (RecoveryResult, error) {
	meeting, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return RecoveryResult{}, err
	}
	if meeting.LifecycleState != "interrupted" {
		return RecoveryResult{}, fmt.Errorf("只有中断会议可以重试恢复")
	}
	result, err := service.recoverMeeting(ctx, meeting)
	if err != nil {
		result.ErrorCode = apperr.CodeMeetingRecoveryFailed.ErrorCode
		_ = service.repository.UpdateInterruptedRecovery(ctx, meetingID, false, service.clock.Now().UnixMilli())
		return result, apperr.Dependency(apperr.CodeMeetingRecoveryFailed, err, apperr.WithOp("meeting.recovery.retry"))
	}
	if err := service.repository.UpdateInterruptedRecovery(ctx, meetingID, true, service.clock.Now().UnixMilli()); err != nil {
		return result, err
	}
	return result, nil
}

// recoverMeeting 修复一场会议的分片、资产与完整录音，不修改会议生命周期。
func (service *RecoveryService) recoverMeeting(ctx context.Context, meeting models.Meeting) (RecoveryResult, error) {
	result := RecoveryResult{MeetingID: meeting.ID}
	audioDirectory := filepath.Join(service.workspaceRoot, filepath.FromSlash(meeting.RelativeDir), "audio")
	segments, repaired, err := service.recoverSegments(ctx, meeting.ID, filepath.Join(audioDirectory, "segments"))
	result.RepairedParts = repaired
	result.SegmentsRecovered = len(segments)
	if err != nil {
		return result, err
	}
	if err := service.recoverFinalAsset(ctx, meeting.ID, audioDirectory, segments); err != nil {
		return result, err
	}
	if service.transcript != nil {
		if err := service.transcript.Recover(ctx, meeting.ID, segments[len(segments)-1].EndSample); err != nil {
			return result, err
		}
	}
	result.RecordingRecovered = true
	return result, nil
}

// recoverSegments 修复 part，并按连续序号和样本长度补建 microphone ready 资产。
func (service *RecoveryService) recoverSegments(ctx context.Context, meetingID string, directory string) ([]CompletedSegment, int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, 0, fmt.Errorf("读取恢复分片目录失败: %w", err)
	}
	readyPaths := make([]string, 0, len(entries))
	repaired := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if strings.HasSuffix(entry.Name(), ".wav.part") {
			readyPath := strings.TrimSuffix(path, ".part")
			if _, statErr := os.Stat(readyPath); statErr == nil {
				return nil, repaired, fmt.Errorf("同一分片同时存在 ready 与 part")
			}
			if _, repairErr := RepairWAVPart(path, readyPath); repairErr != nil {
				return nil, repaired, repairErr
			}
			repaired++
			readyPaths = append(readyPaths, readyPath)
		} else if strings.HasSuffix(entry.Name(), ".wav") {
			readyPaths = append(readyPaths, path)
		}
	}
	sort.Strings(readyPaths)
	return service.inspectAndPersistSegments(ctx, meetingID, readyPaths, repaired)
}

// inspectAndPersistSegments 校验序号和样本连续性，并用真实文件信息登记资产。
func (service *RecoveryService) inspectAndPersistSegments(ctx context.Context, meetingID string, paths []string, repaired int) ([]CompletedSegment, int, error) {
	if len(paths) == 0 {
		return nil, repaired, fmt.Errorf("没有可恢复的录音分片")
	}
	segments := make([]CompletedSegment, 0, len(paths))
	nextSample := int64(0)
	for index, path := range paths {
		sequence, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(path), ".wav"))
		if err != nil || sequence != index+1 {
			return nil, repaired, fmt.Errorf("录音分片序号不连续")
		}
		pcm, err := readCanonicalWAV(path)
		if err != nil || len(pcm) == 0 {
			return nil, repaired, fmt.Errorf("恢复录音分片无效: %w", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, repaired, err
		}
		digest, err := filesystem.SHA256File(path)
		if err != nil {
			return nil, repaired, err
		}
		segment := CompletedSegment{
			SequenceNo: sequence, Path: path, StartSample: nextSample,
			EndSample: nextSample + int64(len(pcm)/2), SizeBytes: info.Size(), SHA256: digest,
		}
		if err := service.persistRecoveredAsset(ctx, meetingID, segment); err != nil {
			return nil, repaired, err
		}
		segments = append(segments, segment)
		nextSample = segment.EndSample
	}
	return segments, repaired, nil
}

// persistRecoveredAsset 幂等补建一个真实完成分片的 ready 元数据。
func (service *RecoveryService) persistRecoveredAsset(ctx context.Context, meetingID string, segment CompletedSegment) error {
	relativePath, err := service.relativePath(segment.Path)
	if err != nil {
		return err
	}
	now := service.clock.Now().UnixMilli()
	return service.repository.CreateReadyAudioAsset(ctx, models.AudioAsset{
		ID: service.ids.New(), MeetingID: meetingID, Kind: "microphone", SequenceNo: segment.SequenceNo,
		RelativePath: relativePath, StartSample: segment.StartSample, EndSample: segment.EndSample,
		SampleRate: recordingSampleRate, BitDepth: recordingBitDepth, Channels: recordingChannels,
		SizeBytes: segment.SizeBytes, SHA256: segment.SHA256, State: "ready", CreatedAt: now, UpdatedAt: now,
	})
}

// recoverFinalAsset 从连续分片重建 recording.wav 并登记 mixed ready 资产。
func (service *RecoveryService) recoverFinalAsset(ctx context.Context, meetingID string, audioDirectory string, segments []CompletedSegment) error {
	paths := make([]string, 0, len(segments))
	for _, segment := range segments {
		paths = append(paths, segment.Path)
	}
	readyPath := filepath.Join(audioDirectory, "recording.wav")
	result := WAVWriteResult{SampleCount: segments[len(segments)-1].EndSample}
	if info, statErr := os.Stat(readyPath); statErr == nil {
		pcm, readErr := readCanonicalWAV(readyPath)
		if readErr != nil || int64(len(pcm)/2) != result.SampleCount {
			return fmt.Errorf("已有完整录音与分片不一致")
		}
		result.SizeBytes = info.Size()
	} else {
		var mergeErr error
		result, mergeErr = MergeWAVSegments(paths, readyPath, service.checkpointSamples)
		if mergeErr != nil {
			return mergeErr
		}
	}
	digest, err := filesystem.SHA256File(readyPath)
	if err != nil {
		return err
	}
	relativePath, err := service.relativePath(readyPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(readyPath)
	if err != nil {
		return err
	}
	now := service.clock.Now().UnixMilli()
	return service.repository.CreateReadyAudioAsset(ctx, models.AudioAsset{
		ID: service.ids.New(), MeetingID: meetingID, Kind: "mixed", SequenceNo: 1,
		RelativePath: relativePath, StartSample: 0, EndSample: result.SampleCount,
		SampleRate: recordingSampleRate, BitDepth: recordingBitDepth, Channels: recordingChannels,
		SizeBytes: info.Size(), SHA256: digest, State: "ready", CreatedAt: now, UpdatedAt: now,
	})
}

// relativePath 将已恢复文件约束为当前工作目录内的相对路径。
func (service *RecoveryService) relativePath(path string) (string, error) {
	relative, err := filepath.Rel(service.workspaceRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("恢复文件不在工作目录内")
	}
	return filepath.ToSlash(relative), nil
}
