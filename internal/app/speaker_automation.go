package app

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	appbuild "meet-sieve/internal/app/buildinfo"
	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	speakerrepository "meet-sieve/internal/repository/speaker"
	speakerservice "meet-sieve/internal/service/speaker"
)

const matchingProfileFilename = "voice-matching-profile.json"

// lazySpeakerProcessor 在每次持久任务执行时读取当前 encoder/profile，允许模型异步启动后自动恢复。
type lazySpeakerProcessor struct {
	voice        *VoiceModule
	repository   *speakerrepository.Repository
	transactions *database.TransactionManager
	evidence     *speakerservice.EvidenceBuilder
	unknown      *speakerservice.UnknownAssigner
	clock        clock.Clock
	onChanged    func(meetingID string, trackID string)
}

// lazyContinuityProcessor 为每条 pending evidence 动态读取模型/profile 并执行短窗路由。
type lazyContinuityProcessor struct {
	voice        *VoiceModule
	repository   *speakerrepository.Repository
	transactions *database.TransactionManager
	audio        speakerservice.EvidenceAudioReader
	clock        clock.Clock
	onRouted     func(meetingID string, trackID string, projectionChanged bool)
}

// Ready 仅在模型和带 continuity 的 schema v2 档案均可用时消费任务。
func (processor *lazyContinuityProcessor) Ready(context.Context) bool {
	if processor == nil || processor.voice == nil {
		return false
	}
	_, profile, err := processor.voice.speakerAutomation()
	return err == nil && profile.Continuity != nil
}

// Process 创建短生命周期 ContinuityRouter，保证模型热切换后读取最新合法依赖。
func (processor *lazyContinuityProcessor) Process(ctx context.Context, evidenceID string, finalizing bool) error {
	if processor == nil || processor.voice == nil {
		return apperr.Biz(apperr.CodeSpeakerModelUnavailable, apperr.WithOp("speaker.continuity.voice"))
	}
	encoder, profile, err := processor.voice.speakerAutomation()
	if err != nil {
		return err
	}
	router := speakerservice.NewContinuityRouter(speakerservice.ContinuityRouterDependencies{
		Repository: processor.repository, Transactions: processor.transactions, Audio: processor.audio,
		Encoder: encoder, Profile: profile, IDs: identity.NewUUIDGenerator(), Clock: processor.clock,
		OnRouted: processor.onRouted,
	})
	return router.Process(ctx, evidenceID, finalizing)
}

// Ready 动态检查模型和正式校准档案；未就绪时不消费可恢复的 SQLite 任务。
func (processor *lazySpeakerProcessor) Ready(context.Context) bool {
	if processor == nil || processor.voice == nil {
		return false
	}
	state, _ := processor.voice.speakerAutomationStatus()
	return state == string(speakerdomain.AutomationReady)
}

// Process 只在模型与真实校准档案均就绪时创建一次短生命周期 Runner。
func (processor *lazySpeakerProcessor) Process(ctx context.Context, trackID string, finalizing bool) error {
	if processor == nil || processor.voice == nil {
		return apperr.Biz(apperr.CodeSpeakerModelUnavailable, apperr.WithOp("speaker.automation.voice"))
	}
	encoder, profile, err := processor.voice.speakerAutomation()
	if err != nil {
		return err
	}
	meetingFinalized, err := processor.repository.IsTrackMeetingFinalized(ctx, trackID)
	if err != nil {
		return err
	}
	runner := speakerservice.NewRunner(speakerservice.RunnerDependencies{
		Repository: processor.repository, Transactions: processor.transactions, Evidence: processor.evidence,
		Encoder: encoder, Profile: profile, Unknown: processor.unknown, IDs: identity.NewUUIDGenerator(),
		Clock: processor.clock, OnChanged: processor.onChanged,
	})
	return runner.Process(ctx, trackID, finalizing || meetingFinalized)
}

// speakerAutomation 返回精确绑定当前模型的正式 profile；没有正式档案时不提供 fallback。
func (module *VoiceModule) speakerAutomation() (port.VoiceEncoder, speakerdomain.MatchingProfile, error) {
	encoder, err := module.Encoder()
	if err != nil {
		return nil, speakerdomain.MatchingProfile{}, err
	}
	model := modelIdentity(encoder.ModelInfo())
	profile, err := speakerservice.LoadMatchingProfile(matchingProfilePath(), model)
	if err != nil {
		return nil, speakerdomain.MatchingProfile{}, err
	}
	return encoder, profile, nil
}

// speakerAutomationStatus 把动态模型/profile 门禁转换为独立 UI 状态。
func (module *VoiceModule) speakerAutomationStatus() (string, string) {
	_, profile, err := module.speakerAutomation()
	if err == nil && profile.Continuity != nil {
		return string(speakerdomain.AutomationReady), ""
	}
	if err == nil {
		return string(speakerdomain.AutomationProfileMismatch), apperr.CodeSpeakerProfileMismatch.ErrorCode
	}
	var appError *apperr.AppError
	if errors.As(err, &appError) {
		switch appError.ErrorCode {
		case apperr.CodeSpeakerProfileMissing.ErrorCode:
			return string(speakerdomain.AutomationProfileMissing), appError.ErrorCode
		case apperr.CodeSpeakerProfileMismatch.ErrorCode:
			return string(speakerdomain.AutomationProfileMismatch), appError.ErrorCode
		}
	}
	return string(speakerdomain.AutomationModelUnavailable), apperr.CodeSpeakerModelUnavailable.ErrorCode
}

// matchingProfilePath 返回开发仓库或发布包内唯一正式校准档案路径。
func matchingProfilePath() string {
	if appbuild.Current().BuildMode == "development" {
		workingDirectory, _ := os.Getwd()
		for _, root := range []string{workingDirectory, filepath.Clean(filepath.Join(workingDirectory, "..", ".."))} {
			candidate := filepath.Join(root, "models", matchingProfileFilename)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
		return filepath.Join(workingDirectory, "models", matchingProfileFilename)
	}
	installRoot, err := filesystem.CurrentInstallRoot()
	if err != nil {
		return filepath.Join("models", matchingProfileFilename)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(installRoot.String(), "Contents", "Resources", "models", matchingProfileFilename)
	}
	return filepath.Join(installRoot.String(), "models", matchingProfileFilename)
}

// modelIdentity 将统一 VoiceEncoder 身份映射为 profile 四元组。
func modelIdentity(info port.ModelInfo) speakerdomain.ModelIdentity {
	return speakerdomain.ModelIdentity{ID: info.ID, Version: info.Version, SHA256: info.SHA256, Dimension: info.Dimension}
}

// writeRollingFrame 将本地已保存的小端 PCM16 副本同步写入固定容量 ring。
func writeRollingFrame(buffer *speakerservice.RollingBuffer, frame port.AudioFrame) error {
	if buffer == nil || len(frame.PCM)%2 != 0 {
		return fmt.Errorf("speaker rolling PCM 无效")
	}
	if frame.StartSample == 0 {
		buffer.Reset()
	}
	samples := make([]int16, len(frame.PCM)/2)
	for index := range samples {
		samples[index] = int16(binary.LittleEndian.Uint16(frame.PCM[index*2 : index*2+2]))
	}
	return buffer.Write(frame.StartSample, samples)
}
