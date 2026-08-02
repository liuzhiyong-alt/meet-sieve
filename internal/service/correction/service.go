package correction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	correctionrepository "meet-sieve/internal/repository/correction"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const maxCorrectionTextBytes = 10_000

// RawRecordFlusher 定义 SQLite 提交后同步刷新 Markdown 的边界。
type RawRecordFlusher interface {
	Flush(ctx context.Context, meetingID string) error
}

// ServiceDependencies 描述人工校对短事务与提交后投影依赖。
type ServiceDependencies struct {
	Repository   *correctionrepository.Repository
	Transactions *database.TransactionManager
	IDs          identity.Generator
	Clock        clock.Clock
	RawRecord    RawRecordFlusher
}

// CommandBase 是所有校对命令共享的幂等、作用域和乐观锁字段。
type CommandBase struct {
	RequestID        string
	MeetingID        string
	TargetID         string
	ExpectedRevision int
	OperatorID       string
	Reason           string
}

// TextCommand 修改单条 utterance 当前文字。
type TextCommand struct {
	CommandBase
	Text string
}

// SpeakerCommand 修改单条 utterance 当前说话人。
type SpeakerCommand struct {
	CommandBase
	ParticipantID string
}

// ResourceCommand 修改 completed resource 当前说明。
type ResourceCommand struct {
	CommandBase
	Description string
}

// ClusterCommand 把确认时的 cluster revision 与影响条数绑定到批量提交。
type ClusterCommand struct {
	CommandBase
	ParticipantID string
	ExpectedCount int
}

// ClusterPreview 返回批量确认所需的稳定 cluster 名称、revision 和当前影响条数。
type ClusterPreview struct {
	ClusterID     string
	DisplayName   string
	Revision      int
	ImpactedCount int
}

// Result 明确区分 SQLite 保存和 Markdown 投影结果。
type Result struct {
	CorrectionID        string
	ResultRevision      int
	Saved               bool
	Duplicate           bool
	NoOp                bool
	ProjectionState     string
	ProjectionErrorCode string
	ImpactedCount       int
}

// PreviewCluster 读取指定 meeting + cluster 当前批量作用域。
func (service *Service) PreviewCluster(ctx context.Context, meetingID string, clusterID string) (ClusterPreview, error) {
	base := CommandBase{RequestID: uuid.NewString(), MeetingID: meetingID, TargetID: clusterID, ExpectedRevision: 1, OperatorID: uuid.NewString()}
	if err := validateServiceCommand(service, base); err != nil {
		return ClusterPreview{}, err
	}
	var preview ClusterPreview
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		cluster, err := service.repository.GetCluster(ctx, tx, meetingID, clusterID)
		if err != nil {
			return mapTargetError(err, "correction.cluster.preview")
		}
		rows, err := service.repository.ListClusterUtterances(ctx, tx, meetingID, clusterID)
		if err != nil {
			return err
		}
		preview = ClusterPreview{ClusterID: cluster.ID, DisplayName: fmt.Sprintf("未知说话人 %d", cluster.DisplayNo), Revision: cluster.Revision, ImpactedCount: len(rows)}
		return nil
	})
	return preview, err
}

// CorrectCluster 批量覆盖指定 cluster 当前全部片段，并为每条保存 before/after。
func (service *Service) CorrectCluster(ctx context.Context, command ClusterCommand) (Result, error) {
	if _, err := uuid.Parse(command.ParticipantID); err != nil || command.ExpectedCount <= 0 {
		return Result{}, apperr.Biz(apperr.CodeCorrectionTargetNotFound, apperr.WithOp("correction.cluster.validate"))
	}
	if err := validateServiceCommand(service, command.CommandBase); err != nil {
		return Result{}, err
	}
	var result Result
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.correctClusterInTransaction(ctx, tx, command, &result)
	})
	if err != nil {
		return Result{}, err
	}
	if !result.NoOp {
		result.Saved = true
	}
	result.ProjectionState = "current"
	if !result.NoOp && service.rawRecord != nil {
		if err := service.rawRecord.Flush(ctx, command.MeetingID); err != nil {
			result.ProjectionState, result.ProjectionErrorCode = "failed", apperr.CodeRawRecordRefreshFailed.ErrorCode
		}
	}
	return result, nil
}

