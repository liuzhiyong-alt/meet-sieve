package calibration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/port"
	speakerrepository "meet-sieve/internal/repository/speaker"
	speakerservice "meet-sieve/internal/service/speaker"
	voiceservice "meet-sieve/internal/service/voice"
)

// SampleResult 保存报告所需的非原始音频事实。
type SampleResult struct {
	SpeakerID     string  `json:"speaker_id"`
	SessionID     string  `json:"session_id"`
	Role          string  `json:"role"`
	SHA256        string  `json:"sha256"`
	DurationMS    int64   `json:"duration_ms"`
	TopSpeakerID  string  `json:"top_speaker_id,omitempty"`
	TopScore      float64 `json:"top_score,omitempty"`
	RunnerUpScore float64 `json:"runner_up_score,omitempty"`
	Matched       bool    `json:"matched,omitempty"`
}

// Metrics 汇总正式档案的可复现质量门禁结果。
type Metrics struct {
	SpeakerCount        int `json:"speaker_count"`
	EnrollmentCount     int `json:"enrollment_count"`
	EvaluationCount     int `json:"evaluation_count"`
	IdentityCorrect     int `json:"identity_correct"`
	IdentityFalseAccept int `json:"identity_false_accept"`
	IdentityFalseReject int `json:"identity_false_reject"`
	ClusterFalseMerge   int `json:"cluster_false_merge"`
	ClusterFalseSplit   int `json:"cluster_false_split"`
}

// Result 是校准成功后可写入仓库的档案和审计事实。
type Result struct {
	Profile speakerdomain.MatchingProfile `json:"profile"`
	Metrics Metrics                       `json:"metrics"`
	Samples []SampleResult                `json:"samples"`
}

type encodedSample struct {
	definition Sample
	embedding  port.Embedding
	result     SampleResult
}

type simulatedCluster struct {
	id        string
	speakerID string
	tracks    []speakerservice.TrackVector
}

// Run 使用生产 encoder、成员 matcher 和匿名 clusterer 执行真实校准。
func Run(ctx context.Context, manifest Manifest, manifestDir string, encoder port.VoiceEncoder) (Result, error) {
	if err := manifest.Validate(); err != nil {
		return Result{}, err
	}
	if encoder == nil {
		return Result{}, fmt.Errorf("校准缺少 VoiceEncoder")
	}
	model := modelIdentity(encoder.ModelInfo())
	encoded, err := encodeSamples(ctx, manifest, manifestDir, encoder, model)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Profile: speakerdomain.MatchingProfile{
			SchemaVersion: 1, ProfileID: manifest.ProfileID, Model: model, Evidence: manifest.Evidence,
			Identity: manifest.Identity, UnknownCluster: manifest.UnknownCluster,
			CalibrationRecord: manifest.CalibrationRecord,
		},
		Metrics: Metrics{SpeakerCount: len(manifest.SpeakerIDs())},
	}
	candidates, evaluations, err := splitSamples(encoded, model.Dimension)
	if err != nil {
		return Result{}, err
	}
	result.Metrics.EnrollmentCount = len(candidates)
	result.Metrics.EvaluationCount = len(evaluations)
	identityResults := evaluateIdentity(&result, evaluations, candidates, manifest.Identity, model.Dimension)
	if err := evaluateClusters(&result, evaluations, manifest.UnknownCluster, model.Dimension); err != nil {
		return Result{}, err
	}
	for _, sample := range encoded {
		if updated, exists := identityResults[sample.result.SHA256]; exists {
			result.Samples = append(result.Samples, updated)
		} else {
			result.Samples = append(result.Samples, sample.result)
		}
	}
	if err := validateQualityGate(result.Metrics); err != nil {
		return result, err
	}
	return result, nil
}

