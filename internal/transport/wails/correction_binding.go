package wails

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	correctionservice "meet-sieve/internal/service/correction"
	gapservice "meet-sieve/internal/service/gap"
	speakerservice "meet-sieve/internal/service/speaker"
	voiceservice "meet-sieve/internal/service/voice"
)

// CorrectionServices 是当前工作目录校对查询、命令、clip 与永久声纹服务集合。
type CorrectionServices struct {
	Query          *correctionservice.QueryService
	Commands       *correctionservice.Service
	Clips          *speakerservice.AudioClipService
	MeetingClip    *correctionservice.MeetingClipService
	RetryRawRecord func(ctx context.Context, meetingID string) error
	RetrySpeaker   func(ctx context.Context, meetingID string) error
	SpeakerStatus  func(ctx context.Context, meetingID string) SpeakerStatusDTO
}

// SpeakerStatusDTO 独立表达自动说话人处理门禁，不影响人工校对可用性。
type SpeakerStatusDTO struct {
	MeetingID string `json:"meeting_id"`
	State     string `json:"state"`
	ErrorCode string `json:"error_code,omitempty"`
}

// SpeakerChangedEventDTO 是自动 track 提交后不含姓名、文本和向量的刷新事实。
type SpeakerChangedEventDTO struct {
	MeetingID string `json:"meeting_id"`
	TrackID   string `json:"track_id"`
}

// AudioClipAssetHandler 把 Wails AssetServer 的 clip 路径延迟路由到当前工作目录 token store。
type AudioClipAssetHandler struct {
	services CorrectionServiceProvider
	gap      GapClipServiceProvider
}

// GapClipServiceProvider 延迟返回当前工作目录的 gap 回放服务。
type GapClipServiceProvider func() (*gapservice.AudioClipService, error)

// NewAudioClipAssetHandler 创建动态 AssetServer handler。
func NewAudioClipAssetHandler(services CorrectionServiceProvider, gap ...GapClipServiceProvider) *AudioClipAssetHandler {
	var gapProvider GapClipServiceProvider
	if len(gap) > 0 {
		gapProvider = gap[0]
	}
	return &AudioClipAssetHandler{services: services, gap: gapProvider}
}

// ServeHTTP 只允许 clip 前缀；工作目录不可用时返回 404，不暴露底层原因。
func (handler *AudioClipAssetHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if handler == nil {
		http.NotFound(response, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/media/gap-clips/") {
		if handler.gap == nil {
			http.NotFound(response, request)
			return
		}
		service, err := handler.gap()
		if err != nil || service == nil {
			http.NotFound(response, request)
			return
		}
		service.ServeHTTP(response, request)
		return
	}
	if handler.services == nil || !strings.HasPrefix(request.URL.Path, "/media/audio-clips/") {
		http.NotFound(response, request)
		return
	}
	services, err := handler.services()
	if err != nil || services.Clips == nil {
		http.NotFound(response, request)
		return
	}
	services.Clips.ServeHTTP(response, request)
}

// CorrectionServiceProvider 延迟返回当前工作目录服务。
type CorrectionServiceProvider func() (CorrectionServices, error)

// CorrectionEventPublisher 发布只含 ID/revision/status 的轻量刷新事件。
type CorrectionEventPublisher func(ctx context.Context, name string, data any)

// CorrectionEntryDTO 是校对页分页条目，不包含 embedding、路径、token hash 或完整 event payload。
type CorrectionEntryDTO struct {
	Seq                      int64  `json:"seq"`
	UtteranceID              string `json:"utterance_id"`
	StartSample              int64  `json:"start_sample"`
	EndSample                int64  `json:"end_sample"`
	OriginalText             string `json:"original_text"`
	CurrentText              string `json:"current_text"`
	SpeakerDisplay           string `json:"speaker_display"`
	CurrentParticipantID     string `json:"current_participant_id,omitempty"`
	SpeakerClusterID         string `json:"speaker_cluster_id,omitempty"`
	ClusterDisplayNo         int    `json:"cluster_display_no,omitempty"`
	ClusterParticipantID     string `json:"cluster_participant_id,omitempty"`
	AssignmentSource         string `json:"assignment_source"`
	TextRevision             int    `json:"text_revision"`
	SpeakerRevision          int    `json:"speaker_revision"`
	ClusterRevision          int    `json:"cluster_revision,omitempty"`
	ClusterCount             int    `json:"cluster_count,omitempty"`
	CanPlay                  bool   `json:"can_play"`
	PlaybackDisabledReason   string `json:"playback_disabled_reason,omitempty"`
	CanEnroll                bool   `json:"can_enroll"`
	EnrollmentDisabledReason string `json:"enrollment_disabled_reason,omitempty"`
}

// CorrectionParticipantDTO 是本场人工说话人选择项。
type CorrectionParticipantDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind"`
	IsMember    bool   `json:"is_member"`
}