// correctClusterInTransaction 校验确认快照并原子提交批量投影、审计和事件。
func (service *Service) correctClusterInTransaction(ctx context.Context, tx *gorm.DB, command ClusterCommand, result *Result) error {
	existing, err := service.repository.FindByRequest(ctx, tx, command.RequestID)
	if err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.cluster.idempotency"))
	}
	afterJSON := mustJSON(map[string]any{"participant_id": command.ParticipantID, "source": "manual_cluster"})
	if existing != nil {
		if existing.MeetingID != command.MeetingID || existing.TargetID != command.TargetID || existing.TargetKind != "speaker_cluster" || existing.CorrectionKind != "member_assignment" || existing.TargetRevision != command.ExpectedRevision || existing.AfterJSON != afterJSON {
			return apperr.Biz(apperr.CodeCorrectionIdempotencyConflict, apperr.WithOp("correction.cluster.idempotency.conflict"))
		}
		*result = Result{CorrectionID: existing.ID, ResultRevision: existing.ResultRevision, Duplicate: true, ImpactedCount: command.ExpectedCount}
		return nil
	}
	meeting, err := service.repository.GetMeeting(ctx, tx, command.MeetingID)
	if err != nil {
		return mapTargetError(err, "correction.cluster.meeting")
	}
	if meeting.LifecycleState != "ended" && meeting.LifecycleState != "interrupted" {
		return apperr.Biz(apperr.CodeCorrectionMeetingStateInvalid, apperr.WithOp("correction.cluster.meeting.state"))
	}
	cluster, err := service.repository.GetCluster(ctx, tx, command.MeetingID, command.TargetID)
	if err != nil {
		return mapTargetError(err, "correction.cluster.target")
	}
	rows, err := service.repository.ListClusterUtterances(ctx, tx, command.MeetingID, command.TargetID)
	if err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.cluster.scope"))
	}
	if cluster.Revision != command.ExpectedRevision || len(rows) != command.ExpectedCount {
		return apperr.Biz(apperr.CodeCorrectionRevisionConflict, apperr.WithOp("correction.cluster.revision"))
	}
	exists, err := service.repository.ParticipantExists(ctx, tx, command.MeetingID, command.ParticipantID)
	if err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.cluster.participant"))
	}
	if !exists {
		return apperr.Biz(apperr.CodeCorrectionTargetNotFound, apperr.WithOp("correction.cluster.participant"))
	}
	if cluster.AssignedParticipantID != nil && *cluster.AssignedParticipantID == command.ParticipantID && cluster.AssignmentSource == "manual" && clusterRowsAreManual(rows, command.ParticipantID) {
		*result = Result{NoOp: true, ResultRevision: cluster.Revision, ImpactedCount: len(rows)}
		return nil
	}
	return service.commitClusterCorrection(ctx, tx, command, cluster, rows, afterJSON, result)
}

