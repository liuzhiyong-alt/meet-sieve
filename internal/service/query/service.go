// Package query 编排 Step 9 首页、会议记录、详情和长列表的只读投影。
package query

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	querydomain "meet-sieve/internal/domain/query"
	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
	queryrepository "meet-sieve/internal/repository/query"
)

// Repository 是查询服务消费的最小只读持久化边界。
type Repository interface {
	ListMeetings(context.Context, queryrepository.ListInput) (queryrepository.MeetingPage, error)
	FindHighestPriorityMeeting(context.Context) (*queryrepository.MeetingSummaryRow, error)
	GetMeeting(context.Context, string) (*queryrepository.MeetingSummaryRow, error)
	ListTranscript(context.Context, string, int64, int64, int) ([]queryrepository.TranscriptRow, queryrepository.SeqPageState, error)
	ListContent(context.Context, string, int64, int64, int) ([]queryrepository.ContentRow, queryrepository.SeqPageState, error)
	CountStatus(context.Context, querydomain.MeetingStatus) (int, error)
	GetRecoveryFacts(context.Context, string) (queryrepository.RecoveryFactsRow, error)
}

// Service 负责校验输入、签发游标并构造安全 read model。
type Service struct {
	repository Repository
}

// NewService 创建只读查询服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// ListMeetingsInput 是 Wails 会议记录查询的服务入参。
type ListMeetingsInput struct {
	Search string
	Status string
	Cursor string
	Limit  int
}

// MeetingPage 是会议记录页面的安全游标投影。
type MeetingPage struct {
	Items          []MeetingSummary
	NextCursor     string
	PreviousCursor string
}

// MeetingSummary 是 Repository 会议事实与唯一主动作组成的服务投影。
type MeetingSummary struct {
	queryrepository.MeetingSummaryRow
	PrimaryAction        PrimaryAction
	CanDeleteMeeting     bool
	DeleteDisabledReason string
}

// Home 是首页快捷开始、继续处理和最近会议投影。
type Home struct {
	Continuation   *MeetingSummary
	Remaining      int
	RecentMeetings []MeetingSummary
}

// PrimaryAction 是会议最高状态对应的唯一稳定业务动作。
type PrimaryAction struct {
	Kind           string
	Label          string
	TargetID       string
	Enabled        bool
	DisabledReason string
}

// PrimaryActionFor 把会议最高状态投影为不包含前端路由的唯一主动作。
func PrimaryActionFor(meeting queryrepository.MeetingSummaryRow) PrimaryAction {
	meetingAction := func(kind string, label string) PrimaryAction {
		return PrimaryAction{Kind: kind, Label: label, TargetID: meeting.ID, Enabled: true}
	}
	switch meeting.HighestStatus {
	case querydomain.StatusDeleting:
		return meetingAction("deletion_recovery", "查看进度")
	case querydomain.StatusRecoveryRequired:
		return meetingAction("recover_meeting", "恢复")
	case querydomain.StatusGapConflict:
		return gapPrimaryAction("resolve_gap", "处理缺口", meeting.PendingGapID)
	case querydomain.StatusGapPending:
		return gapPrimaryAction("open_gap", "打开", meeting.PendingGapID)
	case querydomain.StatusMinuteCandidate:
		return meetingAction("open_meeting", "打开")
	case querydomain.StatusAgentUnsynced, querydomain.StatusMinuteConfirmed, querydomain.StatusSaved:
		return meetingAction("open_meeting", "打开")
	default:
		return PrimaryAction{Kind: "open_meeting", Label: "打开", TargetID: meeting.ID, DisabledReason: "会议状态暂不可处理"}
	}
}

// gapPrimaryAction 构造必须指向真实缺口的动作，缺少目标时明确禁用。
func gapPrimaryAction(kind string, label string, gapID string) PrimaryAction {
	if strings.TrimSpace(gapID) == "" {
		return PrimaryAction{Kind: kind, Label: label, DisabledReason: "未找到待处理的转写缺口"}
	}
	return PrimaryAction{Kind: kind, Label: label, TargetID: gapID, Enabled: true}
}

