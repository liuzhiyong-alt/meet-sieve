package gap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	domaingap "meet-sieve/internal/domain/gap"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/identity"
	gaprepository "meet-sieve/internal/repository/gap"
	"meet-sieve/models"
)

// ConflictRepository 提供冲突读取与单事务解决能力。
type ConflictRepository interface {
	ReadConflict(context.Context, string, string) (gaprepository.ConflictRecord, error)
	HasMatchingResolution(context.Context, string, string, string, string) (bool, error)
	ResolveGapConflict(context.Context, gaprepository.ResolveConflictInput) error
	GetAttempt(context.Context, string) (models.GapTranscriptionAttempt, error)
	MarkGapAssetDeleted(context.Context, string, int64) error
}

// ConflictEvidence 是 Wails 可安全投影的双份证据，不包含路径或完整 provider ID。
type ConflictEvidence struct {
	GapID            string
	Revision         int64
	CoreStartSample  int64
	CoreEndSample    int64
	AudioStartSample int64
	AudioEndSample   int64
	AudioClipID      string
	Candidates       []domaingap.CandidateSegment
	Existing         []gaprepository.ConflictUtteranceRow
	Context          []gaprepository.ConflictUtteranceRow
}

// ConflictQueryService 读取实时双份证据。
type ConflictQueryService struct{ repository ConflictRepository }

// NewConflictQueryService 创建冲突查询服务。
func NewConflictQueryService(repository ConflictRepository) *ConflictQueryService {
	return &ConflictQueryService{repository: repository}
}

// GetConflict 返回当前 revision 对应的候选、现有文本和相邻上下文。
func (service *ConflictQueryService) GetConflict(ctx context.Context, meetingID string, gapID string) (ConflictEvidence, error) {
	if service == nil || service.repository == nil || meetingID == "" || gapID == "" {
		return ConflictEvidence{}, fmt.Errorf("读取补转写冲突：参数无效")
	}
	record, err := service.repository.ReadConflict(ctx, meetingID, gapID)
	if err != nil {
		return ConflictEvidence{}, err
	}
	candidates, err := decodeConflictCandidates(record.Attempt.ResponseJSON)
	if err != nil {
		return ConflictEvidence{}, apperr.Dependency(apperr.CodeGapTranscriptionConflict, err, apperr.WithOp("gap.conflict.decode"))
	}
	return ConflictEvidence{
		GapID: gapID, Revision: record.Gap.UpdatedAt,
		CoreStartSample: record.Attempt.CoreStartSample, CoreEndSample: record.Attempt.CoreEndSample,
		AudioStartSample: record.Attempt.AudioStartSample, AudioEndSample: record.Attempt.AudioEndSample,
		AudioClipID: record.AudioAsset.ID, Candidates: candidates,
		Existing: append([]gaprepository.ConflictUtteranceRow(nil), record.Existing...), Context: append([]gaprepository.ConflictUtteranceRow(nil), record.Context...),
	}, nil
}

// ResolutionCommand 描述主持人提交的明确冲突动作和逐条文字。
type ResolutionCommand struct {
	MeetingID  string
	GapID      string
	Revision   int64
	Resolution domaingap.Resolution
	Edits      []gaprepository.ResolutionEdit
	RequestID  string
	OperatorID string
}

// ResolutionService 原子提交冲突解决并在事务后刷新原始记录。
type ResolutionService struct {
	repository ConflictRepository
	extractor  *Extractor
	rawRecord  RawRecordFlusher
	ids        identity.Generator
	clock      clock.Clock
	events     EventSink
}

// NewResolutionService 创建冲突解决服务。
func NewResolutionService(repository ConflictRepository, extractor *Extractor, rawRecord RawRecordFlusher, ids identity.Generator, appClock clock.Clock) *ResolutionService {
	return &ResolutionService{repository: repository, extractor: extractor, rawRecord: rawRecord, ids: ids, clock: appClock}
}