// commitClusterCorrection 创建逐条 item 后一次提交整个业务事务。
func (service *Service) commitClusterCorrection(ctx context.Context, tx *gorm.DB, command ClusterCommand, cluster models.SpeakerCluster, rows []correctionrepository.ClusterUtteranceRow, afterJSON string, result *Result) error {
	correctionID, eventID, err := service.nextTwoUUIDs()
	if err != nil {
		return err
	}
	now := service.clock.Now().UnixMilli()
	items := make([]models.CorrectionItem, 0, len(rows))
	for index, row := range rows {
		itemID, err := service.nextUUID("correction.cluster.item")
		if err != nil {
			return err
		}
		before := mustJSON(map[string]any{"participant_id": row.CurrentParticipantID, "source": row.SpeakerAssignmentSource})
		items = append(items, models.CorrectionItem{ID: itemID, CorrectionID: correctionID, TargetKind: "utterance", TargetID: row.ID, BeforeJSON: before, AfterJSON: afterJSON, ItemOrder: index + 1, CreatedAt: now, UpdatedAt: now})
	}
	if err := service.repository.AssignCluster(ctx, tx, cluster, command.ParticipantID, now); err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.cluster.update"))
	}
	seq, err := service.repository.NextEventSeq(ctx, tx, command.MeetingID)
	if err != nil {
		return err
	}
	entityType, payload := "correction", `{"scope":"speaker_cluster"}`
	operatorID := command.OperatorID
	beforeJSON := mustJSON(map[string]any{"participant_id": cluster.AssignedParticipantID, "source": cluster.AssignmentSource})
	event := models.MeetingEvent{ID: eventID, MeetingID: command.MeetingID, Seq: seq, Kind: "speaker.corrected", OccurredAt: now, Source: "host", EntityType: &entityType, EntityID: &correctionID, PayloadJSON: &payload, CreatedAt: now, UpdatedAt: now}
	correction := models.Correction{ID: correctionID, MeetingID: command.MeetingID, EventID: eventID, RequestID: command.RequestID, TargetKind: "speaker_cluster", TargetID: command.TargetID, CorrectionKind: "member_assignment", BeforeJSON: beforeJSON, AfterJSON: afterJSON, OperatorKind: "host", OperatorID: &operatorID, Reason: optionalString(command.Reason), TargetRevision: cluster.Revision, ResultRevision: cluster.Revision + 1, BatchScope: "speaker_cluster", CreatedAt: now, UpdatedAt: now}
	if err := service.repository.CreateBatchAudit(ctx, tx, event, correction, items); err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.cluster.audit"))
	}
	*result = Result{CorrectionID: correctionID, ResultRevision: cluster.Revision + 1, ImpactedCount: len(rows)}
	return nil
}

// clusterRowsAreManual 判断所有当前片段是否已是同一批量人工投影。
func clusterRowsAreManual(rows []correctionrepository.ClusterUtteranceRow, participantID string) bool {
	for _, row := range rows {
		if row.CurrentParticipantID == nil || *row.CurrentParticipantID != participantID || row.SpeakerAssignmentSource != "manual_cluster" {
			return false
		}
	}
	return true
}

// nextTwoUUIDs 生成批量 correction 和 event ID。
func (service *Service) nextTwoUUIDs() (string, string, error) {
	first, err := service.nextUUID("correction.cluster.id")
	if err != nil {
		return "", "", err
	}
	second, err := service.nextUUID("correction.cluster.event")
	return first, second, err
}

// nextUUID 生成一个 UUID v4。
func (service *Service) nextUUID(operation string) (string, error) {
	value := service.ids.New()
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 4 {
		return "", apperr.Sys(fmt.Errorf("生成 correction UUID v4 失败"), apperr.WithOp(operation))
	}
	return value, nil
}

// Service 编排单条文本、说话人和 resource 校对。
type Service struct {
	repository   *correctionrepository.Repository
	transactions *database.TransactionManager
	ids          identity.Generator
	clock        clock.Clock
	rawRecord    RawRecordFlusher
}

// NewService 创建校对服务；依赖在命令执行前统一校验。
func NewService(dependencies ServiceDependencies) *Service {
	return &Service{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		ids: dependencies.IDs, clock: dependencies.Clock, rawRecord: dependencies.RawRecord,
	}
}

// CorrectText 校对单条 current text，原始 ASR 永不覆盖。
func (service *Service) CorrectText(ctx context.Context, command TextCommand) (Result, error) {
	text := strings.TrimSpace(command.Text)
	if text == "" || len([]byte(text)) > maxCorrectionTextBytes {
		return Result{}, apperr.Biz(apperr.CodeCorrectionTextInvalid, apperr.WithOp("correction.text.validate"))
	}
	return service.execute(ctx, correctionCommand{base: command.CommandBase, targetKind: "utterance", correctionKind: "text", value: text})
}

// CorrectSpeaker 校对单条 current speaker，保留 track、cluster 和自动分数历史。
func (service *Service) CorrectSpeaker(ctx context.Context, command SpeakerCommand) (Result, error) {
	if _, err := uuid.Parse(command.ParticipantID); err != nil {
		return Result{}, apperr.Biz(apperr.CodeCorrectionTargetNotFound, apperr.WithOp("correction.speaker.participant"))
	}
	return service.execute(ctx, correctionCommand{base: command.CommandBase, targetKind: "utterance", correctionKind: "member_assignment", value: command.ParticipantID})
}

