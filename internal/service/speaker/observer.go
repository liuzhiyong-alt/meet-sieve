package speaker

import (
	"context"
	"fmt"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	speakerrepository "meet-sieve/internal/repository/speaker"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ObserverDependencies 描述 final 提交后创建 speaker 事实所需的短事务边界。
type ObserverDependencies struct {
	Repository   *speakerrepository.Repository
	Transactions *database.TransactionManager
	IDs          identity.Generator
	Clock        clock.Clock
	Queue        chan string
}

// ObserveResult 返回持久 track、幂等与内存唤醒结果。
type ObserveResult struct {
	TrackID           string
	EvidenceID        string
	Duplicate         bool
	Notified          bool
	Skipped           bool
	ProjectionChanged bool
}

// Observer 把已提交 final 幂等归入 session-scoped track，再尝试非阻塞唤醒后台任务。
type Observer struct {
	repository   *speakerrepository.Repository
	transactions *database.TransactionManager
	ids          identity.Generator
	clock        clock.Clock
	queue        chan string
}

// NewObserver 创建 Observer；依赖在 Observe 时统一校验。
func NewObserver(dependencies ObserverDependencies) *Observer {
	return &Observer{
		repository: dependencies.Repository, transactions: dependencies.Transactions,
		ids: dependencies.IDs, clock: dependencies.Clock, queue: dependencies.Queue,
	}
}

// Observe 在 final 已提交后创建 track/evidence；队列满不会回滚 SQLite 事实。
func (observer *Observer) Observe(ctx context.Context, utteranceID string) (ObserveResult, error) {
	if err := validateObserver(observer, utteranceID); err != nil {
		return ObserveResult{}, err
	}
	var result ObserveResult
	err := observer.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return observer.observeInTransaction(ctx, tx, utteranceID, &result)
	})
	if err != nil {
		return ObserveResult{}, fmt.Errorf("持久化 speaker Observe 失败：%w", err)
	}
	if !result.Duplicate && !result.Skipped {
		select {
		case observer.queue <- result.EvidenceID:
			result.Notified = true
		default:
		}
	}
	return result, nil
}

// observeInTransaction 完成幂等检查、track 创建和 evidence 关联。
func (observer *Observer) observeInTransaction(ctx context.Context, tx *gorm.DB, utteranceID string, result *ObserveResult) error {
	existing, err := observer.repository.FindEvidenceByUtterance(ctx, tx, utteranceID)
	if err != nil {
		return err
	}
	if existing != nil {
		*result = ObserveResult{TrackID: existing.SpeakerTrackID, EvidenceID: existing.ID, Duplicate: true}
		return nil
	}
	fact, err := observer.repository.GetObserveFact(ctx, tx, utteranceID)
	if err != nil {
		return err
	}
	track, err := observer.findOrCreateTrack(ctx, tx, fact)
	if err != nil {
		return err
	}
	evidenceID, projectionChanged, err := observer.createEvidence(ctx, tx, fact, track.ID)
	if err != nil {
		return err
	}
	*result = ObserveResult{TrackID: track.ID, EvidenceID: evidenceID, ProjectionChanged: projectionChanged}
	return nil
}

// findOrCreateTrack 按来源选择 provider session/label 或本地 utterance 幂等键。
func (observer *Observer) findOrCreateTrack(ctx context.Context, tx *gorm.DB, fact speakerrepository.ObserveFact) (*models.SpeakerTrack, error) {
	if fact.Utterance.ASRSpeakerLabel != nil && *fact.Utterance.ASRSpeakerLabel != "" {
		return observer.findOrCreateProviderTrack(ctx, tx, fact)
	}
	return observer.findOrCreateLocalTrack(ctx, tx, fact)
}

