package transcript

import (
	"context"
	"errors"
	"fmt"
	"strings"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// EventServiceDependencies 描述统一事件持久化的明确依赖。
type EventServiceDependencies struct {
	Repository   *transcriptrepository.Repository
	Transactions *database.TransactionManager
	IDs          identity.Generator
	Clock        clock.Clock
	// OnPersisted 在事务提交后通知文件投影；回调不得执行阻塞文件 I/O。
	OnPersisted func(meetingID string, event PersistedEvent)
}

// FinalInput 是一个 provider final 转写的安全持久化输入。
type FinalInput struct {
	MeetingID        string
	ASRSessionID     string
	ProviderResultID string
	Text             string
	Range            transcriptdomain.SampleRange
	SpeakerLabel     *string
	LastSentSample   int64
}

// GapInput 是一个已确定音频缺口的安全持久化输入。
type GapInput struct {
	MeetingID    string
	ASRSessionID *string
	Range        transcriptdomain.SampleRange
	Reason       transcriptdomain.GapReason
}

// PersistedEvent 是 final/gap 写入完成后广播给运行时与 UI 的最小事实。
type PersistedEvent struct {
	EventID   string
	EntityID  string
	Seq       int64
	Kind      string
	Duplicate bool
}

// EventService 确保 event header 与其投影在同一个 SQLite 事务提交。
type EventService struct {
	repository   *transcriptrepository.Repository
	transactions *database.TransactionManager
	ids          identity.Generator
	clock        clock.Clock
	onPersisted  func(meetingID string, event PersistedEvent)
}

// NewEventService 创建统一事件服务；构造阶段不进行 I/O。
func NewEventService(dependencies EventServiceDependencies) *EventService {
	return &EventService{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		ids: dependencies.IDs, clock: dependencies.Clock, onPersisted: dependencies.OnPersisted,
	}
}

// PersistFinal 将 provider final 原子写为 utterance.final 与 utterances 投影。
func (service *EventService) PersistFinal(ctx context.Context, input FinalInput) (PersistedEvent, error) {
	if err := validateFinalInput(input); err != nil {
		return PersistedEvent{}, err
	}
	if err := service.validateDependencies(); err != nil {
		return PersistedEvent{}, err
	}

	var result PersistedEvent
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.persistFinalInTransaction(ctx, tx, input, &result)
	})
	if err != nil {
		return PersistedEvent{}, mapPersistError(err, "transcript.final.persist")
	}
	service.notifyPersisted(input.MeetingID, result)
	return result, nil
}

// PersistGap 将确定缺失的样本范围原子写为 asr.gap 与 asr_gaps 投影。
func (service *EventService) PersistGap(ctx context.Context, input GapInput) (PersistedEvent, error) {
	if err := validateGapInput(input); err != nil {
		return PersistedEvent{}, err
	}
	if err := service.validateDependencies(); err != nil {
		return PersistedEvent{}, err
	}

	var result PersistedEvent
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.persistGapInTransaction(ctx, tx, input, &result)
	})
	if err != nil {
		return PersistedEvent{}, mapPersistError(err, "transcript.gap.persist")
	}
	service.notifyPersisted(input.MeetingID, result)
	return result, nil
}

// notifyPersisted 只在新事件提交后标记投影 dirty；幂等重放不触发无意义刷新。
func (service *EventService) notifyPersisted(meetingID string, event PersistedEvent) {
	if service.onPersisted != nil && !event.Duplicate {
		service.onPersisted(meetingID, event)
	}
}

