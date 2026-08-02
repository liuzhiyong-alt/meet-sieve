package voice_test

import (
	"encoding/json"
	"testing"

	voiceservice "meet-sieve/internal/service/voice"
)

// TestAnalyzeQuality_RejectsAllZero 验证全零音频使用稳定质量 code 拒绝。
func TestAnalyzeQuality_RejectsAllZero(t *testing.T) {
	assessment, err := voiceservice.AnalyzeQuality(make([]int16, 16000), 16000, testQualityThresholds())
	if err != nil {
		t.Fatalf("分析全零音频失败：%v", err)
	}
	if assessment.Passed || assessment.Code != voiceservice.QualityCodeNoSpeech || !assessment.Metrics.AllZero {
		t.Fatalf("全零质量结论不正确：%+v", assessment)
	}
}

// TestAnalyzeQuality_RequiresValidatedThresholds 验证未由 ADR 提供的零值阈值不能进入生产判定。
func TestAnalyzeQuality_RequiresValidatedThresholds(t *testing.T) {
	if _, err := voiceservice.AnalyzeQuality([]int16{1}, 16000, voiceservice.QualityThresholds{}); err == nil {
		t.Fatal("期望拒绝未配置的质量阈值")
	}
}

// TestAnalyzeQuality_ProducesStableMetricsJSON 验证质量指标可稳定序列化且不包含路径或音频正文。
func TestAnalyzeQuality_ProducesStableMetricsJSON(t *testing.T) {
	samples := make([]int16, 16000)
	for index := 0; index < 8000; index++ {
		samples[index] = 2000
	}
	assessment, err := voiceservice.AnalyzeQuality(samples, 16000, testQualityThresholds())
	if err != nil {
		t.Fatalf("分析音频失败：%v", err)
	}
	encoded, err := json.Marshal(assessment.Metrics)
	if err != nil {
		t.Fatalf("序列化质量指标失败：%v", err)
	}
	want := `{"schema_version":1,"total_duration_ms":1000,"effective_voice_duration_ms":500,"peak":0.061037018951994385,"rms":0.04315969000436973,"silence_ratio":0.5,"clipping_ratio":0,"all_zero":false}`
	if string(encoded) != want {
		t.Fatalf("质量指标 JSON 不稳定：\ngot  %s\nwant %s", encoded, want)
	}
}

// testQualityThresholds 返回仅供单元测试使用的显式阈值，不作为生产默认值。
func testQualityThresholds() voiceservice.QualityThresholds {
	return voiceservice.QualityThresholds{
		MinimumVoiceMS:   200,
		SilenceAmplitude: 100,
		MinimumRMS:       0.001,
		MaximumSilence:   0.9,
		MaximumClipping:  0.1,
	}
}