// CorrectionPageDTO 是刷新/重启可从 SQLite 恢复的分页结果。
type CorrectionPageDTO struct {
	Entries      []CorrectionEntryDTO       `json:"entries"`
	Participants []CorrectionParticipantDTO `json:"participants"`
	NextSeq      int64                      `json:"next_seq"`
}

// CorrectionCommandDTO 是单条文本/说话人命令共享字段。
type CorrectionCommandDTO struct {
	RequestID        string `json:"request_id"`
	MeetingID        string `json:"meeting_id"`
	UtteranceID      string `json:"utterance_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Value            string `json:"value"`
	Reason           string `json:"reason,omitempty"`
}

// ClusterCorrectionDTO 绑定确认时 revision 与影响条数。
type ClusterCorrectionDTO struct {
	RequestID        string `json:"request_id"`
	MeetingID        string `json:"meeting_id"`
	ClusterID        string `json:"cluster_id"`
	ParticipantID    string `json:"participant_id"`
	ExpectedRevision int    `json:"expected_revision"`
	ExpectedCount    int    `json:"expected_count"`
	Reason           string `json:"reason,omitempty"`
}

// ResourceCorrectionDTO 是 resource 当前说明命令。
type ResourceCorrectionDTO struct {
	RequestID        string `json:"request_id"`
	MeetingID        string `json:"meeting_id"`
	ResourceID       string `json:"resource_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Description      string `json:"description"`
	Reason           string `json:"reason,omitempty"`
}

// CorrectionResultDTO 明确 SQLite 保存、影响条数和 Markdown 投影状态。
type CorrectionResultDTO struct {
	CorrectionID        string `json:"correction_id,omitempty"`
	ResultRevision      int    `json:"result_revision"`
	Saved               bool   `json:"saved"`
	Duplicate           bool   `json:"duplicate"`
	NoOp                bool   `json:"no_op"`
	ImpactedCount       int    `json:"impacted_count"`
	ProjectionState     string `json:"projection_state"`
	ProjectionErrorCode string `json:"projection_error_code,omitempty"`
}

// AudioClipDTO 只暴露短期 URL 与实际采样范围。
type AudioClipDTO struct {
	URL         string `json:"url"`
	StartSample int64  `json:"start_sample"`
	EndSample   int64  `json:"end_sample"`
	ExpiresAt   int64  `json:"expires_at"`
}

// MeetingClipDTO 是独立二次确认的永久声纹命令。
type MeetingClipDTO struct {
	RequestID       string `json:"request_id"`
	MeetingID       string `json:"meeting_id"`
	UtteranceID     string `json:"utterance_id"`
	EnvironmentKind string `json:"environment_kind"`
	Confirmed       bool   `json:"confirmed"`
}

// VoiceSampleChangedDTO 是会议片段录入后的最小状态结果。
type VoiceSampleChangedDTO struct {
	SampleID        string `json:"sample_id"`
	MemberID        string `json:"member_id"`
	ProcessingState string `json:"processing_state"`
	QualityState    string `json:"quality_state"`
	QualityCode     string `json:"quality_code,omitempty"`
}

// CorrectionBinding 暴露 Step 5 校对、clip、声纹加入和恢复命令。
type CorrectionBinding struct {
	services        CorrectionServiceProvider
	contextProvider ContextProvider
	publish         CorrectionEventPublisher
	boundary        *Boundary
}

// NewCorrectionBinding 创建不在构造阶段访问数据库的 binding。
func NewCorrectionBinding(services CorrectionServiceProvider, contextProvider ContextProvider, publish CorrectionEventPublisher, boundary *Boundary) *CorrectionBinding {
	return &CorrectionBinding{services: services, contextProvider: contextProvider, publish: publish, boundary: boundary}
}

