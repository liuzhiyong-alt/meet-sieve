package main

import (
	"context"
	"embed"
	"time"

	application "meet-sieve/internal/app"
	appbootstrap "meet-sieve/internal/app/bootstrap"
	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/infra/singleinstance"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	meetingservice "meet-sieve/internal/service/meeting"
	peopleservice "meet-sieve/internal/service/people"
	transcriptservice "meet-sieve/internal/service/transcript"
	wailstransport "meet-sieve/internal/transport/wails"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.uber.org/zap"
)

const expectedONNXVersion = "1.26.0"

//go:embed all:frontend/dist
var assets embed.FS

// main 启动无控制台依赖的 MeetSieve 桌面应用。
func main() {
	_ = run()
}

// run 装配基础设施、Wails 生命周期和系统 binding。
func run() error {
	outcome, lease, err := singleinstance.Acquire()
	if err != nil {
		return err
	}
	if outcome == singleinstance.OutcomeAlreadyRunning {
		return nil
	}
	defer func() {
		_ = lease.Close()
	}()

	bootstrap := application.NewBootstrap(expectedONNXVersion)
	defer func() {
		_ = bootstrap.Runtime.Stop(context.Background())
		_ = bootstrap.Logger.SyncAndClose()
	}()

	desktopApp := NewApp(
		bootstrap.Runtime,
		bootstrap.Logger,
		bootstrap.Health,
		bootstrap.Dependencies,
	)
	workspaceModule, workspaceErr := application.NewWorkspaceModule(bootstrap.Config, buildinfo.Current().Version)
	if workspaceErr != nil {
		appErr := apperr.Sys(workspaceErr, apperr.WithOp("app.workspace_module"))
		bootstrap.Health.Set(health.Snapshot{Status: health.StatusFailed, ErrorCode: appErr.Code, Message: appErr.Message})
		workspaceModule = &application.WorkspaceModule{Coordinator: appbootstrap.NewCoordinator(appbootstrap.Dependencies{})}
	}
	desktopApp.AttachWorkspaceCoordinator(workspaceModule.Coordinator)
	var wailsContext context.Context
	meetingModule := application.NewMeetingModule(
		workspaceModule.Coordinator,
		bootstrap.Dependencies.AudioEnumerator,
		bootstrap.Config.Recording,
		bootstrap.Config.ASR,
	)
	_ = meetingModule.SetTranscriptPublishers(
		func(event port.TranscriptionEvent) {
			if wailsContext == nil {
				return
			}
			runtime.EventsEmit(wailsContext, "meeting.asr.partial", wailstransport.NewEvent(
				"meeting.asr.partial", time.Now(), 0, wailstransport.ASRPartialEventDTO{
					MeetingID: event.MeetingID, ResultID: event.ResultID, Revision: event.Revision,
					Text: event.Text, StartSample: event.StartSample, EndSample: event.EndSample,
				},
			))
		},
		func(meetingID string, state string, errorCode string) {
			if wailsContext == nil {
				return
			}
			runtime.EventsEmit(wailsContext, "meeting.asr.changed", wailstransport.NewEvent(
				"meeting.asr.changed", time.Now(), 0,
				wailstransport.ASRStateEventDTO{MeetingID: meetingID, State: state, ErrorCode: errorCode},
			))
		},
	)
	_ = meetingModule.SetSpeakerPublisher(func(meetingID string, trackID string) {
		if wailsContext == nil {
			return
		}
		runtime.EventsEmit(wailsContext, "meeting.speaker.changed", wailstransport.NewEvent(
			"meeting.speaker.changed", time.Now(), 0,
			wailstransport.SpeakerChangedEventDTO{MeetingID: meetingID, TrackID: trackID},
		))
	})
	desktopApp.AttachMeetingModule(meetingModule)
	voiceModule, voiceModuleErr := application.NewVoiceModule()
	if voiceModuleErr != nil {
		bootstrap.Logger.LogError(
			"声纹模块装配失败，其他功能继续可用",
			"voice-module",
			apperr.Dependency(apperr.CodeVoiceModelUnavailable, voiceModuleErr, apperr.WithOp("app.voice_module")),
			zap.String("component", "voice"),
		)
	} else {
		desktopApp.AttachVoiceModule(voiceModule)
	}
	_ = meetingModule.SetVoiceModule(voiceModule)
	boundary := wailstransport.NewBoundary(bootstrap.Logger)
	systemBinding := wailstransport.NewSystemBinding(
		bootstrap.Health,
		boundary,
	)
	bootstrapBinding := wailstransport.NewBootstrapBinding(workspaceModule.Coordinator, boundary)
	workspaceBinding := wailstransport.NewWorkspaceBinding(workspaceModule.Coordinator, boundary)
	voiceWorkspaceServices := application.NewVoiceWorkspaceServices(workspaceModule.Coordinator, voiceModule)
	correctionWorkspaceServices := application.NewCorrectionWorkspaceServices(workspaceModule.Coordinator, voiceWorkspaceServices)
	correctionProvider := func() (wailstransport.CorrectionServices, error) {
		bundle, providerErr := correctionWorkspaceServices.Current()
		if providerErr != nil {
			return wailstransport.CorrectionServices{}, providerErr
		}
		return wailstransport.CorrectionServices{
			Query: bundle.Query, Commands: bundle.Commands, Clips: bundle.Clips, MeetingClip: bundle.MeetingClip,
			RetryRawRecord: bundle.RetryRawRecord, RetrySpeaker: bundle.RetrySpeaker,
			SpeakerStatus: func(ctx context.Context, meetingID string) wailstransport.SpeakerStatusDTO {
				state, errorCode := bundle.SpeakerStatus(ctx, meetingID)
				return wailstransport.SpeakerStatusDTO{MeetingID: meetingID, State: state, ErrorCode: errorCode}
			},
		}, nil
	}
	peopleBinding := wailstransport.NewPeopleBinding(func() (*peopleservice.MemberService, *peopleservice.GroupService, error) {
		reader, transactions, accessErr := workspaceModule.Coordinator.BusinessDatabase()
		if accessErr != nil {
			return nil, nil, accessErr
		}
		members := peoplerepository.NewMemberRepository(reader)
		currentClock := clock.NewSystem()
		return peopleservice.NewMemberService(peopleservice.MemberServiceDependencies{
				Repository: members, Transactions: transactions, IDs: identity.NewUUIDGenerator(), Clock: currentClock,
				VoiceModel: func() (port.ModelInfo, error) {
					encoder, encoderErr := voiceModule.Encoder()
					if encoderErr != nil {
						return port.ModelInfo{}, encoderErr
					}
					return encoder.ModelInfo(), nil
				},
				DeleteVoiceSamples: func(ctx context.Context, memberID string) error {
					voiceService, _, serviceErr := voiceWorkspaceServices.Current()
					if serviceErr != nil {
						return serviceErr
					}
					return voiceService.DeleteAllSamples(ctx, memberID)
				},
			}), peopleservice.NewGroupService(peopleservice.GroupServiceDependencies{
				Repository: peoplerepository.NewGroupRepository(reader), Members: members, Transactions: transactions,
				IDs: identity.NewUUIDGenerator(), Clock: currentClock,
			}), nil
	}, boundary)
	directoryDialogBinding := wailstransport.NewDirectoryDialogBinding(func() context.Context { return wailsContext }, boundary)
	voiceModelBinding := wailstransport.NewVoiceModelBinding(voiceModule, func() context.Context { return wailsContext }, boundary)
	voiceBinding := wailstransport.NewVoiceBinding(
		voiceWorkspaceServices.Current,
		bootstrap.Dependencies.AudioEnumerator,
		func() context.Context { return wailsContext },
		boundary,
	)
	meetingBinding := wailstransport.NewMeetingBinding(
		func() (*meetingservice.Service, *meetingservice.RuntimeService, *meetingservice.RecoveryService, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return nil, nil, nil, serviceErr
			}
			return services.Meetings, services.Runtime, services.Recovery, nil
		},
		func() context.Context { return wailsContext },
		func(ctx context.Context, name string, data any) { runtime.EventsEmit(ctx, name, data) },
		boundary,
	)
	asrBinding := wailstransport.NewASRBinding(
		func() (*transcriptservice.SettingsService, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return nil, serviceErr
			}
			return services.TranscriptSettings, nil
		},
		func() (*transcriptservice.TimelineService, *transcriptservice.MeetingRuntime, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return nil, nil, serviceErr
			}
			return services.TranscriptTimeline, services.TranscriptRuntime, nil
		},
		func() context.Context { return wailsContext }, boundary,
	)
	correctionBinding := wailstransport.NewCorrectionBinding(
		correctionProvider,
		func() context.Context { return wailsContext },
		func(ctx context.Context, name string, data any) { runtime.EventsEmit(ctx, name, data) },
		boundary,
	)
	voiceModelBinding.SetAfterActivate(voiceWorkspaceServices.RecoverAndRebuild)

	err = wails.Run(&options.App{
		Title:     "MeetSieve",
		Width:     1280,
		Height:    800,
		MinWidth:  1024,
		MinHeight: 720,
		AssetServer: &assetserver.Options{
			Assets: assets, Handler: wailstransport.NewAudioClipAssetHandler(correctionProvider),
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup: func(ctx context.Context) {
			wailsContext = ctx
			desktopApp.startup(ctx)
			lease.SetActivationHandler(func() {
				desktopApp.activateWindow(ctx)
			})
		},
		OnShutdown:    desktopApp.shutdown,
		OnBeforeClose: desktopApp.beforeClose,
		Bind: []interface{}{
			systemBinding,
			bootstrapBinding,
			workspaceBinding,
			peopleBinding,
			directoryDialogBinding,
			voiceModelBinding,
			voiceBinding,
			meetingBinding,
			asrBinding,
			correctionBinding,
		},
	})
	if err != nil {
		appErr := apperr.Sys(err, apperr.WithOp("wails.run"))
		bootstrap.Logger.LogError(
			"Wails 运行失败",
			uuid.NewString(),
			appErr,
			zap.String("component", "wails.runtime"),
		)
	}
	return err
}