// encodeSamples 读取、规范化并编码全部真实 WAV；评估音频按目标证据时长截断。
func encodeSamples(ctx context.Context, manifest Manifest, manifestDir string, encoder port.VoiceEncoder, model speakerdomain.ModelIdentity) ([]encodedSample, error) {
	result := make([]encodedSample, 0, len(manifest.Samples))
	for _, sample := range manifest.Samples {
		path := sample.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(manifestDir, path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取校准音频 %s 失败：%w", sample.Path, err)
		}
		normalized, err := voiceservice.NormalizeWAV(ctx, content)
		if err != nil {
			return nil, fmt.Errorf("规范化校准音频 %s 失败：%w", sample.Path, err)
		}
		if normalized.DurationMS < int64(manifest.Evidence.MinEvidenceMS) {
			return nil, fmt.Errorf("校准音频 %s 短于 min_evidence_ms", sample.Path)
		}
		samples := normalized.Samples
		if sample.Role == RoleEvaluation {
			limit := manifest.Evidence.TargetEvidenceMS * normalized.SampleRate / 1000
			if len(samples) > limit {
				samples = samples[:limit]
			}
		}
		embedding, err := speakerservice.EncodeTrack(ctx, encoder, model, samples)
		if err != nil {
			return nil, fmt.Errorf("编码校准音频 %s 失败：%w", sample.Path, err)
		}
		digest := sha256.Sum256(normalized.WAV)
		result = append(result, encodedSample{
			definition: sample, embedding: embedding,
			result: SampleResult{SpeakerID: sample.SpeakerID, SessionID: sample.SessionID, Role: string(sample.Role),
				SHA256: hex.EncodeToString(digest[:]), DurationMS: normalized.DurationMS},
		})
	}
	if err := rejectDuplicateAudio(result); err != nil {
		return nil, err
	}
	return result, nil
}

// splitSamples 构造与生产成员匹配器一致的逐样本候选集合。
func splitSamples(samples []encodedSample, dimension int) ([]speakerrepository.CandidateEmbedding, []encodedSample, error) {
	var candidates []speakerrepository.CandidateEmbedding
	var evaluations []encodedSample
	for _, sample := range samples {
		if sample.definition.Role == RoleEvaluation {
			evaluations = append(evaluations, sample)
			continue
		}
		blob, err := voiceservice.EncodeEmbeddingBlob(sample.embedding, dimension)
		if err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, speakerrepository.CandidateEmbedding{
			ParticipantID: sample.definition.SpeakerID, Embedding: blob,
		})
	}
	return candidates, evaluations, nil
}

// evaluateIdentity 按生产 matcher 统计识别正确、误认和拒识。
func evaluateIdentity(result *Result, evaluations []encodedSample, candidates []speakerrepository.CandidateEmbedding, thresholds speakerdomain.ScoreThresholds, dimension int) map[string]SampleResult {
	for index := range evaluations {
		decision, err := speakerservice.MatchMember(evaluations[index].embedding, candidates, thresholds, dimension)
		if err != nil {
			result.Metrics.IdentityFalseReject++
			continue
		}
		evaluations[index].result.TopSpeakerID = decision.TopParticipantID
		evaluations[index].result.TopScore = decision.TopScore
		evaluations[index].result.RunnerUpScore = decision.RunnerUpScore
		if decision.State != speakerservice.MemberMatched {
			result.Metrics.IdentityFalseReject++
			continue
		}
		if decision.ParticipantID != evaluations[index].definition.SpeakerID {
			result.Metrics.IdentityFalseAccept++
			continue
		}
		evaluations[index].result.Matched = true
		result.Metrics.IdentityCorrect++
		// 移除真实成员后再次匹配，验证未参会说话人不会被误认成候选成员。
		unknownDecision, err := speakerservice.MatchMember(
			evaluations[index].embedding,
			withoutParticipant(candidates, evaluations[index].definition.SpeakerID),
			thresholds,
			dimension,
		)
		if err != nil || unknownDecision.State == speakerservice.MemberMatched {
			result.Metrics.IdentityFalseAccept++
		}
	}
	byKey := map[string]SampleResult{}
	for _, sample := range evaluations {
		byKey[sample.result.SHA256] = sample.result
	}
	return byKey
}

// withoutParticipant 构造 out-of-set 场景，模拟真实说话人不在本场候选成员中。
func withoutParticipant(candidates []speakerrepository.CandidateEmbedding, participantID string) []speakerrepository.CandidateEmbedding {
	result := make([]speakerrepository.CandidateEmbedding, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.ParticipantID != participantID {
			result = append(result, candidate)
		}
	}
	return result
}