// MeetingDetail 是不含文件路径的会议详情摘要和动作能力。
type MeetingDetail struct {
	Summary          MeetingSummary
	CanPlayAudio     bool
	CanRetranscribe  bool
	CanDeleteMeeting bool
	DisabledReason   string
}

// InterruptedRecovery 是恢复页的真实只读事实和安全能力。
type InterruptedRecovery struct {
	Meeting        MeetingSummary
	Facts          queryrepository.RecoveryFactsRow
	CanRetry       bool
	DisabledReason string
}

// SeqPageInput 是原始记录和内容页签的 seq 边界。
type SeqPageInput struct {
	MeetingID string
	AfterSeq  int64
	BeforeSeq int64
	Limit     int
}

// TranscriptPage 是原始记录的有界页。
type TranscriptPage struct {
	Items       []queryrepository.TranscriptRow
	HasMore     bool
	HasPrevious bool
	HasNext     bool
	AfterSeq    int64
	BeforeSeq   int64
}

// ContentItem 是去除原始 URL 后的消息或资源投影。
type ContentItem struct {
	queryrepository.ContentRow
	Hostname   string
	DisplayURL string
}

// ContentPage 是消息、资源与公开 AI 回答的有界页。
type ContentPage struct {
	Items       []ContentItem
	HasMore     bool
	HasPrevious bool
	HasNext     bool
	AfterSeq    int64
	BeforeSeq   int64
}

// ListMeetings 校验筛选和不透明游标并签发前后页边界。
func (service *Service) ListMeetings(ctx context.Context, input ListMeetingsInput) (MeetingPage, error) {
	if service == nil || service.repository == nil {
		return MeetingPage{}, apperr.Sys(fmt.Errorf("查询服务不可用"), apperr.WithOp("query.list.dependencies"))
	}
	filter, err := querydomain.NormalizeFilter(querydomain.MeetingFilter{Search: input.Search, Status: input.Status})
	if err != nil {
		return MeetingPage{}, apperr.Biz(apperr.CodeQueryCursorInvalid, apperr.WithOp("query.list.filter"))
	}
	var cursor *querydomain.Cursor
	if strings.TrimSpace(input.Cursor) != "" {
		decoded, decodeErr := querydomain.DecodeCursor(input.Cursor, filter)
		if decodeErr != nil {
			return MeetingPage{}, mapCursorError(decodeErr)
		}
		cursor = &decoded
	}
	page, err := service.repository.ListMeetings(ctx, queryrepository.ListInput{Filter: filter, Cursor: cursor, Limit: input.Limit})
	if err != nil {
		return MeetingPage{}, fmt.Errorf("读取会议记录：%w", err)
	}
	return buildMeetingPage(page, cursor, filter)
}

// GetHome 返回一个最高优先级继续项和少量最近会议，不启动任何外部动作。
func (service *Service) GetHome(ctx context.Context) (Home, error) {
	page, err := service.repository.ListMeetings(ctx, queryrepository.ListInput{Limit: 5})
	if err != nil {
		return Home{}, fmt.Errorf("读取首页：%w", err)
	}
	home := Home{RecentMeetings: projectMeetingSummaries(page.Items)}
	continuation, err := service.repository.FindHighestPriorityMeeting(ctx)
	if err != nil {
		return Home{}, fmt.Errorf("读取首页继续项：%w", err)
	}
	if continuation != nil {
		projected := projectMeetingSummary(*continuation)
		home.Continuation = &projected
	}
	if continuation != nil {
		count, countErr := service.repository.CountStatus(ctx, continuation.HighestStatus)
		if countErr != nil {
			return Home{}, fmt.Errorf("统计首页继续项：%w", countErr)
		}
		if count > 0 {
			home.Remaining = count - 1
		}
	}
	return home, nil
}