// ListCorrectionEntries 按 seq 游标分页恢复校对页。
func (binding *CorrectionBinding) ListCorrectionEntries(meetingID string, afterSeq int64, limit int) Result[CorrectionPageDTO] {
	return Invoke(binding.boundary, "wails.correction.list", func(string) (CorrectionPageDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return CorrectionPageDTO{}, err
		}
		page, err := services.Query.ListEntries(ctx, meetingID, afterSeq, limit)
		return mapCorrectionPage(page), err
	})
}

// GetCorrectionEntry 按 utterance ID 恢复单条最新 revision。
func (binding *CorrectionBinding) GetCorrectionEntry(utteranceID string) Result[CorrectionEntryDTO] {
	return Invoke(binding.boundary, "wails.correction.get", func(string) (CorrectionEntryDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return CorrectionEntryDTO{}, err
		}
		entry, err := services.Query.GetEntry(ctx, utteranceID)
		return mapCorrectionEntry(entry), err
	})
}

// GetSpeakerStatus 返回自动识别门禁；profile 缺失不会阻塞人工校对。
func (binding *CorrectionBinding) GetSpeakerStatus(meetingID string) Result[SpeakerStatusDTO] {
	return Invoke(binding.boundary, "wails.correction.speaker.status", func(string) (SpeakerStatusDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return SpeakerStatusDTO{}, err
		}
		if services.SpeakerStatus == nil {
			return SpeakerStatusDTO{MeetingID: meetingID, State: "unavailable"}, nil
		}
		return services.SpeakerStatus(ctx, meetingID), nil
	})
}

// CorrectUtteranceText 提交单条文本校对。
func (binding *CorrectionBinding) CorrectUtteranceText(input CorrectionCommandDTO) Result[CorrectionResultDTO] {
	return binding.invokeCorrection("wails.correction.text", input.MeetingID, input.UtteranceID, func(ctx context.Context, services CorrectionServices, operatorID string) (correctionservice.Result, error) {
		return services.Commands.CorrectText(ctx, correctionservice.TextCommand{CommandBase: correctionBase(input, operatorID), Text: input.Value})
	})
}

// CorrectUtteranceSpeaker 提交单条说话人校对，Value 是本场 participant ID。
func (binding *CorrectionBinding) CorrectUtteranceSpeaker(input CorrectionCommandDTO) Result[CorrectionResultDTO] {
	return binding.invokeCorrection("wails.correction.speaker", input.MeetingID, input.UtteranceID, func(ctx context.Context, services CorrectionServices, operatorID string) (correctionservice.Result, error) {
		return services.Commands.CorrectSpeaker(ctx, correctionservice.SpeakerCommand{CommandBase: correctionBase(input, operatorID), ParticipantID: input.Value})
	})
}

// CorrectSpeakerCluster 提交确认后的 cluster 批量校对。
func (binding *CorrectionBinding) CorrectSpeakerCluster(input ClusterCorrectionDTO) Result[CorrectionResultDTO] {
	return binding.invokeCorrection("wails.correction.cluster", input.MeetingID, input.ClusterID, func(ctx context.Context, services CorrectionServices, operatorID string) (correctionservice.Result, error) {
		base := correctionservice.CommandBase{RequestID: input.RequestID, MeetingID: input.MeetingID, TargetID: input.ClusterID, ExpectedRevision: input.ExpectedRevision, OperatorID: operatorID, Reason: input.Reason}
		return services.Commands.CorrectCluster(ctx, correctionservice.ClusterCommand{CommandBase: base, ParticipantID: input.ParticipantID, ExpectedCount: input.ExpectedCount})
	})
}

// CorrectResourceDescription 提交 completed resource 当前说明校对。
func (binding *CorrectionBinding) CorrectResourceDescription(input ResourceCorrectionDTO) Result[CorrectionResultDTO] {
	return binding.invokeCorrection("wails.correction.resource", input.MeetingID, input.ResourceID, func(ctx context.Context, services CorrectionServices, operatorID string) (correctionservice.Result, error) {
		base := correctionservice.CommandBase{RequestID: input.RequestID, MeetingID: input.MeetingID, TargetID: input.ResourceID, ExpectedRevision: input.ExpectedRevision, OperatorID: operatorID, Reason: input.Reason}
		return services.Commands.CorrectResource(ctx, correctionservice.ResourceCommand{CommandBase: base, Description: input.Description})
	})
}