// evaluateClusters 按生产 clusterer 顺序模拟匿名说话人归并。
func evaluateClusters(result *Result, evaluations []encodedSample, thresholds speakerdomain.ScoreThresholds, dimension int) error {
	clusters := make([]simulatedCluster, 0)
	for index, sample := range evaluations {
		candidates := make([]speakerservice.UnknownClusterCandidate, 0, len(clusters))
		for _, cluster := range clusters {
			centroid, err := speakerservice.RecomputeCentroid(cluster.tracks, dimension)
			if err != nil {
				return err
			}
			blob, err := voiceservice.EncodeEmbeddingBlob(centroid, dimension)
			if err != nil {
				return err
			}
			candidates = append(candidates, speakerservice.UnknownClusterCandidate{ID: cluster.id, Centroid: blob})
		}
		decision, err := speakerservice.SelectUnknownCluster(sample.embedding, candidates, thresholds, dimension)
		if err != nil {
			return err
		}
		track := speakerservice.TrackVector{TrackID: fmt.Sprintf("eval-%04d", index+1), FirstFinalSeq: int64(index + 1), Embedding: sample.embedding}
		if decision.State == speakerservice.ClusterJoined {
			clusterIndex := findCluster(clusters, decision.ClusterID)
			if clusterIndex < 0 {
				return fmt.Errorf("校准聚类返回不存在的 cluster")
			}
			if clusters[clusterIndex].speakerID != sample.definition.SpeakerID {
				result.Metrics.ClusterFalseMerge++
			}
			clusters[clusterIndex].tracks = append(clusters[clusterIndex].tracks, track)
			continue
		}
		if hasSpeakerCluster(clusters, sample.definition.SpeakerID) {
			result.Metrics.ClusterFalseSplit++
		}
		clusters = append(clusters, simulatedCluster{
			id: fmt.Sprintf("cluster-%04d", len(clusters)+1), speakerID: sample.definition.SpeakerID,
			tracks: []speakerservice.TrackVector{track},
		})
	}
	return nil
}

// validateQualityGate 拒绝任何误认、拒识、误合并或误拆分的阈值组合。
func validateQualityGate(metrics Metrics) error {
	if metrics.IdentityCorrect != metrics.EvaluationCount || metrics.IdentityFalseAccept != 0 || metrics.IdentityFalseReject != 0 {
		return fmt.Errorf("成员识别校准未通过：correct=%d evaluation=%d false_accept=%d false_reject=%d",
			metrics.IdentityCorrect, metrics.EvaluationCount, metrics.IdentityFalseAccept, metrics.IdentityFalseReject)
	}
	if metrics.ClusterFalseMerge != 0 || metrics.ClusterFalseSplit != 0 {
		return fmt.Errorf("匿名聚类校准未通过：false_merge=%d false_split=%d", metrics.ClusterFalseMerge, metrics.ClusterFalseSplit)
	}
	return nil
}

// rejectDuplicateAudio 基于规范化音频摘要阻止同一内容跨集合复用。
func rejectDuplicateAudio(samples []encodedSample) error {
	seen := map[string]Sample{}
	for _, sample := range samples {
		if previous, exists := seen[sample.result.SHA256]; exists {
			return fmt.Errorf("校准音频内容重复：%s 与 %s", previous.Path, sample.definition.Path)
		}
		seen[sample.result.SHA256] = sample.definition
	}
	return nil
}

// modelIdentity 将 encoder 身份转换为档案绑定的模型四元组。
func modelIdentity(info port.ModelInfo) speakerdomain.ModelIdentity {
	return speakerdomain.ModelIdentity{ID: info.ID, Version: info.Version, SHA256: info.SHA256, Dimension: info.Dimension}
}

// findCluster 返回稳定 cluster ID 对应的数组位置。
func findCluster(clusters []simulatedCluster, id string) int {
	for index := range clusters {
		if clusters[index].id == id {
			return index
		}
	}
	return -1
}

// hasSpeakerCluster 判断当前模拟状态是否已经有该真实说话人的 cluster。
func hasSpeakerCluster(clusters []simulatedCluster, speakerID string) bool {
	for _, cluster := range clusters {
		if cluster.speakerID == speakerID {
			return true
		}
	}
	return false
}
