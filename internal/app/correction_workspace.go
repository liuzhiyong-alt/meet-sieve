package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	appbootstrap "meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	correctionrepository "meet-sieve/internal/repository/correction"
	meetingrepository "meet-sieve/internal/repository/meeting"
	speakerrepository "meet-sieve/internal/repository/speaker"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	correctionservice "meet-sieve/internal/service/correction"
	speakerservice "meet-sieve/internal/service/speaker"
	transcriptservice "meet-sieve/internal/service/transcript"
)

// CorrectionWorkspaceBundle 是 transport 无关的当前工作目录 Step 5 服务集合。
type CorrectionWorkspaceBundle struct {
	Query          *correctionservice.QueryService
	Commands       *correctionservice.Service
	Clips          *speakerservice.AudioClipService
	MeetingClip    *correctionservice.MeetingClipService
	RetryRawRecord func(ctx context.Context, meetingID string) error
	RetrySpeaker   func(ctx context.Context, meetingID string) error
	SpeakerStatus  func(ctx context.Context, meetingID string) (state string, errorCode string)
}

// CorrectionWorkspaceServices 为当前数据库身份持有唯一 clip token store 和校对服务。
type CorrectionWorkspaceServices struct {
	workspace  *appbootstrap.Coordinator
	voice      *VoiceWorkspaceServices
	mu         sync.Mutex
	activePath string
	services   CorrectionWorkspaceBundle
}

// NewCorrectionWorkspaceServices 创建按 ready 工作目录延迟装配的 Step 5 服务。
func NewCorrectionWorkspaceServices(workspace *appbootstrap.Coordinator, voice *VoiceWorkspaceServices) *CorrectionWorkspaceServices {
	return &CorrectionWorkspaceServices{workspace: workspace, voice: voice}
}

// Current 返回当前工作目录稳定服务集合，切换工作目录时撤销旧内存 token store。
func (workspaceServices *CorrectionWorkspaceServices) Current() (CorrectionWorkspaceBundle, error) {
	if workspaceServices == nil || workspaceServices.workspace == nil || workspaceServices.voice == nil {
		return CorrectionWorkspaceBundle{}, fmt.Errorf("校对工作目录服务不可用")
	}
	settings := workspaceServices.workspace.GetWorkspaceSettings()
	if settings.ActivePath == "" {
		return CorrectionWorkspaceBundle{}, fmt.Errorf("工作目录尚未就绪")
	}
	workspaceServices.mu.Lock()
	defer workspaceServices.mu.Unlock()
	if workspaceServices.activePath == settings.ActivePath && workspaceServices.services.Query != nil {
		return workspaceServices.services, nil
	}
	reader, transactions, err := workspaceServices.workspace.BusinessDatabase()
	if err != nil {
		return CorrectionWorkspaceBundle{}, err
	}
	repository := correctionrepository.NewRepository(reader)
	meetingAudio, err := speakerservice.NewMeetingAudioReader(settings.ActivePath, meetingrepository.NewRepository(reader), nil, 120*16000)
	if err != nil {
		return CorrectionWorkspaceBundle{}, err
	}
	clipRoot := filepath.Join(os.TempDir(), "meet-sieve-audio-clips", workspaceClipKey(settings.ActivePath))
	speakerRepository := speakerrepository.NewRepository(reader)
	clips, err := speakerservice.NewAudioClipService(speakerservice.AudioClipDependencies{
		Repository: speakerRepository, Audio: meetingAudio, TempRoot: clipRoot, Clock: clock.NewSystem(),
	})
	if err != nil {
		return CorrectionWorkspaceBundle{}, err
	}
	voiceEnrollment, _, err := workspaceServices.voice.Current()
	if err != nil {
		return CorrectionWorkspaceBundle{}, err
	}
	rawRecord := transcriptservice.NewRawRecordProjector(transcriptservice.RawRecordProjectorDependencies{
		Repository: transcriptrepository.NewRepository(reader), WorkspaceRoot: settings.ActivePath,
	})
	commands := correctionservice.NewService(correctionservice.ServiceDependencies{
		Repository: repository, Transactions: transactions, IDs: identity.NewUUIDGenerator(), Clock: clock.NewSystem(), RawRecord: rawRecord,
	})
	meetingClip := correctionservice.NewMeetingClipService(correctionservice.MeetingClipDependencies{
		Repository: repository, Transactions: transactions, Audio: meetingAudio, Enrollment: voiceEnrollment,
	})
	currentClock := clock.NewSystem()
	unknown := speakerservice.NewUnknownAssigner(speakerservice.UnknownAssignerDependencies{
		Repository: speakerRepository, Transactions: transactions, IDs: identity.NewUUIDGenerator(), Clock: currentClock,
	})
	processor := &lazySpeakerProcessor{
		voice: workspaceServices.voice.module, repository: speakerRepository, transactions: transactions,
		evidence: speakerservice.NewEvidenceBuilder(meetingAudio), unknown: unknown, clock: currentClock,
		onChanged: func(meetingID string, _ string) { _ = rawRecord.MarkDirty(meetingID) },
	}
	services := CorrectionWorkspaceBundle{
		Query: correctionservice.NewQueryService(repository), Commands: commands, Clips: clips, MeetingClip: meetingClip,
		RetryRawRecord: rawRecord.Flush,
		RetrySpeaker: func(ctx context.Context, meetingID string) error {
			trackIDs, retryErr := speakerRepository.ListRecoverableMeetingTrackIDs(ctx, meetingID, 256)
			if retryErr != nil {
				return retryErr
			}
			for _, trackID := range trackIDs {
				if retryErr = processor.Process(ctx, trackID, true); retryErr != nil {
					return retryErr
				}
			}
			return nil
		},
		SpeakerStatus: func(context.Context, string) (string, string) {
			if workspaceServices.voice.module == nil {
				return "model_unavailable", apperr.CodeSpeakerModelUnavailable.ErrorCode
			}
			return workspaceServices.voice.module.speakerAutomationStatus()
		},
	}
	workspaceServices.activePath, workspaceServices.services = settings.ActivePath, services
	return services, nil
}

// workspaceClipKey 生成不泄漏绝对路径的稳定临时子目录名。
func workspaceClipKey(activePath string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(activePath)))
	return fmt.Sprintf("%x", digest[:16])
}