// CorrectResource 校对同场 completed resource 的当前说明。
func (service *Service) CorrectResource(ctx context.Context, command ResourceCommand) (Result, error) {
	description := strings.TrimSpace(command.Description)
	if description == "" || len([]byte(description)) > maxCorrectionTextBytes {
		return Result{}, apperr.Biz(apperr.CodeCorrectionTextInvalid, apperr.WithOp("correction.resource.validate"))
	}
	return service.execute(ctx, correctionCommand{base: command.CommandBase, targetKind: "resource", correctionKind: "description", value: description})
}

type correctionCommand struct {
	base           CommandBase
	targetKind     string
	correctionKind string
	value          string
}

type targetChange struct {
	beforeJSON     string
	afterJSON      string
	targetRevision int
	resultRevision int
	eventKind      string
	noOp           bool
	apply          func() error
}

// execute 在单事务内完成幂等检查、目标更新、审计和统一事件，再同步刷新投影。
func (service *Service) execute(ctx context.Context, command correctionCommand) (Result, error) {
	if err := validateServiceCommand(service, command.base); err != nil {
		return Result{}, err
	}
	var result Result
	err := service.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return service.executeInTransaction(ctx, tx, command, &result)
	})
	if err != nil {
		return Result{}, err
	}
	if result.NoOp {
		return result, nil
	}
	result.Saved = true
	result.ProjectionState = "current"
	if service.rawRecord != nil {
		if err := service.rawRecord.Flush(ctx, command.base.MeetingID); err != nil {
			result.ProjectionState = "failed"
			result.ProjectionErrorCode = apperr.CodeRawRecordRefreshFailed.ErrorCode
		}
	}
	return result, nil
}

// executeInTransaction 校验会议与目标，并以 correction/item/event 全量审计一次变化。
func (service *Service) executeInTransaction(ctx context.Context, tx *gorm.DB, command correctionCommand, result *Result) error {
	existing, err := service.repository.FindByRequest(ctx, tx, command.base.RequestID)
	if err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.idempotency.read"))
	}
	if existing != nil {
		return resolveIdempotent(existing, command, result)
	}
	meeting, err := service.repository.GetMeeting(ctx, tx, command.base.MeetingID)
	if err != nil {
		return mapTargetError(err, "correction.meeting.read")
	}
	if meeting.LifecycleState != "ended" && meeting.LifecycleState != "interrupted" {
		return apperr.Biz(apperr.CodeCorrectionMeetingStateInvalid, apperr.WithOp("correction.meeting.state"))
	}
	change, err := service.buildTargetChange(ctx, tx, command)
	if err != nil {
		return err
	}
	if change.targetRevision != command.base.ExpectedRevision {
		return apperr.Biz(apperr.CodeCorrectionRevisionConflict, apperr.WithOp("correction.revision"))
	}
	if change.noOp {
		*result = Result{NoOp: true, ResultRevision: change.targetRevision, ProjectionState: "current"}
		return nil
	}
	ids, err := service.newAuditIDs()
	if err != nil {
		return err
	}
	if err := change.apply(); err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.target.update"))
	}
	if err := service.createAudit(ctx, tx, command, change, ids); err != nil {
		return apperr.Sys(err, apperr.WithOp("correction.audit.create"))
	}
	*result = Result{CorrectionID: ids.correctionID, ResultRevision: change.resultRevision}
	return nil
}

// buildTargetChange 根据目标类型构造不可变 before/after 和乐观更新闭包。
func (service *Service) buildTargetChange(ctx context.Context, tx *gorm.DB, command correctionCommand) (targetChange, error) {
	switch command.correctionKind {
	case "text":
		return service.buildTextChange(ctx, tx, command)
	case "member_assignment":
		return service.buildSpeakerChange(ctx, tx, command)
	case "description":
		return service.buildResourceChange(ctx, tx, command)
	default:
		return targetChange{}, apperr.Biz(apperr.CodeCorrectionTargetNotFound)
	}
}

