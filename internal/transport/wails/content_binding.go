package wails

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"time"

	"meet-sieve/internal/infra/identity"
	contentservice "meet-sieve/internal/service/content"
	lanservice "meet-sieve/internal/service/lan"
	meetingservice "meet-sieve/internal/service/meeting"
	resourceservice "meet-sieve/internal/service/resource"
	guesthttp "meet-sieve/internal/transport/http/guest"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ContentServices 是会中统一时间线 Binding 需要的最小服务集合。
type ContentServices struct {
	Content     *contentservice.Service
	Attachments *resourceservice.AttachmentService
	Meetings    *meetingservice.Service
	Runtime     *meetingservice.RuntimeService
	LAN         *lanservice.Manager
	Presence    *guesthttp.Presence
}

// ContentServiceProvider 延迟返回当前工作目录对应的会中内容服务。
type ContentServiceProvider func() (ContentServices, error)

// TimelineQueryDTO 是桌面时间线的固定游标查询契约。
type TimelineQueryDTO struct {
	MeetingID string `json:"meeting_id"`
	Direction string `json:"direction"`
	CursorSeq int64  `json:"cursor_seq"`
	Limit     int    `json:"limit"`
}

// TimelinePageDTO 是页面刷新和通知丢失后可恢复的持久事件页。
type TimelinePageDTO struct {
	Entries      []TimelineEntryDTO `json:"entries"`
	OldestSeq    int64              `json:"oldest_seq"`
	LatestSeq    int64              `json:"latest_seq"`
	HasOlder     bool               `json:"has_older"`
	HasMoreAfter bool               `json:"has_more_after"`
}

// TimelineChangedEventDTO 是持久事件提交后的轻量失效通知，正文通过游标接口恢复。
type TimelineChangedEventDTO struct {
	MeetingID string `json:"meeting_id"`
	LatestSeq int64  `json:"latest_seq"`
	Reason    string `json:"reason"`
}

// TimelineEntryDTO 是通过 kind 判别的统一会中事件。
type TimelineEntryDTO struct {
	Seq           int64  `json:"seq"`
	Kind          string `json:"kind"`
	OccurredAt    int64  `json:"occurred_at"`
	Source        string `json:"source"`
	EntityID      string `json:"entity_id,omitempty"`
	DisplayName   string `json:"display_name,omitempty"`
	Text          string `json:"text,omitempty"`
	ContentFormat string `json:"content_format,omitempty"`
	SpeakerKey    string `json:"speaker_key,omitempty"`
	SpeakerLabel  string `json:"speaker_label,omitempty"`
	StartSample   int64  `json:"start_sample,omitempty"`
	EndSample     int64  `json:"end_sample,omitempty"`
	State         string `json:"state,omitempty"`
	Reason        string `json:"reason,omitempty"`
	ResourceKind  string `json:"resource_kind,omitempty"`
	OriginalName  string `json:"original_name,omitempty"`
	MediaType     string `json:"media_type,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	URL           string `json:"url,omitempty"`
	Description   string `json:"description,omitempty"`
}

// SendMeetingMessageDTO 是主持人发送 Markdown 消息的幂等输入。
type SendMeetingMessageDTO struct {
	MeetingID string `json:"meeting_id"`
	RequestID string `json:"request_id"`
	Content   string `json:"content"`
}

// ChooseAttachmentDTO 只允许前端指定会议，文件路径始终来自系统窗口。
type ChooseAttachmentDTO struct {
	MeetingID string `json:"meeting_id"`
}

// AttachmentSendDTO 返回系统窗口取消或真实附件持久结果。
type AttachmentSendDTO struct {
	Cancelled    bool   `json:"cancelled"`
	RequestID    string `json:"request_id,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Seq          int64  `json:"seq,omitempty"`
	OccurredAt   int64  `json:"occurred_at,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	MediaType    string `json:"media_type,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
}