// SetEventSink 在装配阶段接入冲突解决后的轻量刷新通知。
func (service *ResolutionService) SetEventSink(events EventSink) {
	if service != nil {
		service.events = events
	}
}

// Resolve 校验 revision 和逐条 edit 后提交，不猜测跨 utterance 拼接。
func (service *ResolutionService) Resolve(ctx context.Context, command ResolutionCommand) error {
	if err := service.validate(command); err != nil {
		return err
	}
	duplicate, err := service.repository.HasMatchingResolution(ctx, command.MeetingID, command.GapID, string(command.Resolution), command.RequestID)
	if err != nil || duplicate {
		return err
	}
	record, err := service.repository.ReadConflict(ctx, command.MeetingID, command.GapID)
	if err != nil {
		return err
	}
	if record.Gap.UpdatedAt != command.Revision {
		return gaprepository.ErrConflict
	}
	candidates, err := decodeConflictCandidates(record.Attempt.ResponseJSON)
	if err != nil {
		return err
	}
	if err := validateResolutionEdits(command, record.Existing, candidates); err != nil {
		return err
	}
	input, err := service.buildResolution(command, record, candidates)
	if err != nil {
		return err
	}
	if err := service.repository.ResolveGapConflict(ctx, input); err != nil {
		return err
	}
	if service.events != nil {
		service.events.PublishGapChanged(command.MeetingID)
	}
	flushErr := service.rawRecord.Flush(ctx, command.MeetingID)
	service.cleanupResolvedAttempt(ctx, record)
	if flushErr != nil {
		return apperr.Dependency(apperr.CodeRawRecordRefreshFailed, flushErr, apperr.WithOp("gap.conflict.flush"))
	}
	return nil
}

// buildResolution 构造非重叠 file 事实、逐条 correction 和 resolution 审计事件。
func (service *ResolutionService) buildResolution(command ResolutionCommand, record gaprepository.ConflictRecord, candidates []domaingap.CandidateSegment) (gaprepository.ResolveConflictInput, error) {
	now := service.clock.Now().UnixMilli()
	fileCandidates := nonOverlappingCandidates(candidates, record.Existing)
	session, events, utterances, err := service.buildFileFacts(record, fileCandidates, now)
	if err != nil {
		return gaprepository.ResolveConflictInput{}, err
	}
	correctionEvents, corrections, err := service.buildCorrections(command, record.Existing, now)
	if err != nil {
		return gaprepository.ResolveConflictInput{}, err
	}
	payloadBytes, _ := json.Marshal(map[string]any{"v": 1, "resolution": command.Resolution, "request_id": command.RequestID})
	payload, entityType, entityID := string(payloadBytes), "asr_gap", command.GapID
	resolutionEvent := models.MeetingEvent{ID: service.ids.New(), MeetingID: command.MeetingID, Kind: "asr.compensated", OccurredAt: now, Source: "host", EntityType: &entityType, EntityID: &entityID, PayloadJSON: &payload, CreatedAt: now, UpdatedAt: now}
	return gaprepository.ResolveConflictInput{
		MeetingID: command.MeetingID, GapID: command.GapID, AttemptID: record.Attempt.ID,
		ExpectedUpdatedAt: command.Revision, RequestID: command.RequestID, Resolution: string(command.Resolution),
		ResolutionEvent: resolutionEvent, Session: session, Events: events, Utterances: utterances,
		CorrectionEvents: correctionEvents, Corrections: corrections, Edits: command.Edits, UpdatedAt: now,
	}, nil
}