// persistFinalInTransaction 在一个写事务中完成幂等检查、事件创建与游标推进。
func (service *EventService) persistFinalInTransaction(ctx context.Context, tx *gorm.DB, input FinalInput, result *PersistedEvent) error {
	meeting, err := service.repository.GetMeetingForEvent(ctx, tx, input.MeetingID)
	if err != nil {
		return err
	}
	if !acceptsTranscriptEvents(meeting) {
		return apperr.Biz(apperr.CodeConflict, apperr.WithOp("transcript.final.lifecycle"))
	}
	if _, err = service.repository.GetSessionForEvent(ctx, tx, input.MeetingID, input.ASRSessionID); err != nil {
		return err
	}
	if err = service.repository.AdvanceSessionSentSample(ctx, tx, input.MeetingID, input.ASRSessionID, input.LastSentSample, service.clock.Now().UnixMilli()); err != nil {
		return err
	}
	previous, err := service.repository.FindUtteranceByProviderResult(ctx, tx, input.ASRSessionID, input.ProviderResultID)
	if err != nil {
		return err
	}
	if previous != nil {
		return service.resolveFinalDuplicate(ctx, tx, previous, input, result)
	}

	now := service.clock.Now().UnixMilli()
	ids, err := service.newIDs(2)
	if err != nil {
		return err
	}
	eventID, utteranceID := ids[0], ids[1]
	seq, err := service.repository.NextEventSeq(ctx, tx, input.MeetingID)
	if err != nil {
		return err
	}
	event := models.MeetingEvent{ID: eventID, MeetingID: input.MeetingID, Seq: seq, Kind: "utterance.final", OccurredAt: eventOccurredAt(meeting, input.Range), Source: "asr", EntityType: pointer("utterance"), EntityID: pointer(utteranceID), CreatedAt: now, UpdatedAt: now}
	utterance := models.Utterance{
		ID: utteranceID, MeetingID: input.MeetingID, EventID: eventID, ASRSessionID: input.ASRSessionID,
		ProviderResultID: input.ProviderResultID, OriginalText: input.Text, CurrentText: input.Text,
		StartSample: input.Range.Start, EndSample: input.Range.End, ASRSpeakerLabel: input.SpeakerLabel,
		SpeakerAssignmentSource: "unassigned", TextRevision: 1, SpeakerRevision: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err = service.repository.CreateFinal(ctx, tx, event, utterance); err != nil {
		return err
	}
	if err = service.repository.UpdateLastFinalSample(ctx, tx, input.MeetingID, input.ASRSessionID, input.Range.End, now); err != nil {
		return err
	}
	*result = PersistedEvent{EventID: eventID, EntityID: utteranceID, Seq: seq, Kind: event.Kind}
	return nil
}

// persistGapInTransaction 在一个写事务中按可重放 origin key 去重并创建 gap。
func (service *EventService) persistGapInTransaction(ctx context.Context, tx *gorm.DB, input GapInput, result *PersistedEvent) error {
	meeting, err := service.repository.GetMeetingForEvent(ctx, tx, input.MeetingID)
	if err != nil {
		return err
	}
	if !acceptsTranscriptEvents(meeting) {
		return apperr.Biz(apperr.CodeConflict, apperr.WithOp("transcript.gap.lifecycle"))
	}
	if input.ASRSessionID != nil {
		if _, err = service.repository.GetSessionForEvent(ctx, tx, input.MeetingID, *input.ASRSessionID); err != nil {
			return err
		}
	}
	originKey, err := transcriptdomain.BuildGapOriginKey(input.MeetingID, input.Reason, input.Range)
	if err != nil {
		return err
	}
	previous, err := service.repository.FindGapByOriginKey(ctx, tx, originKey)
	if err != nil {
		return err
	}
	if previous != nil {
		return service.resolveGapDuplicate(ctx, tx, previous, result)
	}

	now := service.clock.Now().UnixMilli()
	ids, err := service.newIDs(2)
	if err != nil {
		return err
	}
	eventID, gapID := ids[0], ids[1]
	seq, err := service.repository.NextEventSeq(ctx, tx, input.MeetingID)
	if err != nil {
		return err
	}
	event := models.MeetingEvent{ID: eventID, MeetingID: input.MeetingID, Seq: seq, Kind: "asr.gap", OccurredAt: eventOccurredAt(meeting, input.Range), Source: "system", EntityType: pointer("asr_gap"), EntityID: pointer(gapID), CreatedAt: now, UpdatedAt: now}
	gap := models.ASRGap{ID: gapID, MeetingID: input.MeetingID, EventID: eventID, ASRSessionID: input.ASRSessionID, StartSample: input.Range.Start, EndSample: input.Range.End, Reason: string(input.Reason), OriginKey: originKey, State: "pending", CreatedAt: now, UpdatedAt: now}
	if err = service.repository.CreateGap(ctx, tx, event, gap); err != nil {
		return err
	}
	*result = PersistedEvent{EventID: eventID, EntityID: gapID, Seq: seq, Kind: event.Kind}
	return nil
}

// resolveFinalDuplicate 保证仅完全相同的 provider 重投会取得成功。
func (service *EventService) resolveFinalDuplicate(ctx context.Context, tx *gorm.DB, previous *models.Utterance, input FinalInput, result *PersistedEvent) error {
	if previous.MeetingID != input.MeetingID || previous.OriginalText != input.Text || previous.StartSample != input.Range.Start || previous.EndSample != input.Range.End {
		return apperr.Biz(apperr.CodeASRFinalInvalid, apperr.WithOp("transcript.final.idempotency"))
	}
	event, err := service.repository.FindEventByID(ctx, tx, previous.EventID)
	if err != nil {
		return fmt.Errorf("读取已存在 final 事件失败：%w", err)
	}
	if event == nil {
		return fmt.Errorf("读取已存在 final 事件失败：事件不存在")
	}
	*result = PersistedEvent{EventID: event.ID, EntityID: previous.ID, Seq: event.Seq, Kind: event.Kind, Duplicate: true}
	return nil
}

// resolveGapDuplicate 返回首次持久化的事件序号，避免重复产生 gap。
func (service *EventService) resolveGapDuplicate(ctx context.Context, tx *gorm.DB, previous *models.ASRGap, result *PersistedEvent) error {
	event, err := service.repository.FindEventByID(ctx, tx, previous.EventID)
	if err != nil {
		return fmt.Errorf("读取已存在 gap 事件失败：%w", err)
	}
	if event == nil {
		return fmt.Errorf("读取已存在 gap 事件失败：事件不存在")
	}
	*result = PersistedEvent{EventID: event.ID, EntityID: previous.ID, Seq: event.Seq, Kind: event.Kind, Duplicate: true}
	return nil
}

// validateFinalInput 在事务前拒绝不完整 provider 事实，避免创建无法补偿的记录。
func validateFinalInput(input FinalInput) error {
	if input.MeetingID == "" || input.ASRSessionID == "" || input.ProviderResultID == "" || strings.TrimSpace(input.Text) == "" || !isValidSampleRange(input.Range) || input.LastSentSample < input.Range.End {
		return apperr.Biz(apperr.CodeASRFinalInvalid, apperr.WithOp("transcript.final.validate"))
	}
	return nil
}

// validateGapInput 校验 gap 的可重放业务键组成部分。
func validateGapInput(input GapInput) error {
	if input.MeetingID == "" || !isValidSampleRange(input.Range) || !input.Reason.IsValid() {
		return apperr.Biz(apperr.CodeASRFinalInvalid, apperr.WithOp("transcript.gap.validate"))
	}
	return nil
}

// isValidSampleRange 复用领域构造器验证外部输入携带的样本范围。
func isValidSampleRange(sampleRange transcriptdomain.SampleRange) bool {
	_, err := transcriptdomain.NewSampleRange(sampleRange.Start, sampleRange.End)
	return err == nil
}

// validateDependencies 避免运行期 nil dependency 伪装成可重试持久化失败。
func (service *EventService) validateDependencies() error {
	if service == nil || service.repository == nil || service.transactions == nil || service.ids == nil || service.clock == nil {
		return fmt.Errorf("实时转写事件服务依赖未初始化")
	}
	return nil
}

// newIDs 分配固定数量 ID，并防止测试替身耗尽时写入空主键。
func (service *EventService) newIDs(count int) ([]string, error) {
	ids := make([]string, 0, count)
	for range count {
		id := service.ids.New()
		if id == "" {
			return nil, fmt.Errorf("生成实时转写事件 ID 失败")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// acceptsTranscriptEvents 仅允许录音中或收尾中的会议接收已在途 final/gap。
func acceptsTranscriptEvents(meeting models.Meeting) bool {
	return meeting.LifecycleState == "preparing" || meeting.LifecycleState == "recording" || meeting.LifecycleState == "finalizing"
}

// eventOccurredAt 将全局样本时间线映射到会议开始的毫秒时间线。
func eventOccurredAt(meeting models.Meeting, sampleRange transcriptdomain.SampleRange) int64 {
	if meeting.StartedAt == nil {
		return 0
	}
	return *meeting.StartedAt + sampleRange.Start*1000/transcriptdomain.SampleRate
}

// mapPersistError 保留明确的业务错误，其余数据库错误统一变为安全的持久化失败。
func mapPersistError(err error, operation string) error {
	if err == nil {
		return nil
	}
	var appErr *apperr.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return apperr.Dependency(apperr.CodeASREventPersistFailed, err, apperr.WithOp(operation))
}

func pointer(value string) *string { return &value }
