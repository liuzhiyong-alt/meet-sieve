package wails

import (
	"context"
	"fmt"

	"meet-sieve/internal/infra/apperr"
	queryservice "meet-sieve/internal/service/query"
)

// QueryServiceProvider 返回当前工作目录的只读查询服务。
type QueryServiceProvider func() (*queryservice.Service, error)

// QueryBinding 暴露首页、记录、详情和长列表的稳定只读契约。
type QueryBinding struct {
	service  QueryServiceProvider
	boundary *Boundary
}

// NewQueryBinding 创建查询 Binding，不在构造阶段访问数据库。
func NewQueryBinding(service QueryServiceProvider, boundary *Boundary) *QueryBinding {
	return &QueryBinding{service: service, boundary: boundary}
}

// GetHome 返回首页真实继续处理项和最近会议。
func (binding *QueryBinding) GetHome() Result[HomeDTO] {
	return Invoke(binding.boundary, "wails.query.home", func(string) (HomeDTO, error) {
		service, err := binding.currentService()
		if err != nil {
			return HomeDTO{}, err
		}
		home, err := service.GetHome(context.Background())
		if err != nil {
			return HomeDTO{}, err
		}
		dto := HomeDTO{Remaining: home.Remaining, RecentMeetings: make([]MeetingSummaryDTO, 0, len(home.RecentMeetings))}
		for _, meeting := range home.RecentMeetings {
			dto.RecentMeetings = append(dto.RecentMeetings, mapMeetingSummaryDTO(meeting))
		}
		if home.Continuation != nil {
			continuation := mapMeetingSummaryDTO(*home.Continuation)
			dto.Continuation = &continuation
		}
		return dto, nil
	})
}

// ListMeetings 返回最多 50 条且不含总数的游标页。
func (binding *QueryBinding) ListMeetings(input MeetingListInputDTO) Result[MeetingPageDTO] {
	return Invoke(binding.boundary, "wails.query.meetings", func(string) (MeetingPageDTO, error) {
		if input.Limit < 0 || input.Limit > 50 {
			return MeetingPageDTO{}, apperr.Biz(apperr.CodeQueryCursorInvalid)
		}
		service, err := binding.currentService()
		if err != nil {
			return MeetingPageDTO{}, err
		}
		page, err := service.ListMeetings(context.Background(), queryservice.ListMeetingsInput{
			Search: input.Search, Status: input.Status, Cursor: input.Cursor, Limit: input.Limit,
		})
		return mapMeetingPageDTO(page), err
	})
}

// GetMeetingDetail 返回会议全部状态轴和安全动作能力。
func (binding *QueryBinding) GetMeetingDetail(meetingID string) Result[MeetingDetailDTO] {
	return Invoke(binding.boundary, "wails.query.detail", func(string) (MeetingDetailDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return MeetingDetailDTO{}, err
		}
		service, err := binding.currentService()
		if err != nil {
			return MeetingDetailDTO{}, err
		}
		detail, err := service.GetMeetingDetail(context.Background(), meetingID)
		return mapMeetingDetailDTO(detail), err
	})
}

// ListTranscript 返回最多 200 条原始记录。
func (binding *QueryBinding) ListTranscript(input SeqPageInputDTO) Result[TranscriptPageDTO] {
	return Invoke(binding.boundary, "wails.query.transcript", func(string) (TranscriptPageDTO, error) {
		if err := validateSeqInput(input, 200); err != nil {
			return TranscriptPageDTO{}, err
		}
		service, err := binding.currentService()
		if err != nil {
			return TranscriptPageDTO{}, err
		}
		page, err := service.ListTranscript(context.Background(), mapSeqInput(input))
		return mapTranscriptPageDTO(page), err
	})
}

// ListMeetingContent 返回最多 100 条消息、资源和公开 AI 回答。
func (binding *QueryBinding) ListMeetingContent(input SeqPageInputDTO) Result[ContentPageDTO] {
	return Invoke(binding.boundary, "wails.query.content", func(string) (ContentPageDTO, error) {
		if err := validateSeqInput(input, 100); err != nil {
			return ContentPageDTO{}, err
		}
		service, err := binding.currentService()
		if err != nil {
			return ContentPageDTO{}, err
		}
		page, err := service.ListContent(context.Background(), mapSeqInput(input))
		return mapContentPageDTO(page), err
	})
}