// buildTextChange 构造单条文字变更。
func (service *Service) buildTextChange(ctx context.Context, tx *gorm.DB, command correctionCommand) (targetChange, error) {
	target, err := service.repository.GetUtterance(ctx, tx, command.base.MeetingID, command.base.TargetID)
	if err != nil {
		return targetChange{}, mapTargetError(err, "correction.text.target")
	}
	before := mustJSON(map[string]any{"text": target.CurrentText})
	after := mustJSON(map[string]any{"text": command.value})
	return targetChange{
		beforeJSON: before, afterJSON: after, targetRevision: target.TextRevision,
		resultRevision: target.TextRevision + 1, eventKind: "utterance.corrected", noOp: target.CurrentText == command.value,
		apply: func() error {
			return service.repository.UpdateUtteranceText(ctx, tx, target, command.value, service.clock.Now().UnixMilli())
		},
	}, nil
}

// buildSpeakerChange 构造只影响一个 utterance 的人工说话人变更。
func (service *Service) buildSpeakerChange(ctx context.Context, tx *gorm.DB, command correctionCommand) (targetChange, error) {
	target, err := service.repository.GetUtterance(ctx, tx, command.base.MeetingID, command.base.TargetID)
	if err != nil {
		return targetChange{}, mapTargetError(err, "correction.speaker.target")
	}
	exists, err := service.repository.ParticipantExists(ctx, tx, command.base.MeetingID, command.value)
	if err != nil {
		return targetChange{}, apperr.Sys(err, apperr.WithOp("correction.speaker.participant"))
	}
	if !exists {
		return targetChange{}, apperr.Biz(apperr.CodeCorrectionTargetNotFound, apperr.WithOp("correction.speaker.participant"))
	}
	before := mustJSON(map[string]any{"participant_id": target.CurrentParticipantID, "source": target.SpeakerAssignmentSource})
	after := mustJSON(map[string]any{"participant_id": command.value, "source": "manual_single"})
	noOp := target.CurrentParticipantID != nil && *target.CurrentParticipantID == command.value && target.SpeakerAssignmentSource == "manual_single"
	return targetChange{
		beforeJSON: before, afterJSON: after, targetRevision: target.SpeakerRevision,
		resultRevision: target.SpeakerRevision + 1, eventKind: "speaker.corrected", noOp: noOp,
		apply: func() error {
			return service.repository.UpdateUtteranceSpeaker(ctx, tx, target, command.value, service.clock.Now().UnixMilli())
		},
	}, nil
}

// buildResourceChange 构造 completed resource 当前说明变更。
func (service *Service) buildResourceChange(ctx context.Context, tx *gorm.DB, command correctionCommand) (targetChange, error) {
	target, err := service.repository.GetResource(ctx, tx, command.base.MeetingID, command.base.TargetID)
	if err != nil {
		return targetChange{}, mapTargetError(err, "correction.resource.target")
	}
	current := ""
	if target.CurrentDescription != nil {
		current = *target.CurrentDescription
	}
	before := mustJSON(map[string]any{"description": current})
	after := mustJSON(map[string]any{"description": command.value})
	return targetChange{
		beforeJSON: before, afterJSON: after, targetRevision: target.DescriptionRevision,
		resultRevision: target.DescriptionRevision + 1, eventKind: "resource.corrected", noOp: current == command.value,
		apply: func() error {
			return service.repository.UpdateResourceDescription(ctx, tx, target, command.value, service.clock.Now().UnixMilli())
		},
	}, nil
}

type auditIDs struct{ correctionID, itemID, eventID string }