// AttachmentUploadEventDTO 是 Timeline 临时附件行使用的安全进程内状态。
type AttachmentUploadEventDTO struct {
	MeetingID string `json:"meeting_id"`
	RequestID string `json:"request_id"`
	State     string `json:"state"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	ErrorCode string `json:"error_code,omitempty"`
}

// LiveMeetingStatusDTO 聚合会中右侧状态卡的真实状态轴。
type LiveMeetingStatusDTO struct {
	StartedAt        *int64 `json:"started_at,omitempty"`
	EndedAt          *int64 `json:"ended_at,omitempty"`
	RecordingState   string `json:"recording_state"`
	MicrophoneState  string `json:"microphone_state"`
	LocalSaveState   string `json:"local_save_state"`
	RealtimeASRState string `json:"realtime_asr_state"`
	LatestFinalAt    int64  `json:"latest_final_at,omitempty"`
	AgentState       string `json:"agent_state"`
	LANState         string `json:"lan_state"`
	OnlineCount      int    `json:"online_count"`
}

// ContentBinding 暴露会中统一时间线、主持人消息和系统附件选择。
type ContentBinding struct {
	services        ContentServiceProvider
	contextProvider ContextProvider
	emit            MeetingEventEmitter
	ids             identity.Generator
	boundary        *Boundary
}

// NewContentBinding 创建会中内容 Binding；构造阶段不打开文件或数据库。
func NewContentBinding(services ContentServiceProvider, contextProvider ContextProvider, emit MeetingEventEmitter, ids identity.Generator, boundary *Boundary) *ContentBinding {
	return &ContentBinding{services: services, contextProvider: contextProvider, emit: emit, ids: ids, boundary: boundary}
}

// GetMeetingTimeline 按 seq 游标返回统一持久事件页。
func (binding *ContentBinding) GetMeetingTimeline(query TimelineQueryDTO) Result[TimelinePageDTO] {
	return Invoke(binding.boundary, "wails.content.timeline", func(_ string) (TimelinePageDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return TimelinePageDTO{}, err
		}
		page, err := services.Content.ListTimeline(ctx, contentservice.TimelineQuery{
			MeetingID: query.MeetingID, Direction: query.Direction, CursorSeq: query.CursorSeq, Limit: query.Limit,
		})
		return mapTimelinePageDTO(page), err
	})
}

// SendMeetingMessage 提交主持人 Markdown 消息并返回真实 seq。
func (binding *ContentBinding) SendMeetingMessage(input SendMeetingMessageDTO) Result[TimelineEntryDTO] {
	return Invoke(binding.boundary, "wails.content.message.send", func(_ string) (TimelineEntryDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return TimelineEntryDTO{}, err
		}
		result, err := services.Content.SendHostMessage(ctx, contentservice.SendMessageInput{
			MeetingID: input.MeetingID, RequestID: input.RequestID, Content: input.Content,
		})
		if err != nil {
			return TimelineEntryDTO{}, err
		}
		return TimelineEntryDTO{
			Seq: result.Seq, Kind: "message", OccurredAt: result.OccurredAt, Source: "host",
			EntityID: result.EntityID, DisplayName: "你", Text: input.Content, ContentFormat: "markdown",
		}, nil
	})
}

// ChooseAndSendMeetingAttachment 打开系统文件窗口，确认选择后立即使用安全流式链路发送。
func (binding *ContentBinding) ChooseAndSendMeetingAttachment(input ChooseAttachmentDTO) Result[AttachmentSendDTO] {
	return Invoke(binding.boundary, "wails.content.attachment.choose_send", func(_ string) (AttachmentSendDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return AttachmentSendDTO{}, err
		}
		path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{Title: "选择要发送到会议的附件"})
		if err != nil {
			return AttachmentSendDTO{}, err
		}
		if path == "" {
			return AttachmentSendDTO{Cancelled: true}, nil
		}
		return binding.sendSelectedAttachment(ctx, services, input.MeetingID, path)
	})
}

// GetLiveMeetingStatus 返回会中状态卡需要的真实聚合状态。
func (binding *ContentBinding) GetLiveMeetingStatus(meetingID string) Result[LiveMeetingStatusDTO] {
	return Invoke(binding.boundary, "wails.content.live_status", func(_ string) (LiveMeetingStatusDTO, error) {
		services, ctx, err := binding.current()
		if err != nil {
			return LiveMeetingStatusDTO{}, err
		}
		meeting, err := services.Meetings.GetMeeting(ctx, meetingID)
		if err != nil {
			return LiveMeetingStatusDTO{}, fmt.Errorf("会议状态不可用")
		}
		latestFinalAt, err := services.Content.LatestFinalOccurredAt(ctx, meetingID)
		if err != nil {
			return LiveMeetingStatusDTO{}, err
		}
		status := LiveMeetingStatusDTO{
			StartedAt: meeting.StartedAt, EndedAt: meeting.EndedAt, RecordingState: meeting.LifecycleState,
			MicrophoneState: services.Runtime.MicrophoneState(), LocalSaveState: meeting.LocalSaveState,
			RealtimeASRState: meeting.RealtimeASRState, LatestFinalAt: latestFinalAt, AgentState: meeting.AgentState,
		}
		if services.LAN != nil {
			status.LANState = string(services.LAN.Snapshot().State)
		}
		if services.Presence != nil {
			status.OnlineCount = services.Presence.Count(time.Now())
		}
		return status, nil
	})
}

// sendSelectedAttachment 只读取系统窗口返回的路径，前端永远拿不到完整本地路径。
func (binding *ContentBinding) sendSelectedAttachment(ctx context.Context, services ContentServices, meetingID string, path string) (AttachmentSendDTO, error) {
	if binding.ids == nil {
		return AttachmentSendDTO{}, fmt.Errorf("附件 request ID 生成器不可用")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return AttachmentSendDTO{}, fmt.Errorf("选择的附件不是可读普通文件")
	}
	file, err := os.Open(path)
	if err != nil {
		return AttachmentSendDTO{}, fmt.Errorf("打开所选附件失败：%w", err)
	}
	defer file.Close()
	requestID := binding.ids.New()
	name := filepath.Base(path)
	binding.emitAttachmentState(ctx, meetingID, requestID, name, info.Size(), "uploading", "")
	result, err := services.Attachments.UploadHost(ctx, meetingID, resourceservice.AttachmentInput{
		RequestID: requestID, OriginalName: name, DeclaredSize: info.Size(),
		DeclaredMediaType: mime.TypeByExtension(filepath.Ext(name)), Reader: file,
	})
	if err != nil {
		binding.emitAttachmentState(ctx, meetingID, requestID, name, info.Size(), "failed", "ATTACHMENT_SEND_FAILED")
		return AttachmentSendDTO{}, err
	}
	binding.emitAttachmentState(ctx, meetingID, requestID, name, info.Size(), "completed", "")
	return AttachmentSendDTO{
		RequestID: requestID, ResourceID: result.ResourceID, Seq: result.Seq, OccurredAt: result.OccurredAt,
		OriginalName: result.OriginalName, MediaType: result.MediaType, SizeBytes: result.SizeBytes, SHA256: result.SHA256,
	}, nil
}

// emitAttachmentState 发布不含本地路径的临时附件状态。
func (binding *ContentBinding) emitAttachmentState(ctx context.Context, meetingID string, requestID string, name string, size int64, state string, errorCode string) {
	if binding.emit == nil {
		return
	}
	binding.emit(ctx, "meeting.attachment.upload.changed", NewEvent(
		"meeting.attachment.upload.changed", time.Now(), 0,
		AttachmentUploadEventDTO{MeetingID: meetingID, RequestID: requestID, State: state, Name: name, SizeBytes: size, ErrorCode: errorCode},
	))
}

// current 返回当前工作目录服务和 Wails 生命周期 context。
func (binding *ContentBinding) current() (ContentServices, context.Context, error) {
	if binding == nil || binding.services == nil || binding.contextProvider == nil || binding.contextProvider() == nil {
		return ContentServices{}, nil, fmt.Errorf("会中内容服务尚未准备")
	}
	services, err := binding.services()
	if err != nil {
		return ContentServices{}, nil, err
	}
	if services.Content == nil || services.Attachments == nil || services.Meetings == nil || services.Runtime == nil {
		return ContentServices{}, nil, fmt.Errorf("会中内容服务依赖不完整")
	}
	return services, binding.contextProvider(), nil
}

// mapTimelinePageDTO 转换统一时间线页。
func mapTimelinePageDTO(page contentservice.TimelinePage) TimelinePageDTO {
	entries := make([]TimelineEntryDTO, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, mapTimelineEntryDTO(entry))
	}
	return TimelinePageDTO{
		Entries: entries, OldestSeq: page.OldestSeq, LatestSeq: page.LatestSeq,
		HasOlder: page.HasOlder, HasMoreAfter: page.HasMoreAfter,
	}
}

// mapTimelineEntryDTO 把 Service 投影限制为前端展示白名单。
func mapTimelineEntryDTO(entry contentservice.TimelineEntry) TimelineEntryDTO {
	return TimelineEntryDTO{
		Seq: entry.Seq, Kind: entry.Kind, OccurredAt: entry.OccurredAt, Source: entry.Source,
		EntityID: entry.EntityID, DisplayName: entry.DisplayName, Text: entry.Text,
		ContentFormat: entry.ContentFormat, SpeakerKey: entry.SpeakerKey, SpeakerLabel: entry.SpeakerLabel,
		StartSample: entry.StartSample, EndSample: entry.EndSample, State: entry.State, Reason: entry.Reason,
		ResourceKind: entry.ResourceKind, OriginalName: entry.OriginalName, MediaType: entry.MediaType,
		SizeBytes: entry.SizeBytes, SHA256: entry.SHA256, URL: entry.URL, Description: entry.Description,
	}
}
