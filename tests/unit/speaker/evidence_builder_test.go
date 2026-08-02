package speaker_test

import (
	"context"
	"errors"
	"testing"

	speakerservice "meet-sieve/internal/service/speaker"
)

type deterministicAudioReader struct {
	pending bool
	reads   int
}

// Read 返回值等于全局采样点的确定性 PCM，便于验证拼接不补静音。
func (reader *deterministicAudioReader) Read(_ context.Context, _ string, start int64, end int64) ([]int16, error) {
	reader.reads++
	if reader.pending {
		return nil, speakerservice.ErrAudioEvidencePending
	}
	result := make([]int16, end-start)
	for index := range result {
		result[index] = int16(start + int64(index))
	}
	return result, nil
}

// TestEvidenceBuilder_SortsConcatenatesAndCapsTarget 验证按 final seq 拼接、不补静音并精确限制 target。
func TestEvidenceBuilder_SortsConcatenatesAndCapsTarget(t *testing.T) {
	reader := &deterministicAudioReader{}
	builder := speakerservice.NewEvidenceBuilder(reader)
	utterances := []speakerservice.EvidenceUtterance{
		{ID: "second", ASRSessionID: "session", SpeakerLabel: "speaker-1", FinalSeq: 2, StartSample: 32, EndSample: 64},
		{ID: "first", ASRSessionID: "session", SpeakerLabel: "speaker-1", FinalSeq: 1, StartSample: 0, EndSample: 32},
	}
	result, err := builder.Build(context.Background(), "meeting", "session", "speaker-1", utterances, 1, 3, false)
	if err != nil {
		t.Fatalf("构造证据失败：%v", err)
	}
	if result.State != speakerservice.EvidenceReady || len(result.Samples) != 48 || result.Samples[31] != 31 || result.Samples[32] != 32 || result.Samples[47] != 47 {
		t.Fatalf("证据顺序或 target 上限错误：state=%s len=%d samples=%v", result.State, len(result.Samples), result.Samples)
	}
	if len(result.Items) != 2 || result.Items[1].UsedEndSample != 48 {
		t.Fatalf("部分使用的 utterance 范围未追溯：%+v", result.Items)
	}
}

// TestEvidenceBuilder_ExcludesPositiveOverlapAcrossLabels 验证同 session 不同 label 正长度重叠不进入 embedding。
func TestEvidenceBuilder_ExcludesPositiveOverlapAcrossLabels(t *testing.T) {
	reader := &deterministicAudioReader{}
	builder := speakerservice.NewEvidenceBuilder(reader)
	utterances := []speakerservice.EvidenceUtterance{
		{ID: "overlap", ASRSessionID: "session", SpeakerLabel: "speaker-1", FinalSeq: 1, StartSample: 0, EndSample: 32},
		{ID: "other", ASRSessionID: "session", SpeakerLabel: "speaker-2", FinalSeq: 2, StartSample: 16, EndSample: 48},
		{ID: "safe", ASRSessionID: "session", SpeakerLabel: "speaker-1", FinalSeq: 3, StartSample: 48, EndSample: 80},
	}
	result, err := builder.Build(context.Background(), "meeting", "session", "speaker-1", utterances, 1, 2, true)
	if err != nil {
		t.Fatalf("构造重叠证据失败：%v", err)
	}
	if len(result.Items) != 2 || !result.Items[0].OverlapRisk || result.Items[0].Included || result.Items[0].ExcludedReason != "overlap_risk" {
		t.Fatalf("重叠排除记录错误：%+v", result.Items)
	}
	if reader.reads != 1 || len(result.Samples) != 32 || result.Samples[0] != 48 {
		t.Fatalf("重叠片段不得读取或进入样本：reads=%d samples=%v", reader.reads, result.Samples)
	}
}

// TestEvidenceBuilder_DistinguishesPendingAndInsufficient 验证音频未 ready 可重试，收尾证据不足不调用后续 encoder。
func TestEvidenceBuilder_DistinguishesPendingAndInsufficient(t *testing.T) {
	utterances := []speakerservice.EvidenceUtterance{
		{ID: "one", ASRSessionID: "session", SpeakerLabel: "speaker-1", FinalSeq: 1, StartSample: 0, EndSample: 16},
	}
	pendingReader := &deterministicAudioReader{pending: true}
	result, err := speakerservice.NewEvidenceBuilder(pendingReader).Build(
		context.Background(), "meeting", "session", "speaker-1", utterances, 1, 2, false,
	)
	if err != nil || result.State != speakerservice.EvidencePending {
		t.Fatalf("音频未 ready 状态错误：result=%+v err=%v", result, err)
	}
	readyReader := &deterministicAudioReader{}
	result, err = speakerservice.NewEvidenceBuilder(readyReader).Build(
		context.Background(), "meeting", "session", "speaker-1", utterances, 2, 3, true,
	)
	if err != nil || result.State != speakerservice.EvidenceInsufficient {
		t.Fatalf("收尾证据不足状态错误：result=%+v err=%v", result, err)
	}
}

// TestEvidenceBuilder_PropagatesUnsafeAudio 验证不把不安全资产降级为 pending 后无限重试。
func TestEvidenceBuilder_PropagatesUnsafeAudio(t *testing.T) {
	reader := failingEvidenceReader{err: speakerservice.ErrAudioAssetUnsafe}
	_, err := speakerservice.NewEvidenceBuilder(reader).Build(context.Background(), "meeting", "session", "speaker-1", []speakerservice.EvidenceUtterance{
		{ID: "one", ASRSessionID: "session", SpeakerLabel: "speaker-1", FinalSeq: 1, StartSample: 0, EndSample: 16},
	}, 1, 1, true)
	if !errors.Is(err, speakerservice.ErrAudioAssetUnsafe) {
		t.Fatalf("不安全音频错误不得被吞掉：%v", err)
	}
}

type failingEvidenceReader struct{ err error }

// Read 返回固定失败，验证 builder 的错误分类。
func (reader failingEvidenceReader) Read(context.Context, string, int64, int64) ([]int16, error) {
	return nil, reader.err
}