// buildFileFacts 为不与现有转写重叠的候选创建 synthetic file session。
func (service *ResolutionService) buildFileFacts(record gaprepository.ConflictRecord, candidates []domaingap.CandidateSegment, now int64) (*models.ASRSession, []models.MeetingEvent, []models.Utterance, error) {
	if len(candidates) == 0 {
		return nil, nil, nil, nil
	}
	sessionID := service.ids.New()
	providerID := record.Attempt.ProviderRequestID
	session := &models.ASRSession{ID: sessionID, MeetingID: record.Attempt.MeetingID, Provider: "volcano", ProviderSessionID: &providerID, State: "stopped", StartedAt: now, EndedAt: &now, TransportMode: "auc_flash_v3", InputStartSample: record.Attempt.AudioStartSample, LastSentSample: record.Attempt.AudioEndSample, LastFinalSample: record.Attempt.AudioEndSample, CreatedAt: now, UpdatedAt: now}
	events := make([]models.MeetingEvent, 0, len(candidates))
	utterances := make([]models.Utterance, 0, len(candidates))
	for index, candidate := range candidates {
		eventID, utteranceID := service.ids.New(), service.ids.New()
		entityType := "utterance"
		events = append(events, models.MeetingEvent{ID: eventID, MeetingID: record.Attempt.MeetingID, Kind: "asr.compensated", OccurredAt: now, Source: "asr", EntityType: &entityType, EntityID: &utteranceID, CreatedAt: now, UpdatedAt: now})
		utterances = append(utterances, models.Utterance{ID: utteranceID, MeetingID: record.Attempt.MeetingID, ASRSessionID: sessionID, ProviderResultID: fmt.Sprintf("%s:resolved:%d", record.Attempt.ProviderRequestID, index), OriginalText: candidate.Text, CurrentText: candidate.Text, StartSample: candidate.StartSample, EndSample: candidate.EndSample, SpeakerAssignmentSource: "unassigned", TextRevision: 1, SpeakerRevision: 1, CreatedAt: now, UpdatedAt: now})
	}
	return session, events, utterances, nil
}

// buildCorrections 为每个明确 edit 创建独立 single correction 审计。
func (service *ResolutionService) buildCorrections(command ResolutionCommand, existing []gaprepository.ConflictUtteranceRow, now int64) ([]models.MeetingEvent, []models.Correction, error) {
	if command.Resolution == domaingap.ResolutionKeepExisting {
		return nil, nil, nil
	}
	byID := make(map[string]gaprepository.ConflictUtteranceRow, len(existing))
	for _, row := range existing {
		byID[row.ID] = row
	}
	events := make([]models.MeetingEvent, 0, len(command.Edits))
	corrections := make([]models.Correction, 0, len(command.Edits))
	for _, edit := range command.Edits {
		row := byID[edit.TargetID]
		eventID, correctionID, correctionRequestID := service.ids.New(), service.ids.New(), service.ids.New()
		before, _ := json.Marshal(map[string]any{"text": row.CurrentText})
		after, _ := json.Marshal(map[string]any{"text": edit.Text})
		entityType := "correction"
		payload := `{"v":1,"scope":"gap_conflict"}`
		events = append(events, models.MeetingEvent{ID: eventID, MeetingID: command.MeetingID, Kind: "utterance.corrected", OccurredAt: now, Source: "host", EntityType: &entityType, EntityID: &correctionID, PayloadJSON: &payload, CreatedAt: now, UpdatedAt: now})
		operatorID := command.OperatorID
		corrections = append(corrections, models.Correction{ID: correctionID, MeetingID: command.MeetingID, EventID: eventID, RequestID: correctionRequestID, TargetKind: "utterance", TargetID: row.ID, CorrectionKind: "text", BeforeJSON: string(before), AfterJSON: string(after), OperatorKind: "host", OperatorID: &operatorID, TargetRevision: row.TextRevision, ResultRevision: row.TextRevision + 1, BatchScope: "single", CreatedAt: now, UpdatedAt: now})
	}
	return events, corrections, nil
}

// cleanupResolvedAttempt 仅在 attempt 全部 gap 已解决后删除派生音频。
func (service *ResolutionService) cleanupResolvedAttempt(ctx context.Context, record gaprepository.ConflictRecord) {
	attempt, err := service.repository.GetAttempt(ctx, record.Attempt.ID)
	if err != nil || attempt.State != "completed" || service.extractor.DeleteDerived(record.AudioAsset) != nil {
		return
	}
	_ = service.repository.MarkGapAssetDeleted(ctx, record.AudioAsset.ID, service.clock.Now().UnixMilli())
}

