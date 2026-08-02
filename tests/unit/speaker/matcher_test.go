package speaker_test

import (
	"context"
	"math"
	"testing"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/port"
	speakerrepository "meet-sieve/internal/repository/speaker"
	speakerservice "meet-sieve/internal/service/speaker"
)

// TestMatcher_TakesMemberMaxAndRequiresAbsoluteAndMargin 验证成员多样本取最大值且两道门槛同时成立。
func TestMatcher_TakesMemberMaxAndRequiresAbsoluteAndMargin(t *testing.T) {
	candidates := []speakerrepository.CandidateEmbedding{
		{ParticipantID: "p1", VoiceSampleID: "s1", Embedding: encodeFloats(t, []float32{0, 1})},
		{ParticipantID: "p1", VoiceSampleID: "s2", Embedding: encodeFloats(t, []float32{1, 0})},
		{ParticipantID: "p2", VoiceSampleID: "s3", Embedding: encodeFloats(t, []float32{0.8, 0.6})},
	}
	decision, err := speakerservice.MatchMember(
		port.Embedding{1, 0}, candidates, speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.1}, 2,
	)
	if err != nil || decision.State != speakerservice.MemberMatched || decision.ParticipantID != "p1" {
		t.Fatalf("成员匹配错误：decision=%+v err=%v", decision, err)
	}
	if math.Abs(decision.TopScore-1) > 1e-6 || math.Abs(decision.RunnerUpScore-0.8) > 1e-6 {
		t.Fatalf("top 分数错误：%+v", decision)
	}
	decision, err = speakerservice.MatchMember(
		port.Embedding{1, 0}, candidates, speakerdomain.ScoreThresholds{MinScore: 0.9, MinMargin: 0.3}, 2,
	)
	if err != nil || decision.State != speakerservice.MemberAmbiguous {
		t.Fatalf("margin 失败必须拒识：decision=%+v err=%v", decision, err)
	}
}

// TestMatcher_HandlesSingleTieAndNoCandidates 验证单候选仍查绝对阈值、同分不降 margin、无候选显式拒识。
func TestMatcher_HandlesSingleTieAndNoCandidates(t *testing.T) {
	single := []speakerrepository.CandidateEmbedding{{ParticipantID: "p1", Embedding: encodeFloats(t, []float32{1, 0})}}
	decision, err := speakerservice.MatchMember(port.Embedding{0, 1}, single, speakerdomain.ScoreThresholds{MinScore: 0.5, MinMargin: 2}, 2)
	if err != nil || decision.State != speakerservice.MemberAmbiguous || !decision.SingleCandidate {
		t.Fatalf("单候选绝对阈值语义错误：decision=%+v err=%v", decision, err)
	}
	tied := []speakerrepository.CandidateEmbedding{
		{ParticipantID: "p2", Embedding: encodeFloats(t, []float32{1, 0})},
		{ParticipantID: "p1", Embedding: encodeFloats(t, []float32{1, 0})},
	}
	decision, err = speakerservice.MatchMember(port.Embedding{1, 0}, tied, speakerdomain.ScoreThresholds{MinScore: 0.5, MinMargin: 0.01}, 2)
	if err != nil || decision.State != speakerservice.MemberAmbiguous || decision.TopParticipantID != "p1" || decision.RunnerUpScore != 1 {
		t.Fatalf("同分稳定排序或 margin 错误：decision=%+v err=%v", decision, err)
	}
	decision, err = speakerservice.MatchMember(port.Embedding{1, 0}, nil, speakerdomain.ScoreThresholds{}, 2)
	if err != nil || decision.State != speakerservice.MemberNoCandidates {
		t.Fatalf("无候选状态错误：decision=%+v err=%v", decision, err)
	}
}

// TestMatcher_RejectsInvalidEmbeddings 验证维度、零范数和非有限数不会产生分数。
func TestMatcher_RejectsInvalidEmbeddings(t *testing.T) {
	invalid := []port.Embedding{{1}, {0, 0}, {float32(math.NaN()), 0}, {float32(math.Inf(1)), 0}}
	for _, embedding := range invalid {
		if _, err := speakerservice.MatchMember(embedding, nil, speakerdomain.ScoreThresholds{}, 2); err == nil {
			t.Fatalf("非法 track embedding 必须拒绝：%v", embedding)
		}
	}
	candidate := []speakerrepository.CandidateEmbedding{{ParticipantID: "p1", Embedding: []byte{1}}}
	if _, err := speakerservice.MatchMember(port.Embedding{1, 0}, candidate, speakerdomain.ScoreThresholds{}, 2); err == nil {
		t.Fatal("非法候选 embedding 必须拒绝")
	}
}

// TestEncodeTrack_ValidatesModelBeforeCallingEncoder 验证模型四元组不匹配时不执行推理。
func TestEncodeTrack_ValidatesModelBeforeCallingEncoder(t *testing.T) {
	encoder := &fixedVoiceEncoder{info: port.ModelInfo{ID: expectedModel.ID, Version: expectedModel.Version, SHA256: expectedModel.SHA256, Dimension: 2}, embedding: port.Embedding{3, 4}}
	model := expectedModel
	model.Dimension = 2
	embedding, err := speakerservice.EncodeTrack(context.Background(), encoder, model, []int16{0, 32767})
	if err != nil || encoder.calls != 1 || math.Abs(float64(embedding[0])-0.6) > 1e-6 || math.Abs(float64(embedding[1])-0.8) > 1e-6 {
		t.Fatalf("真实 encoder port 接入错误：embedding=%v calls=%d err=%v", embedding, encoder.calls, err)
	}
	model.Version = "other"
	if _, err := speakerservice.EncodeTrack(context.Background(), encoder, model, []int16{1, 2}); err == nil || encoder.calls != 1 {
		t.Fatalf("模型不匹配不得调用 encoder：calls=%d err=%v", encoder.calls, err)
	}
}

type fixedVoiceEncoder struct {
	info      port.ModelInfo
	embedding port.Embedding
	calls     int
}

// Encode 返回固定 embedding 并记录调用次数。
func (encoder *fixedVoiceEncoder) Encode(context.Context, port.AudioPCM) (port.Embedding, error) {
	encoder.calls++
	return append(port.Embedding(nil), encoder.embedding...), nil
}

// ModelInfo 返回测试固定模型身份。
func (encoder *fixedVoiceEncoder) ModelInfo() port.ModelInfo { return encoder.info }

// encodeFloats 把小向量编码成数据库小端 float32 BLOB。
func encodeFloats(t *testing.T, values []float32) []byte {
	t.Helper()
	result := make([]byte, len(values)*4)
	for index, value := range values {
		bits := math.Float32bits(value)
		result[index*4] = byte(bits)
		result[index*4+1] = byte(bits >> 8)
		result[index*4+2] = byte(bits >> 16)
		result[index*4+3] = byte(bits >> 24)
	}
	return result
}