// findOrCreateProviderTrack 保留 provider session/label 的既有 track 归并语义。
func (observer *Observer) findOrCreateProviderTrack(ctx context.Context, tx *gorm.DB, fact speakerrepository.ObserveFact) (*models.SpeakerTrack, error) {
	label := *fact.Utterance.ASRSpeakerLabel
	track, err := observer.repository.FindTrackBySessionLabel(ctx, tx, fact.Utterance.ASRSessionID, label)
	if err != nil || track != nil {
		return track, err
	}
	segmentNo := 1
	return observer.createTrack(ctx, tx, fact, "provider_label", &label, &segmentNo, nil)
}

// findOrCreateLocalTrack 让无标签 final 以 source_utterance_id 幂等进入本地证据链。
func (observer *Observer) findOrCreateLocalTrack(ctx context.Context, tx *gorm.DB, fact speakerrepository.ObserveFact) (*models.SpeakerTrack, error) {
	track, err := observer.repository.FindTrackBySourceUtterance(ctx, tx, fact.Utterance.ID)
	if err != nil || track != nil {
		return track, err
	}
	utteranceID := fact.Utterance.ID
	return observer.createTrack(ctx, tx, fact, "local_utterance", nil, nil, &utteranceID)
}

// createTrack 分配会议内展示编号，并写入来源专属的幂等字段。
func (observer *Observer) createTrack(
	ctx context.Context,
	tx *gorm.DB,
	fact speakerrepository.ObserveFact,
	source string,
	label *string,
	providerSegmentNo *int,
	sourceUtteranceID *string,
) (*models.SpeakerTrack, error) {
	id, err := observer.newUUID()
	if err != nil {
		return nil, err
	}
	displayNo, err := observer.repository.NextTrackDisplayNo(ctx, tx, fact.Utterance.MeetingID)
	if err != nil {
		return nil, err
	}
	now := observer.clock.Now().UnixMilli()
	track := &models.SpeakerTrack{
		ID: id, MeetingID: fact.Utterance.MeetingID, ASRSessionID: fact.Utterance.ASRSessionID,
		Source: source, ASRSpeakerLabel: label, ProviderSegmentNo: providerSegmentNo, SourceUtteranceID: sourceUtteranceID,
		DisplayNo: displayNo, State: "collecting", RoutingRevision: 1, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := observer.repository.CreateTrack(ctx, tx, *track); err != nil {
		return nil, err
	}
	return track, nil
}

// createEvidence 分配稳定顺序，并在同一事务中更新 utterance 反向关联。
func (observer *Observer) createEvidence(ctx context.Context, tx *gorm.DB, fact speakerrepository.ObserveFact, trackID string) (string, bool, error) {
	order, err := observer.repository.NextEvidenceOrder(ctx, tx, trackID)
	if err != nil {
		return "", false, err
	}
	id, err := observer.newUUID()
	if err != nil {
		return "", false, err
	}
	now := observer.clock.Now().UnixMilli()
	changed, err := observer.repository.AttachEvidence(ctx, tx, models.SpeakerTrackEvidence{
		ID: id, SpeakerTrackID: trackID, UtteranceID: fact.Utterance.ID,
		EvidenceOrder: order, RoutingState: "pending", CreatedAt: now, UpdatedAt: now,
	}, now)
	return id, changed, err
}

// newUUID 只接受生成器给出的 UUID v4，避免不稳定主键进入事实表。
func (observer *Observer) newUUID() (string, error) {
	value := observer.ids.New()
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 4 {
		return "", fmt.Errorf("生成 speaker UUID v4 失败")
	}
	return value, nil
}

// validateObserver 在事务前拒绝缺失依赖和非 UUID utterance。
func validateObserver(observer *Observer, utteranceID string) error {
	if observer == nil || observer.repository == nil || observer.transactions == nil || observer.ids == nil ||
		observer.clock == nil || observer.queue == nil {
		return fmt.Errorf("speaker Observer 依赖不完整")
	}
	if _, err := uuid.Parse(utteranceID); err != nil {
		return fmt.Errorf("speaker Observe utterance ID 无效")
	}
	return nil
}