// GetMeetingDetail 返回全部状态轴和统一音频/删除能力。
func (service *Service) GetMeetingDetail(ctx context.Context, meetingID string) (MeetingDetail, error) {
	if strings.TrimSpace(meetingID) == "" {
		return MeetingDetail{}, apperr.Biz(apperr.CodeMeetingNotFound)
	}
	row, err := service.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return MeetingDetail{}, fmt.Errorf("读取会议详情：%w", err)
	}
	if row == nil {
		return MeetingDetail{}, apperr.Biz(apperr.CodeMeetingNotFound)
	}
	locked := row.HighestStatus == querydomain.StatusDeleting
	audioAvailable := row.LocalSaveState == "saved" && row.HasReadyAudio && !row.RecordingDeleted && !locked
	detail := MeetingDetail{
		Summary: projectMeetingSummary(*row), CanPlayAudio: audioAvailable, CanRetranscribe: audioAvailable,
		CanDeleteMeeting: !locked && row.LifecycleState != "recording",
	}
	if locked {
		detail.DisabledReason = apperr.CodeMeetingMaintenanceLocked.Message
	}
	return detail, nil
}

// GetInterruptedRecovery 只允许 interrupted 且尚未保存成功的会议进入恢复页。
func (service *Service) GetInterruptedRecovery(ctx context.Context, meetingID string) (InterruptedRecovery, error) {
	detail, err := service.GetMeetingDetail(ctx, meetingID)
	if err != nil {
		return InterruptedRecovery{}, err
	}
	if detail.Summary.LifecycleState != "interrupted" || detail.Summary.LocalSaveState == "saved" {
		return InterruptedRecovery{}, apperr.Biz(apperr.CodeRecoveryNotAllowed)
	}
	facts, err := service.repository.GetRecoveryFacts(ctx, meetingID)
	if err != nil {
		return InterruptedRecovery{}, err
	}
	canRetry := !detail.Summary.RecordingDeleted && facts.SegmentCount > 0
	reason := ""
	if !canRetry {
		reason = "本地录音分片不可用，无法重试恢复"
	}
	return InterruptedRecovery{Meeting: detail.Summary, Facts: facts, CanRetry: canRetry, DisabledReason: reason}, nil
}

// ListTranscript 读取最多 200 条原始记录，并返回可写入 URL 的 seq 边界。
func (service *Service) ListTranscript(ctx context.Context, input SeqPageInput) (TranscriptPage, error) {
	rows, state, err := service.repository.ListTranscript(ctx, input.MeetingID, input.AfterSeq, input.BeforeSeq, input.Limit)
	if err != nil {
		return TranscriptPage{}, err
	}
	for index := range rows {
		rows[index].SpeakerDisplay = transcriptSpeakerDisplay(rows[index])
	}
	page := TranscriptPage{
		Items: rows, HasMore: state.HasNext,
		HasPrevious: state.HasPrevious, HasNext: state.HasNext,
	}
	setTranscriptBoundaries(&page)
	return page, nil
}

// ListContent 读取最多 100 条内容，并只返回脱敏链接展示信息。
func (service *Service) ListContent(ctx context.Context, input SeqPageInput) (ContentPage, error) {
	rows, state, err := service.repository.ListContent(ctx, input.MeetingID, input.AfterSeq, input.BeforeSeq, input.Limit)
	if err != nil {
		return ContentPage{}, err
	}
	items := make([]ContentItem, 0, len(rows))
	for _, row := range rows {
		item := ContentItem{ContentRow: row}
		item.SourceURL = ""
		if row.ResourceKind == "link" {
			item.Hostname, item.DisplayURL = formatLink(row.SourceURL)
		}
		items = append(items, item)
	}
	page := ContentPage{
		Items: items, HasMore: state.HasNext,
		HasPrevious: state.HasPrevious, HasNext: state.HasNext,
	}
	if len(items) > 0 {
		page.BeforeSeq = items[0].Seq
		page.AfterSeq = items[len(items)-1].Seq
	}
	return page, nil
}