// GetInterruptedRecovery 返回只允许修复本地保存的中断会议摘要。
func (binding *QueryBinding) GetInterruptedRecovery(meetingID string) Result[InterruptedRecoveryDTO] {
	return Invoke(binding.boundary, "wails.query.recovery", func(string) (InterruptedRecoveryDTO, error) {
		if err := requireUUID("meeting ID", meetingID); err != nil {
			return InterruptedRecoveryDTO{}, err
		}
		service, err := binding.currentService()
		if err != nil {
			return InterruptedRecoveryDTO{}, err
		}
		recovery, err := service.GetInterruptedRecovery(context.Background(), meetingID)
		if err != nil {
			return InterruptedRecoveryDTO{}, err
		}
		return InterruptedRecoveryDTO{
			Meeting: mapMeetingSummaryDTO(recovery.Meeting), CanRetry: recovery.CanRetry, DisabledReason: recovery.DisabledReason,
			SegmentCount: recovery.Facts.SegmentCount, DurationSamples: recovery.Facts.DurationSamples,
			SampleRate: recovery.Facts.SampleRate, FirstSequence: recovery.Facts.FirstSequence, LastSequence: recovery.Facts.LastSequence,
			GapCount: recovery.Facts.GapCount, PendingGapCount: recovery.Facts.PendingGapCount,
			ReadyFileCount: recovery.Facts.ReadyFileCount, FailedFileCount: recovery.Facts.FailedFileCount,
			DeletedFileCount: recovery.Facts.DeletedFileCount, FailureStage: recovery.Facts.FailureStage,
		}, nil
	})
}

// currentService 获取当前工作目录查询服务。
func (binding *QueryBinding) currentService() (*queryservice.Service, error) {
	if binding == nil || binding.service == nil {
		return nil, fmt.Errorf("查询 Binding 不可用")
	}
	return binding.service()
}

// validateSeqInput 校验 UUID、互斥边界和固定页上限。
func validateSeqInput(input SeqPageInputDTO, maximum int) error {
	if err := requireUUID("meeting ID", input.MeetingID); err != nil {
		return err
	}
	if input.AfterSeq < 0 || input.BeforeSeq < 0 || (input.AfterSeq > 0 && input.BeforeSeq > 0) || input.Limit < 0 || input.Limit > maximum {
		return apperr.Biz(apperr.CodeQueryCursorInvalid)
	}
	return nil
}

// mapSeqInput 转换长列表输入。
func mapSeqInput(input SeqPageInputDTO) queryservice.SeqPageInput {
	return queryservice.SeqPageInput{MeetingID: input.MeetingID, AfterSeq: input.AfterSeq, BeforeSeq: input.BeforeSeq, Limit: input.Limit}
}

// mapMeetingDetailDTO 转换详情能力。
func mapMeetingDetailDTO(detail queryservice.MeetingDetail) MeetingDetailDTO {
	return MeetingDetailDTO{
		Summary: mapMeetingSummaryDTO(detail.Summary), CanPlayAudio: detail.CanPlayAudio,
		CanRetranscribe: detail.CanRetranscribe, CanDeleteMeeting: detail.CanDeleteMeeting,
		DisabledReason: detail.DisabledReason,
	}
}

// mapTranscriptPageDTO 转换原始记录页。
func mapTranscriptPageDTO(page queryservice.TranscriptPage) TranscriptPageDTO {
	items := make([]TranscriptItemDTO, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, TranscriptItemDTO{
			Seq: item.Seq, Kind: item.Kind, OccurredAt: item.OccurredAt, Text: item.Text,
			SpeakerName: item.SpeakerName, SpeakerDisplay: item.SpeakerDisplay,
			StartSample: item.StartSample, EndSample: item.EndSample,
		})
	}
	return TranscriptPageDTO{
		Items: items, HasMore: page.HasMore, HasPrevious: page.HasPrevious, HasNext: page.HasNext,
		AfterSeq: page.AfterSeq, BeforeSeq: page.BeforeSeq,
	}
}

// mapContentPageDTO 转换安全内容页。
func mapContentPageDTO(page queryservice.ContentPage) ContentPageDTO {
	items := make([]ContentItemDTO, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, ContentItemDTO{
			Seq: item.Seq, Kind: item.Kind, OccurredAt: item.OccurredAt, EntityID: item.EntityID,
			DisplayName: item.DisplayName, Text: item.Text, ResourceKind: item.ResourceKind,
			ResourceName: item.ResourceName, ResourceState: item.ResourceState,
			Hostname: item.Hostname, DisplayURL: item.DisplayURL,
		})
	}
	return ContentPageDTO{
		Items: items, HasMore: page.HasMore, HasPrevious: page.HasPrevious, HasNext: page.HasNext,
		AfterSeq: page.AfterSeq, BeforeSeq: page.BeforeSeq,
	}
}