// createAudit 写入统一事件、correction 和单条 correction item。
func (service *Service) createAudit(ctx context.Context, tx *gorm.DB, command correctionCommand, change targetChange, ids auditIDs) error {
	seq, err := service.repository.NextEventSeq(ctx, tx, command.base.MeetingID)
	if err != nil {
		return err
	}
	now := service.clock.Now().UnixMilli()
	entityType, payload := "correction", `{"scope":"single"}`
	reason := optionalString(command.base.Reason)
	operatorID := command.base.OperatorID
	event := models.MeetingEvent{ID: ids.eventID, MeetingID: command.base.MeetingID, Seq: seq, Kind: change.eventKind, OccurredAt: now, Source: "host", EntityType: &entityType, EntityID: &ids.correctionID, PayloadJSON: &payload, CreatedAt: now, UpdatedAt: now}
	correction := models.Correction{
		ID: ids.correctionID, MeetingID: command.base.MeetingID, EventID: ids.eventID, RequestID: command.base.RequestID,
		TargetKind: command.targetKind, TargetID: command.base.TargetID, CorrectionKind: command.correctionKind,
		BeforeJSON: change.beforeJSON, AfterJSON: change.afterJSON, OperatorKind: "host", OperatorID: &operatorID,
		Reason: reason, TargetRevision: change.targetRevision, ResultRevision: change.resultRevision,
		BatchScope: "single", CreatedAt: now, UpdatedAt: now,
	}
	item := models.CorrectionItem{ID: ids.itemID, CorrectionID: ids.correctionID, TargetKind: command.targetKind, TargetID: command.base.TargetID, BeforeJSON: change.beforeJSON, AfterJSON: change.afterJSON, ItemOrder: 1, CreatedAt: now, UpdatedAt: now}
	return service.repository.CreateAudit(ctx, tx, event, correction, item)
}

// resolveIdempotent 比较已提交的目标、kind、revision 和 canonical after payload。
func resolveIdempotent(existing *models.Correction, command correctionCommand, result *Result) error {
	after := canonicalAfterJSON(command)
	if existing.MeetingID != command.base.MeetingID || existing.TargetKind != command.targetKind || existing.TargetID != command.base.TargetID ||
		existing.CorrectionKind != command.correctionKind || existing.TargetRevision != command.base.ExpectedRevision || existing.AfterJSON != after {
		return apperr.Biz(apperr.CodeCorrectionIdempotencyConflict, apperr.WithOp("correction.idempotency.conflict"))
	}
	*result = Result{CorrectionID: existing.ID, ResultRevision: existing.ResultRevision, Duplicate: true}
	return nil
}

// canonicalAfterJSON 返回命令在审计中使用的稳定 after JSON。
func canonicalAfterJSON(command correctionCommand) string {
	if command.correctionKind == "text" {
		return mustJSON(map[string]any{"text": command.value})
	}
	if command.correctionKind == "description" {
		return mustJSON(map[string]any{"description": command.value})
	}
	return mustJSON(map[string]any{"participant_id": command.value, "source": "manual_single"})
}

// newAuditIDs 一次生成三个 UUID v4，分别用于 correction、item 和 event。
func (service *Service) newAuditIDs() (auditIDs, error) {
	values := []string{service.ids.New(), service.ids.New(), service.ids.New()}
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed.Version() != 4 {
			return auditIDs{}, apperr.Sys(fmt.Errorf("生成 correction UUID v4 失败"), apperr.WithOp("correction.id.generate"))
		}
	}
	return auditIDs{correctionID: values[0], itemID: values[1], eventID: values[2]}, nil
}

// validateServiceCommand 校验依赖、UUID、revision 和本地主机操作人。
func validateServiceCommand(service *Service, command CommandBase) error {
	if service == nil || service.repository == nil || service.transactions == nil || service.ids == nil || service.clock == nil {
		return apperr.Sys(fmt.Errorf("correction service 依赖不完整"), apperr.WithOp("correction.validate"))
	}
	for _, value := range []string{command.RequestID, command.MeetingID, command.TargetID, command.OperatorID} {
		if _, err := uuid.Parse(value); err != nil {
			return apperr.Biz(apperr.CodeCorrectionTargetNotFound, apperr.WithOp("correction.validate.uuid"))
		}
	}
	if command.ExpectedRevision < 1 {
		return apperr.Biz(apperr.CodeCorrectionRevisionConflict, apperr.WithOp("correction.validate.revision"))
	}
	return nil
}

// mapTargetError 把不存在映射为稳定业务错误，其他数据库错误保持内部错误。
func mapTargetError(err error, operation string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperr.Biz(apperr.CodeCorrectionTargetNotFound, apperr.WithOp(operation))
	}
	return apperr.Sys(err, apperr.WithOp(operation))
}

// mustJSON 编码仅由内部固定结构构造的 canonical JSON。
func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// optionalString 对审计 reason 保留 NULL 语义。
func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
