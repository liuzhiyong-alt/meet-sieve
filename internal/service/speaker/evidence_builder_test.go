package speaker

import (
	"context"
	"testing"
)

type evidenceRangeReader struct{}

// Read 返回与请求范围等长的 PCM，便于精确验证累计时长。
func (evidenceRangeReader) Read(_ context.Context, _ string, startSample int64, endSample int64) ([]int16, error) {
	return make([]int16, endSample-startSample), nil
}

// TestEvidenceBuilderAccumulatesShortUtterancesByProviderTrack 验证单句都不足 3 秒时仍可按同一 Speaker 轨道累计。
func TestEvidenceBuilderAccumulatesShortUtterancesByProviderTrack(t *testing.T) {
	utterances := []EvidenceUtterance{
		{ID: "first", ASRSessionID: "session", SpeakerLabel: "speaker_0", FinalSeq: 1, StartSample: 0, EndSample: 32_000},
		{ID: "other", ASRSessionID: "session", SpeakerLabel: "speaker_1", FinalSeq: 2, StartSample: 32_000, EndSample: 64_000},
		{ID: "second", ASRSessionID: "session", SpeakerLabel: "speaker_0", FinalSeq: 3, StartSample: 64_000, EndSample: 96_000},
		{ID: "third", ASRSessionID: "session", SpeakerLabel: "speaker_0", FinalSeq: 4, StartSample: 96_000, EndSample: 160_000},
	}
	result, err := NewEvidenceBuilder(evidenceRangeReader{}).Build(
		context.Background(), "meeting", "session", "speaker_0", "", utterances, 3_000, 8_000, false,
	)
	if err != nil {
		t.Fatalf("构造短句累计证据失败：%v", err)
	}
	if result.State != EvidenceReady || result.DurationMS != 8_000 || len(result.Items) != 3 {
		t.Fatalf("同轨短句未正确累计：%+v", result)
	}
	for _, item := range result.Items {
		if !item.Included || item.OverlapRisk {
			t.Fatalf("同轨短句证据状态错误：%+v", item)
		}
	}
}

// TestEvidenceBuilderUsesMinimumEvidenceOnlyWhenFinalizing 验证 3～8 秒证据只在会议收尾时进入识别。
func TestEvidenceBuilderUsesMinimumEvidenceOnlyWhenFinalizing(t *testing.T) {
	utterances := []EvidenceUtterance{{
		ID: "short", ASRSessionID: "session", SpeakerLabel: "speaker_0",
		FinalSeq: 1, StartSample: 0, EndSample: 64_000,
	}}
	builder := NewEvidenceBuilder(evidenceRangeReader{})
	realtime, err := builder.Build(context.Background(), "meeting", "session", "speaker_0", "", utterances, 3_000, 8_000, false)
	if err != nil || realtime.State != EvidenceCollecting {
		t.Fatalf("实时阶段应继续累计：result=%+v err=%v", realtime, err)
	}
	finalized, err := builder.Build(context.Background(), "meeting", "session", "speaker_0", "", utterances, 3_000, 8_000, true)
	if err != nil || finalized.State != EvidenceReady || finalized.DurationMS != 4_000 {
		t.Fatalf("收尾阶段应使用最小证据：result=%+v err=%v", finalized, err)
	}
}
