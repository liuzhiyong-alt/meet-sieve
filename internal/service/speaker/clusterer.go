package speaker

import (
	"fmt"
	"sort"

	domain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/port"
	voiceservice "meet-sieve/internal/service/voice"
)

// ClusterDecisionState 表示 unknown track 加入已有 cluster 或创建新 cluster。
type ClusterDecisionState string

const (
	// ClusterJoined 表示绝对阈值和 margin 均通过。
	ClusterJoined ClusterDecisionState = "joined"
	// ClusterCreateRequired 表示没有候选或至少一道聚类门槛失败。
	ClusterCreateRequired ClusterDecisionState = "create_required"
)

// UnknownClusterCandidate 是本场未人工分配 cluster 的最小比较事实。
type UnknownClusterCandidate struct {
	ID        string
	DisplayNo int
	Centroid  []byte
}

// ClusterDecision 保存可复现的 cluster top-1/top-2 分数。
type ClusterDecision struct {
	State           ClusterDecisionState
	ClusterID       string
	TopClusterID    string
	TopScore        float64
	RunnerUpScore   float64
	SingleCandidate bool
	CandidateCount  int
}

// TrackVector 是重算 centroid 所需的稳定 track 顺序和 embedding。
type TrackVector struct {
	TrackID       string
	FirstFinalSeq int64
	Embedding     port.Embedding
}

type clusterScore struct {
	id    string
	score float64
}

// SelectUnknownCluster 只比较调用方已限定为本场 unassigned 的 cluster 候选。
func SelectUnknownCluster(
	track port.Embedding,
	candidates []UnknownClusterCandidate,
	thresholds domain.ScoreThresholds,
	dimension int,
) (ClusterDecision, error) {
	normalizedTrack, err := normalizeEmbedding(track, dimension)
	if err != nil {
		return ClusterDecision{}, err
	}
	scores := make([]clusterScore, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ID == "" {
			return ClusterDecision{}, fmt.Errorf("unknown cluster ID 为空")
		}
		centroid, err := voiceservice.DecodeEmbeddingBlob(candidate.Centroid, dimension)
		if err != nil {
			return ClusterDecision{}, fmt.Errorf("解码 unknown centroid 失败：%w", err)
		}
		normalized, err := normalizeEmbedding(centroid, dimension)
		if err != nil {
			return ClusterDecision{}, err
		}
		scores = append(scores, clusterScore{id: candidate.ID, score: dotProduct(normalizedTrack, normalized)})
	}
	if len(scores) == 0 {
		return ClusterDecision{State: ClusterCreateRequired}, nil
	}
	sort.Slice(scores, func(left int, right int) bool {
		if scores[left].score == scores[right].score {
			return scores[left].id < scores[right].id
		}
		return scores[left].score > scores[right].score
	})
	decision := buildClusterDecision(scores)
	absolutePassed := decision.TopScore >= thresholds.MinScore
	marginPassed := decision.SingleCandidate || decision.TopScore-decision.RunnerUpScore >= thresholds.MinMargin
	if absolutePassed && marginPassed {
		decision.State = ClusterJoined
		decision.ClusterID = decision.TopClusterID
	}
	return decision, nil
}

// buildClusterDecision 构造默认需新建的决策，单候选只免除 margin 不免除绝对阈值。
func buildClusterDecision(scores []clusterScore) ClusterDecision {
	decision := ClusterDecision{
		State: ClusterCreateRequired, TopClusterID: scores[0].id, TopScore: scores[0].score,
		SingleCandidate: len(scores) == 1, CandidateCount: len(scores),
	}
	if len(scores) > 1 {
		decision.RunnerUpScore = scores[1].score
	}
	return decision
}

// RecomputeCentroid 按最早 final seq/track ID 求规范化均值，输入顺序不影响结果。
func RecomputeCentroid(tracks []TrackVector, dimension int) (port.Embedding, error) {
	if len(tracks) == 0 {
		return nil, fmt.Errorf("cluster centroid 缺少 track")
	}
	sorted := append([]TrackVector(nil), tracks...)
	sort.Slice(sorted, func(left int, right int) bool {
		if sorted[left].FirstFinalSeq == sorted[right].FirstFinalSeq {
			return sorted[left].TrackID < sorted[right].TrackID
		}
		return sorted[left].FirstFinalSeq < sorted[right].FirstFinalSeq
	})
	sums := make(port.Embedding, dimension)
	for _, track := range sorted {
		normalized, err := normalizeEmbedding(track.Embedding, dimension)
		if err != nil {
			return nil, fmt.Errorf("cluster track embedding 无效：%w", err)
		}
		for index, value := range normalized {
			sums[index] += value
		}
	}
	return normalizeEmbedding(sums, dimension)
}