// validateResolutionEdits 要求修改动作逐条覆盖所有冲突 existing，且 use_file_text 与候选一致。
func validateResolutionEdits(command ResolutionCommand, existing []gaprepository.ConflictUtteranceRow, candidates []domaingap.CandidateSegment) error {
	if command.Resolution == domaingap.ResolutionKeepExisting {
		if len(command.Edits) != 0 {
			return fmt.Errorf("保留现有内容不能携带 edit")
		}
		return nil
	}
	if len(existing) == 0 || len(command.Edits) != len(existing) {
		return fmt.Errorf("冲突解决必须逐条提交全部现有转写")
	}
	expected := make(map[string]gaprepository.ConflictUtteranceRow, len(existing))
	for _, row := range existing {
		expected[row.ID] = row
	}
	seen := make(map[string]struct{}, len(command.Edits))
	for _, edit := range command.Edits {
		row, exists := expected[edit.TargetID]
		if !exists || edit.ExpectedRevision != row.TextRevision || strings.TrimSpace(edit.Text) == "" || len(edit.Text) > 10000 || !utf8.ValidString(edit.Text) {
			return fmt.Errorf("冲突 edit 无效")
		}
		if _, duplicate := seen[edit.TargetID]; duplicate {
			return fmt.Errorf("冲突 edit 重复")
		}
		seen[edit.TargetID] = struct{}{}
		if command.Resolution == domaingap.ResolutionUseFileText && edit.Text != fileTextFor(row, candidates) {
			return fmt.Errorf("采用文件文字时 edit 必须等于对应候选")
		}
	}
	return nil
}

// fileTextFor 按候选顺序连接与指定 existing 重叠的文字，不替 UI 猜跨条映射。
func fileTextFor(row gaprepository.ConflictUtteranceRow, candidates []domaingap.CandidateSegment) string {
	parts := make([]string, 0)
	for _, candidate := range candidates {
		if domaingap.HasPositiveOverlap(row.StartSample, row.EndSample, candidate.StartSample, candidate.EndSample) {
			parts = append(parts, candidate.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// nonOverlappingCandidates 返回可以安全补入的候选。
func nonOverlappingCandidates(candidates []domaingap.CandidateSegment, existing []gaprepository.ConflictUtteranceRow) []domaingap.CandidateSegment {
	result := make([]domaingap.CandidateSegment, 0)
	for _, candidate := range candidates {
		overlaps := false
		for _, row := range existing {
			if domaingap.HasPositiveOverlap(candidate.StartSample, candidate.EndSample, row.StartSample, row.EndSample) {
				overlaps = true
				break
			}
		}
		if !overlaps {
			result = append(result, candidate)
		}
	}
	return result
}

// decodeConflictCandidates 只解析本地保存的有限 normalized response。
func decodeConflictCandidates(value *string) ([]domaingap.CandidateSegment, error) {
	if value == nil || len(*value) == 0 || len(*value) > 512*1024 {
		return nil, fmt.Errorf("冲突候选不存在")
	}
	var envelope struct {
		Segments []domaingap.CandidateSegment `json:"segments"`
	}
	if err := json.Unmarshal([]byte(*value), &envelope); err != nil || len(envelope.Segments) == 0 {
		return nil, fmt.Errorf("冲突候选无效")
	}
	return envelope.Segments, nil
}

// validate 校验稳定枚举与必要身份。
func (service *ResolutionService) validate(command ResolutionCommand) error {
	if service == nil || service.repository == nil || service.extractor == nil || service.rawRecord == nil || service.ids == nil || service.clock == nil || command.MeetingID == "" || command.GapID == "" || command.Revision < 0 || !command.Resolution.Valid() || command.RequestID == "" || command.OperatorID == "" {
		return fmt.Errorf("解决补转写冲突：参数无效")
	}
	return nil
}