// CreateUtteranceAudioClip 创建短期回放 URL。
func (binding *CorrectionBinding) CreateUtteranceAudioClip(utteranceID string) Result[AudioClipDTO] {
	return Invoke(binding.boundary, "wails.correction.clip.create", func(string) (AudioClipDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AudioClipDTO{}, err
		}
		clip, err := services.Clips.Create(ctx, utteranceID)
		return AudioClipDTO{URL: clip.URL, StartSample: clip.StartSample, EndSample: clip.EndSample, ExpiresAt: clip.ExpiresAt}, err
	})
}

// RevokeAudioClip 撤销页面持有的短期 token。
func (binding *CorrectionBinding) RevokeAudioClip(tokenOrURL string) Result[bool] {
	return Invoke(binding.boundary, "wails.correction.clip.revoke", func(string) (bool, error) {
		services, _, err := binding.current()
		if err != nil {
			return false, err
		}
		token := strings.TrimPrefix(tokenOrURL, "/media/audio-clips/")
		if err := services.Clips.Revoke(token); err != nil {
			return false, err
		}
		return true, nil
	})
}

// AddUtteranceToVoiceSamples 独立二次确认后加入永久声纹。
func (binding *CorrectionBinding) AddUtteranceToVoiceSamples(input MeetingClipDTO) Result[VoiceSampleChangedDTO] {
	return Invoke(binding.boundary, "wails.correction.voice.add", func(string) (VoiceSampleChangedDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return VoiceSampleChangedDTO{}, err
		}
		sample, err := services.MeetingClip.Add(ctx, correctionservice.MeetingClipCommand{RequestID: input.RequestID, MeetingID: input.MeetingID, UtteranceID: input.UtteranceID, EnvironmentKind: input.EnvironmentKind, Confirmed: input.Confirmed})
		if err != nil {
			return VoiceSampleChangedDTO{}, err
		}
		result := VoiceSampleChangedDTO{SampleID: sample.ID, MemberID: sample.MemberID, ProcessingState: sample.ProcessingState, QualityState: sample.QualityState, QualityCode: sample.QualityCode}
		binding.emit(ctx, "voice.sample.changed", map[string]any{"sample_id": result.SampleID, "member_id": result.MemberID, "status": result.ProcessingState})
		return result, nil
	})
}

// RetryRawRecordProjection 同步重建当前 SQLite 快照。
func (binding *CorrectionBinding) RetryRawRecordProjection(meetingID string) Result[bool] {
	return binding.invokeRetry("wails.correction.raw_record.retry", meetingID, func(ctx context.Context, services CorrectionServices) error {
		if err := services.RetryRawRecord(ctx, meetingID); err != nil {
			return err
		}
		binding.emit(ctx, "meeting.raw-record.changed", map[string]any{"meeting_id": meetingID, "status": "ready"})
		return nil
	})
}

// RetrySpeakerProcessing 触发 SQLite 可恢复 speaker track 补拉。
func (binding *CorrectionBinding) RetrySpeakerProcessing(meetingID string) Result[bool] {
	return binding.invokeRetry("wails.correction.speaker.retry", meetingID, func(ctx context.Context, services CorrectionServices) error {
		if err := services.RetrySpeaker(ctx, meetingID); err != nil {
			return err
		}
		binding.emit(ctx, "meeting.speaker.changed", map[string]any{"meeting_id": meetingID, "status": "retried"})
		return nil
	})
}

type correctionAction func(context.Context, CorrectionServices, string) (correctionservice.Result, error)

// invokeCorrection 统一映射结果并发布不含正文的刷新事件。
func (binding *CorrectionBinding) invokeCorrection(operation string, meetingID string, targetID string, action correctionAction) Result[CorrectionResultDTO] {
	return Invoke(binding.boundary, operation, func(operatorID string) (CorrectionResultDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return CorrectionResultDTO{}, err
		}
		result, err := action(ctx, services, operatorID)
		if err != nil {
			return CorrectionResultDTO{}, err
		}
		binding.emit(ctx, "meeting.correction.changed", map[string]any{"meeting_id": meetingID, "target_id": targetID, "revision": result.ResultRevision, "status": result.ProjectionState})
		return mapCorrectionResult(result), nil
	})
}

