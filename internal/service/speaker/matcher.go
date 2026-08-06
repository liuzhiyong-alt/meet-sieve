package speaker

import (
	"context"
	"fmt"
	"math"
	"sort"

	domain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/port"
	speakerrepository "meet-sieve/internal/repository/speaker"
	voiceservice "meet-sieve/internal/service/voice"
)

// MemberDecisionState 表示自动成员匹配的可解释结果。
type MemberDecisionState string

const (
	// MemberMatched 表示绝对阈值和 margin 均通过。
	MemberMatched MemberDecisionState = "matched"
	// MemberAmbiguous 表示候选存在但至少一道门槛失败。
	MemberAmbiguous MemberDecisionState = "ambiguous"
	// MemberNoCandidates 表示本场没有当前模型可用成员声纹。
	MemberNoCandidates MemberDecisionState = "no_candidates"
)

// MemberDecision 保存可复现的 top-1/top-2 与最终参与者结果。
type MemberDecision struct {
	State            MemberDecisionState
	ParticipantID    string
	TopParticipantID string
	TopScore         float64
	RunnerUpScore    float64
	SingleCandidate  bool
	CandidateCount   int
}

type participantScore struct {
	participantID string
	score         float64
}

// MatchMember 对本场候选执行线性 cosine 比较，每个成员取所有 accepted 样本的最大值。
func MatchMember(
	track port.Embedding,
	candidates []speakerrepository.CandidateEmbedding,
	thresholds domain.ScoreThresholds,
	dimension int,
) (MemberDecision, error) {
	normalizedTrack, err := normalizeEmbedding(track, dimension)
	if err != nil {
		return MemberDecision{}, err
	}
	scores, err := scoreParticipants(normalizedTrack, candidates, dimension)
	if err != nil {
		return MemberDecision{}, err
	}
	if len(scores) == 0 {
		return MemberDecision{State: MemberNoCandidates}, nil
	}
	if len(scores) > 10 {
		return MemberDecision{}, fmt.Errorf("speaker 候选成员超过 10 人")
	}
	sort.Slice(scores, func(left int, right int) bool {
		if scores[left].score == scores[right].score {
			return scores[left].participantID < scores[right].participantID
		}
		return scores[left].score > scores[right].score
	})
	decision := buildMemberDecision(scores)
	absolutePassed := decision.TopScore >= thresholds.MinScore
	marginPassed := decision.SingleCandidate || decision.TopScore-decision.RunnerUpScore >= thresholds.MinMargin
	if absolutePassed && marginPassed {
		decision.State = MemberMatched
		decision.ParticipantID = decision.TopParticipantID
	}
	return decision, nil
}

// scoreParticipants 解码并规范化候选向量，再按 participant 聚合最大 cosine。
func scoreParticipants(track port.Embedding, candidates []speakerrepository.CandidateEmbedding, dimension int) ([]participantScore, error) {
	maximums := map[string]float64{}
	for _, candidate := range candidates {
		if candidate.ParticipantID == "" {
			return nil, fmt.Errorf("speaker 候选 participant ID 为空")
		}
		embedding, err := voiceservice.DecodeEmbeddingBlob(candidate.Embedding, dimension)
		if err != nil {
			return nil, fmt.Errorf("解码 speaker 候选 embedding 失败：%w", err)
		}
		normalized, err := normalizeEmbedding(embedding, dimension)
		if err != nil {
			return nil, fmt.Errorf("规范化 speaker 候选 embedding 失败：%w", err)
		}
		score := dotProduct(track, normalized)
		current, exists := maximums[candidate.ParticipantID]
		if !exists || score > current {
			maximums[candidate.ParticipantID] = score
		}
	}
	result := make([]participantScore, 0, len(maximums))
	for participantID, score := range maximums {
		result = append(result, participantScore{participantID: participantID, score: score})
	}
	return result, nil
}

// buildMemberDecision 构造默认 ambiguous 决策；单候选显式记录 runner 不存在。
func buildMemberDecision(scores []participantScore) MemberDecision {
	decision := MemberDecision{
		State: MemberAmbiguous, TopParticipantID: scores[0].participantID,
		TopScore: scores[0].score, CandidateCount: len(scores), SingleCandidate: len(scores) == 1,
	}
	if len(scores) > 1 {
		decision.RunnerUpScore = scores[1].score
	}
	return decision
}

// EncodeTrack 校验 encoder 模型身份，编码 PCM，并返回有限、定长、L2 规范化 embedding。
func EncodeTrack(ctx context.Context, encoder port.VoiceEncoder, expected domain.ModelIdentity, samples []int16) (port.Embedding, error) {
	if encoder == nil || len(samples) == 0 {
		return nil, fmt.Errorf("speaker encoder 或 PCM 为空")
	}
	info := encoder.ModelInfo()
	if info.ID != expected.ID || info.Version != expected.Version || info.SHA256 != expected.SHA256 || info.Dimension != expected.Dimension {
		return nil, domain.ErrProfileMismatch
	}
	pcm := make([]float32, len(samples))
	for index, sample := range samples {
		pcm[index] = float32(sample) / 32768
	}
	embedding, err := encoder.Encode(ctx, port.AudioPCM{Samples: pcm, SampleRate: speakerSampleRate})
	if err != nil {
		return nil, fmt.Errorf("生成 speaker embedding 失败：%w", err)
	}
	return normalizeEmbedding(embedding, expected.Dimension)
}

// normalizeEmbedding 拒绝维度、NaN/Inf 和零范数，并返回独立 L2 规范化向量。
func normalizeEmbedding(embedding port.Embedding, dimension int) (port.Embedding, error) {
	if dimension <= 0 || len(embedding) != dimension {
		return nil, fmt.Errorf("speaker embedding 维度错误")
	}
	var normSquared float64
	for _, value := range embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, fmt.Errorf("speaker embedding 包含非有限值")
		}
		normSquared += float64(value) * float64(value)
	}
	if normSquared == 0 {
		return nil, fmt.Errorf("speaker embedding 零范数")
	}
	inverseNorm := 1 / math.Sqrt(normSquared)
	result := make(port.Embedding, len(embedding))
	for index, value := range embedding {
		result[index] = float32(float64(value) * inverseNorm)
	}
	return result, nil
}

// dotProduct 计算两个已规范化等长向量的 cosine。
func dotProduct(left port.Embedding, right port.Embedding) float64 {
	var result float64
	for index := range left {
		result += float64(left[index]) * float64(right[index])
	}
	return result
}

// CosineSimilarity 规范化两个 embedding 后计算 cosine，供连续性路由和校准共用。
func CosineSimilarity(left port.Embedding, right port.Embedding, dimension int) (float64, error) {
	normalizedLeft, err := normalizeEmbedding(left, dimension)
	if err != nil {
		return 0, err
	}
	normalizedRight, err := normalizeEmbedding(right, dimension)
	if err != nil {
		return 0, err
	}
	return dotProduct(normalizedLeft, normalizedRight), nil
}