// transcriptSpeakerDisplay 按成员、单场未知聚类和未识别状态生成用户可见名称。
func transcriptSpeakerDisplay(row queryrepository.TranscriptRow) string {
	if row.Kind != "utterance.final" {
		return ""
	}
	return speakerdomain.DisplayName(row.SpeakerName, row.ClusterDisplayNo, row.TrackDisplayNo)
}

// buildMeetingPage 按当前页首尾项签发前后游标。
func buildMeetingPage(page queryrepository.MeetingPage, current *querydomain.Cursor, filter querydomain.MeetingFilter) (MeetingPage, error) {
	result := MeetingPage{Items: projectMeetingSummaries(page.Items)}
	if len(page.Items) == 0 {
		return result, nil
	}
	if current != nil {
		previous := querydomain.Cursor{Version: 1, Direction: querydomain.DirectionPrevious, StartedAt: page.Items[0].StartedAt, MeetingNo: page.Items[0].MeetingNo}
		encoded, err := querydomain.EncodeCursor(previous, filter)
		if err != nil {
			return MeetingPage{}, err
		}
		result.PreviousCursor = encoded
	}
	if page.HasMore {
		last := page.Items[len(page.Items)-1]
		next := querydomain.Cursor{Version: 1, Direction: querydomain.DirectionNext, StartedAt: last.StartedAt, MeetingNo: last.MeetingNo}
		encoded, err := querydomain.EncodeCursor(next, filter)
		if err != nil {
			return MeetingPage{}, err
		}
		result.NextCursor = encoded
	}
	return result, nil
}

// projectMeetingSummaries 为一页会议集中生成唯一主动作。
func projectMeetingSummaries(rows []queryrepository.MeetingSummaryRow) []MeetingSummary {
	items := make([]MeetingSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, projectMeetingSummary(row))
	}
	return items
}

// projectMeetingSummary 组合单场会议事实与业务动作。
func projectMeetingSummary(row queryrepository.MeetingSummaryRow) MeetingSummary {
	canDelete := row.HighestStatus != querydomain.StatusDeleting && row.LifecycleState != "recording"
	disabledReason := ""
	if row.HighestStatus == querydomain.StatusDeleting {
		disabledReason = apperr.CodeMeetingMaintenanceLocked.Message
	} else if row.LifecycleState == "recording" {
		disabledReason = "会议进行中，结束会议后才能删除"
	}
	return MeetingSummary{
		MeetingSummaryRow:    row,
		PrimaryAction:        PrimaryActionFor(row),
		CanDeleteMeeting:     canDelete,
		DeleteDisabledReason: disabledReason,
	}
}

// mapCursorError 把领域游标原因映射为前端稳定错误码。
func mapCursorError(err error) error {
	if errors.Is(err, querydomain.ErrCursorFilterChanged) {
		return apperr.Biz(apperr.CodeQueryCursorFilterChanged, apperr.WithOp("query.cursor.filter"))
	}
	return apperr.Biz(apperr.CodeQueryCursorInvalid, apperr.WithOp("query.cursor.decode"))
}

// setTranscriptBoundaries 从当前页生成 after/before seq。
func setTranscriptBoundaries(page *TranscriptPage) {
	if page == nil || len(page.Items) == 0 {
		return
	}
	page.BeforeSeq = page.Items[0].Seq
	page.AfterSeq = page.Items[len(page.Items)-1].Seq
}

// formatLink 只返回完整 hostname 和不含用户信息、query、fragment 的展示 URL。
func formatLink(raw string) (string, string) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return "", ""
	}
	display := parsed.Scheme + "://" + parsed.Hostname()
	if parsed.Port() != "" {
		display += ":" + parsed.Port()
	}
	return parsed.Hostname(), display
}
