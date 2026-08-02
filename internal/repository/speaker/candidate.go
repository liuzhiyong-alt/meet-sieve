package speaker

import (
	"context"
	"fmt"
	"strings"

	domain "meet-sieve/internal/domain/speaker"
)

// CandidateEmbedding 是本场正式参会者的一条当前模型 accepted 声纹候选。
type CandidateEmbedding struct {
	ParticipantID string `gorm:"column:participant_id"`
	MemberID      string `gorm:"column:member_id"`
	VoiceSampleID string `gorm:"column:voice_sample_id"`
	Embedding     []byte `gorm:"column:embedding"`
}

// ListCandidateEmbeddings 只查询本场前十位正式成员的当前模型 accepted embedding。
func (repository *Repository) ListCandidateEmbeddings(
	ctx context.Context,
	meetingID string,
	model domain.ModelIdentity,
) ([]CandidateEmbedding, error) {
	if repository == nil || repository.reader == nil || meetingID == "" || strings.TrimSpace(model.ID) == "" ||
		strings.TrimSpace(model.Version) == "" || len(model.SHA256) != 64 || model.Dimension <= 0 {
		return nil, fmt.Errorf("查询 speaker 候选：参数无效")
	}
	const statement = `WITH meeting_candidates AS (
    SELECT participant.id AS participant_id, participant.member_id, participant.sort_order
    FROM meeting_participants AS participant
    JOIN members AS member ON member.id = participant.member_id
    WHERE participant.meeting_id = ? AND participant.participant_kind = 'member'
      AND member.archived_at IS NULL
    ORDER BY participant.sort_order ASC, participant.id ASC
    LIMIT 10
)
SELECT candidate.participant_id, candidate.member_id, sample.id AS voice_sample_id, embedding.embedding
FROM meeting_candidates AS candidate
JOIN voice_samples AS sample ON sample.member_id = candidate.member_id
JOIN voice_embeddings AS embedding ON embedding.voice_sample_id = sample.id
WHERE sample.processing_state = 'ready' AND sample.quality_state = 'accepted'
  AND embedding.model_id = ? AND embedding.model_version = ?
  AND embedding.model_sha256 = ? AND embedding.dimension = ?
ORDER BY candidate.sort_order ASC, candidate.participant_id ASC, sample.created_at ASC, sample.id ASC`
	var candidates []CandidateEmbedding
	if err := repository.reader.WithContext(ctx).Raw(
		statement, meetingID, model.ID, model.Version, model.SHA256, model.Dimension,
	).Scan(&candidates).Error; err != nil {
		return nil, fmt.Errorf("查询 speaker 候选失败：%w", err)
	}
	return candidates, nil
}
