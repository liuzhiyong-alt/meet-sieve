package main

import (
	"context"
	"embed"
	"io/fs"
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
	agentservice "meet-sieve/internal/service/agent"
	audioservice "meet-sieve/internal/service/audio"
	deletionservice "meet-sieve/internal/service/deletion"
	diagnosticsservice "meet-sieve/internal/service/diagnostics"
	gapservice "meet-sieve/internal/service/gap"
	lanservice "meet-sieve/internal/service/lan"
	meetingservice "meet-sieve/internal/service/meeting"
	minutesservice "meet-sieve/internal/service/minutes"
	peopleservice "meet-sieve/internal/service/people"
	queryservice "meet-sieve/internal/service/query"
	resourceservice "meet-sieve/internal/service/resource"
	resourceopenservice "meet-sieve/internal/service/resourceopen"
	transcriptservice "meet-sieve/internal/service/transcript"
	guesthttp "meet-sieve/internal/transport/http/guest"
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
		bootstrap.Logger,
		bootstrap.Health,
	)
	workspaceModule.Coordinator.SetWorkspaceChangeBlocker(func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return meetingModule.HasUnsafeWorkspaceChange(ctx)
	})
	if guestAssets, guestAssetsErr := fs.Sub(assets, "frontend/dist/guest"); guestAssetsErr != nil {
		bootstrap.Logger.Component("guest_assets").Warn("访客页面资源未嵌入，局域网访客页不可用", zap.Error(guestAssetsErr))
	} else if err := meetingModule.SetGuestAssets(guestAssets); err != nil {
		bootstrap.Logger.Component("guest_assets").Warn("访客页面资源校验失败，局域网访客页不可用", zap.Error(err))
	}
	_ = meetingModule.SetTranscriptPublishers(
		func(event port.TranscriptionEvent) {
			if wailsContext == nil {
				return
			}
			runtime.EventsEmit(wailsContext, "meeting.asr.partial", wailstransport.NewEvent(
				"meeting.asr.partial", time.Now(), 0, wailstransport.ASRPartialEventDTO{
					MeetingID: event.MeetingID, SessionID: event.SessionID, Generation: event.Generation,
					ResultID: event.ResultID, Revision: event.Revision,
					Text: event.Text, StartSample: event.StartSample, EndSample: event.EndSample,
				},
			))
		},
		func(event transcriptservice.PartialClearEvent) {
			if wailsContext == nil {
				return
			}
			runtime.EventsEmit(wailsContext, "meeting.asr.partial.cleared", wailstransport.NewEvent(
				"meeting.asr.partial.cleared", time.Now(), 0, wailstransport.ASRPartialClearEventDTO{
					MeetingID: event.MeetingID, SessionID: event.SessionID,
					Generation: event.Generation, ResultID: event.ResultID,
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
	_ = meetingModule.SetTimelinePublisher(func(meetingID string, latestSeq int64, reason string) {
		if wailsContext == nil {
			return
		}
		const name = "meeting.timeline.changed"
		runtime.EventsEmit(wailsContext, name, wailstransport.NewEvent(
			name, time.Now(), uint64(latestSeq),
			wailstransport.TimelineChangedEventDTO{MeetingID: meetingID, LatestSeq: latestSeq, Reason: reason},
		))
	})
	_ = meetingModule.SetAgentPublishers(
		func(event port.AgentEvent) {
			if event.Type == port.AgentEventTimelineChanged {
				services, serviceErr := meetingModule.Current()
				if serviceErr == nil && services.Content != nil {
					latestSeq, latestErr := services.Content.LatestEventSeq(context.Background(), event.MeetingID)
					if latestErr == nil {
						if services.GuestHub != nil {
							services.GuestHub.Publish(event.MeetingID, latestSeq, "agent_persisted")
						}
						if wailsContext != nil {
							const timelineName = "meeting.timeline.changed"
							runtime.EventsEmit(wailsContext, timelineName, wailstransport.NewEvent(
								timelineName, time.Now(), uint64(latestSeq),
								wailstransport.TimelineChangedEventDTO{MeetingID: event.MeetingID, LatestSeq: latestSeq, Reason: "agent_persisted"},
							))
						}
					}
				}
				if wailsContext == nil {
					return
				}
				name := "meeting.agent.timeline.changed"
				runtime.EventsEmit(wailsContext, name, wailstransport.NewEvent(name, time.Now(), 0, wailstransport.MapAgentEventDTO(event)))
				return
			}
			if wailsContext == nil {
				return
			}
			name := "meeting.agent.changed"
			if event.Type == port.AgentEventAnswerDelta {
				name = "meeting.agent.delta"
			} else if event.Type == port.AgentEventApprovalRequested {
				name = "meeting.agent.approval.requested"
			}
			runtime.EventsEmit(wailsContext, name, wailstransport.NewEvent(name, time.Now(), 0, wailstransport.MapAgentEventDTO(event)))
		},
		func(state agentservice.WakeWordTestState) {
			if wailsContext != nil {
				runtime.EventsEmit(wailsContext, "settings.wake_word_test.changed", wailstransport.NewEvent(
					"settings.wake_word_test.changed", time.Now(), 0, wailstransport.MapWakeWordTestStateDTO(state),
				))
			}
		},
		func(state agentservice.WakeCommandState) {
			if wailsContext != nil {
				const name = "meeting.agent.wake.changed"
				runtime.EventsEmit(wailsContext, name, wailstransport.NewEvent(
					name, time.Now(), state.Revision, wailstransport.MapWakeCommandStateDTO(state),
				))
			}
		},
	)
	_ = meetingModule.SetStep8Publishers(
		meetingservice.FinalizationEventSinkFunc(func(state meetingservice.FinalizationSnapshot) {
			emitStep8Event(wailsContext, "meeting.finalization.changed", wailstransport.Step8ChangedEventDTO{
				MeetingID: state.MeetingID, State: state.State, Stage: string(state.Stage), ErrorCode: state.ErrorCode,
				Revision: state.Revision, Retryable: state.State == "failed",
			})
		}),
		gapservice.EventSinkFunc(func(meetingID string) {
			if wailsContext == nil {
				return
			}
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return
			}
			state, stateErr := services.GapRepository.ReadState(context.Background(), meetingID)
			if stateErr != nil {
				return
			}
			completed := 0
			var revision uint64
			for _, item := range state.Gaps {
				if item.State == "completed" {
					completed++
				}
				if item.UpdatedAt > int64(revision) {
					revision = uint64(item.UpdatedAt)
				}
			}
			emitStep8Event(wailsContext, "meeting.gap.changed", wailstransport.Step8ChangedEventDTO{
				MeetingID: meetingID, State: state.Aggregate, Revision: revision,
				Completed: completed, Total: len(state.Gaps), Retryable: state.Aggregate == "failed" || state.Aggregate == "conflict",
			})
		}),
		minutesservice.GenerationEventSinkFunc(func(meetingID string, state minutesservice.GenerationState) {
			emitStep8Event(wailsContext, "meeting.minutes.changed", wailstransport.Step8ChangedEventDTO{
				MeetingID: meetingID, State: state.State, ErrorCode: state.ErrorCode,
				Revision: state.Revision, Retryable: state.State == "failed" || state.State == "timed_out" || state.State == "cancelled",
			})
		}),
		agentservice.FinalSyncEventSinkFunc(func(state agentservice.FinalSyncState) {
			emitStep8Event(wailsContext, "meeting.agent.final_sync.changed", wailstransport.Step8ChangedEventDTO{
				MeetingID: state.MeetingID, State: state.State, ErrorCode: state.ErrorCode,
				Revision: state.Revision, Retryable: state.State == "unsynced",
			})
		}),
	)
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
	voiceModelBinding := wailstransport.NewVoiceModelBinding(voiceModule, func() context.Context { return wailsContext }, boundary, func(ctx context.Context) (int, error) {
		services, serviceErr := meetingModule.Current()
		if serviceErr != nil {
			return 0, serviceErr
		}
		settings, settingsErr := services.AgentSettings.Get(ctx)
		return settings.ProxyPort, settingsErr
	})
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
	contentBinding := wailstransport.NewContentBinding(
		func() (wailstransport.ContentServices, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return wailstransport.ContentServices{}, serviceErr
			}
			return wailstransport.ContentServices{
				Content: services.Content, Attachments: services.Attachments, Meetings: services.Meetings,
				Runtime: services.Runtime, LAN: services.LAN, Presence: services.GuestPresence,
			}, nil
		},
		func() context.Context { return wailsContext },
		func(ctx context.Context, name string, data any) { runtime.EventsEmit(ctx, name, data) },
		identity.NewUUIDGenerator(),
		boundary,
	)
	lanBinding := wailstransport.NewLANBinding(
		func() (*lanservice.Manager, *guesthttp.Presence, *resourceservice.UploadCoordinator, *meetingservice.Service, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return nil, nil, nil, nil, serviceErr
			}
			return services.LAN, services.GuestPresence, services.GuestUploads, services.Meetings, nil
		},
		func() context.Context { return wailsContext },
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
	agentBinding := wailstransport.NewAgentBinding(
		func() (wailstransport.AgentServices, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return wailstransport.AgentServices{}, serviceErr
			}
			return wailstransport.AgentServices{
				Settings: services.AgentSettings, WakeTest: services.AgentWakeTest, Session: services.AgentOrchestrator,
				Turns: services.AgentTurns, Recovery: services.AgentRecoveryCommands,
			}, nil
		},
		func() context.Context { return wailsContext }, boundary,
	)
	finalizationBinding := wailstransport.NewFinalizationBinding(
		func() (wailstransport.FinalizationServices, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return wailstransport.FinalizationServices{}, serviceErr
			}
			return wailstransport.FinalizationServices{Runtime: services.Runtime, FinalSync: services.FinalSync}, nil
		},
		func() context.Context { return wailsContext }, boundary,
	)
	gapBinding := wailstransport.NewGapBinding(
		func() (wailstransport.GapServices, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return wailstransport.GapServices{}, serviceErr
			}
			return wailstransport.GapServices{Repository: services.GapRepository, Processor: services.PostMeeting, Conflicts: services.GapConflicts, Resolution: services.GapResolution, Clips: services.GapClips}, nil
		}, boundary,
	)
	minutesBinding := wailstransport.NewMinutesBinding(
		func() (wailstransport.MinutesServices, error) {
			services, serviceErr := meetingModule.Current()
			if serviceErr != nil {
				return wailstransport.MinutesServices{}, serviceErr
			}
			return wailstransport.MinutesServices{Repository: services.MinutesRepository, Generation: services.MinutesGeneration, Settings: services.MinutesSettings, Versions: services.MinutesVersions, Projector: services.MinutesProjector}, nil
		},
		func() context.Context { return wailsContext }, boundary,
	)
	queryBinding := wailstransport.NewQueryBinding(func() (*queryservice.Service, error) {
		services, serviceErr := meetingModule.Current()
		if serviceErr != nil {
			return nil, serviceErr
		}
		return services.Query, nil
	}, boundary)
	deletionBinding := wailstransport.NewDeletionBinding(func() (*deletionservice.Service, error) {
		services, serviceErr := meetingModule.Current()
		if serviceErr != nil {
			return nil, serviceErr
		}
		return services.Deletion, nil
	}, boundary)
	diagnosticBinding := wailstransport.NewDiagnosticBinding(func() (*diagnosticsservice.StorageScanService, *diagnosticsservice.ExportService, error) {
		services, serviceErr := meetingModule.Current()
		if serviceErr != nil {
			return nil, nil, serviceErr
		}
		return services.StorageScan, services.Diagnostics, nil
	}, func() context.Context { return wailsContext }, boundary)
	resourceBinding := wailstransport.NewResourceBinding(func() (*resourceopenservice.Service, error) {
		services, serviceErr := meetingModule.Current()
		if serviceErr != nil {
			return nil, serviceErr
		}
		return services.ResourceOpen, nil
	}, boundary)
	audioSettingsBinding := wailstransport.NewAudioSettingsBinding(func() (*audioservice.SettingsService, error) {
		services, serviceErr := meetingModule.Current()
		if serviceErr != nil {
			return nil, serviceErr
		}
		return services.AudioSettings, nil
	}, boundary)
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
			Assets: assets, Handler: wailstransport.NewAudioClipAssetHandler(correctionProvider, func() (*gapservice.AudioClipService, error) {
				services, serviceErr := meetingModule.Current()
				if serviceErr != nil {
					return nil, serviceErr
				}
				return services.GapClips, nil
			}),
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
			contentBinding,
			lanBinding,
			asrBinding,
			agentBinding,
			finalizationBinding,
			gapBinding,
			minutesBinding,
			queryBinding,
			deletionBinding,
			diagnosticBinding,
			resourceBinding,
			audioSettingsBinding,
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

// emitStep8Event 统一发布会后版本化 envelope；nil context 时安全忽略。
func emitStep8Event(ctx context.Context, name string, data wailstransport.Step8ChangedEventDTO) {
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, name, wailstransport.NewEvent(name, time.Now(), data.Revision, data))
}
