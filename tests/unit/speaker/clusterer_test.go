package speaker_test

import (
	"math"
	"testing"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/port"
	speakerservice "meet-sieve/internal/service/speaker"
)

// TestSelectUnknownCluster_RequiresAbsoluteAndMargin 验证 unknown 只有两道门槛均通过才加入已有 cluster。
func TestSelectUnknownCluster_RequiresAbsoluteAndMargin(t *testing.T) {
	candidates := []speakerservice.UnknownClusterCandidate{
		{ID: "cluster-b", DisplayNo: 2, Centroid: encodeFloats(t, []float32{0.8, 0.6})},
		{ID: "cluster-a", DisplayNo: 1, Centroid: encodeFloats(t, []float32{1, 0})},
	}
	decision, err := speakerservice.SelectUnknownCluster(
		port.Embedding{1, 0}, candidates, speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1}, 2,
	)
	if err != nil || decision.State != speakerservice.ClusterJoined || decision.ClusterID != "cluster-a" {
		t.Fatalf("unknown cluster 选择错误：decision=%+v err=%v", decision, err)
	}
	decision, err = speakerservice.SelectUnknownCluster(
		port.Embedding{1, 0}, candidates, speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.3}, 2,
	)
	if err != nil || decision.State != speakerservice.ClusterCreateRequired {
		t.Fatalf("margin 失败必须新建 cluster：decision=%+v err=%v", decision, err)
	}
}

// TestSelectUnknownCluster_HandlesEmptySingleAndTie 验证空候选、单候选绝对阈值和同分稳定 ID。
func TestSelectUnknownCluster_HandlesEmptySingleAndTie(t *testing.T) {
	decision, err := speakerservice.SelectUnknownCluster(port.Embedding{1, 0}, nil, speakerdomain.ScoreThresholds{}, 2)
	if err != nil || decision.State != speakerservice.ClusterCreateRequired {
		t.Fatalf("空 cluster 应新建：decision=%+v err=%v", decision, err)
	}
	single := []speakerservice.UnknownClusterCandidate{{ID: "one", Centroid: encodeFloats(t, []float32{0, 1})}}
	decision, err = speakerservice.SelectUnknownCluster(port.Embedding{1, 0}, single, speakerdomain.ScoreThresholds{MinScore: 0.5, MinMargin: 2}, 2)
	if err != nil || decision.State != speakerservice.ClusterCreateRequired || !decision.SingleCandidate {
		t.Fatalf("单候选绝对阈值错误：decision=%+v err=%v", decision, err)
	}
	tied := []speakerservice.UnknownClusterCandidate{
		{ID: "b", Centroid: encodeFloats(t, []float32{1, 0})},
		{ID: "a", Centroid: encodeFloats(t, []float32{1, 0})},
	}
	decision, err = speakerservice.SelectUnknownCluster(port.Embedding{1, 0}, tied, speakerdomain.ScoreThresholds{MinScore: 0.5, MinMargin: 0.01}, 2)
	if err != nil || decision.State != speakerservice.ClusterCreateRequired || decision.TopClusterID != "a" {
		t.Fatalf("同分必须稳定但不得降低 margin：decision=%+v err=%v", decision, err)
	}
}

// TestRecomputeCentroid_IsStableBySeqAndID 验证输入顺序不影响按 final seq/track ID 求均值后的规范化 centroid。
func TestRecomputeCentroid_IsStableBySeqAndID(t *testing.T) {
	left := []speakerservice.TrackVector{
		{TrackID: "b", FirstFinalSeq: 2, Embedding: port.Embedding{0, 1}},
		{TrackID: "a", FirstFinalSeq: 1, Embedding: port.Embedding{1, 0}},
	}
	right := []speakerservice.TrackVector{left[1], left[0]}
	first, err := speakerservice.RecomputeCentroid(left, 2)
	if err != nil {
		t.Fatalf("计算 centroid 失败：%v", err)
	}
	second, err := speakerservice.RecomputeCentroid(right, 2)
	if err != nil || len(first) != 2 || first[0] != second[0] || first[1] != second[1] {
		t.Fatalf("centroid 不稳定：first=%v second=%v err=%v", first, second, err)
	}
	want := 1 / math.Sqrt(2)
	if math.Abs(float64(first[0])-want) > 1e-6 || math.Abs(float64(first[1])-want) > 1e-6 {
		t.Fatalf("centroid 数值错误：%v", first)
	}
}