// invokeRetry 统一执行恢复命令。
func (binding *CorrectionBinding) invokeRetry(operation string, meetingID string, action func(context.Context, CorrectionServices) error) Result[bool] {
	return Invoke(binding.boundary, operation, func(string) (bool, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return false, err
		}
		if err := action(ctx, services); err != nil {
			return false, err
		}
		return true, nil
	})
}

// current 返回完整服务和有效 Wails 生命周期 context。
func (binding *CorrectionBinding) current() (CorrectionServices, context.Context, error) {
	if binding == nil || binding.services == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
		return CorrectionServices{}, nil, fmt.Errorf("校对服务尚未准备")
	}
	services, err := binding.services()
	if err != nil {
		return CorrectionServices{}, nil, fmt.Errorf("校对服务不可用：%w", err)
	}
	if services.Query == nil || services.Commands == nil || services.Clips == nil || services.MeetingClip == nil {
		return CorrectionServices{}, nil, fmt.Errorf("校对服务不可用")
	}
	return services, binding.contextProvider(), nil
}

// emit 发布轻量事件。
func (binding *CorrectionBinding) emit(ctx context.Context, name string, data any) {
	if binding.publish != nil {
		binding.publish(ctx, name, data)
	}
}

// correctionBase 映射单条命令公共字段。
func correctionBase(input CorrectionCommandDTO, operatorID string) correctionservice.CommandBase {
	return correctionservice.CommandBase{RequestID: input.RequestID, MeetingID: input.MeetingID, TargetID: input.UtteranceID, ExpectedRevision: input.ExpectedRevision, OperatorID: operatorID, Reason: input.Reason}
}

// mapCorrectionResult 转换部分成功语义。
func mapCorrectionResult(value correctionservice.Result) CorrectionResultDTO {
	return CorrectionResultDTO{CorrectionID: value.CorrectionID, ResultRevision: value.ResultRevision, Saved: value.Saved, Duplicate: value.Duplicate, NoOp: value.NoOp, ImpactedCount: value.ImpactedCount, ProjectionState: value.ProjectionState, ProjectionErrorCode: value.ProjectionErrorCode}
}

// mapCorrectionPage 转换分页快照。
func mapCorrectionPage(value correctionservice.Page) CorrectionPageDTO {
	result := CorrectionPageDTO{Entries: make([]CorrectionEntryDTO, 0, len(value.Entries)), Participants: make([]CorrectionParticipantDTO, 0, len(value.Participants)), NextSeq: value.NextSeq}
	for _, entry := range value.Entries {
		result.Entries = append(result.Entries, mapCorrectionEntry(entry))
	}
	for _, participant := range value.Participants {
		result.Participants = append(result.Participants, CorrectionParticipantDTO{ID: participant.ID, DisplayName: participant.DisplayName, Kind: participant.Kind, IsMember: participant.IsMember})
	}
	return result
}

// mapCorrectionEntry 转换单条安全投影。
func mapCorrectionEntry(entry correctionservice.Entry) CorrectionEntryDTO {
	return CorrectionEntryDTO{Seq: entry.Seq, UtteranceID: entry.UtteranceID, StartSample: entry.StartSample, EndSample: entry.EndSample, OriginalText: entry.OriginalText, CurrentText: entry.CurrentText, SpeakerDisplay: entry.SpeakerDisplay, CurrentParticipantID: entry.CurrentParticipantID, SpeakerClusterID: entry.SpeakerClusterID, ClusterDisplayNo: entry.ClusterDisplayNo, ClusterParticipantID: entry.ClusterParticipantID, AssignmentSource: entry.AssignmentSource, TextRevision: entry.TextRevision, SpeakerRevision: entry.SpeakerRevision, ClusterRevision: entry.ClusterRevision, ClusterCount: entry.ClusterCount, CanPlay: entry.CanPlay, PlaybackDisabledReason: entry.PlaybackDisabledReason, CanEnroll: entry.CanEnroll, EnrollmentDisabledReason: entry.EnrollmentDisabledReason}
}

var _ correctionservice.MeetingVoiceEnrollment = (*voiceservice.VoiceEnrollmentService)(nil)
