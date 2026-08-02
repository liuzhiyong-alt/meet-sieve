package voice

import (
	"fmt"
	"math"
)

// QualityCode 是与界面文案分离的稳定音频质量原因。
type QualityCode string

const (
	// QualityCodeNoSpeech 表示样本全零或没有达到有效语音幅度。
	QualityCodeNoSpeech QualityCode = "VOICE_NO_SPEECH"
	// QualityCodeTooShort 表示有效语音时长低于模型 ADR 阈值。
	QualityCodeTooShort QualityCode = "VOICE_TOO_SHORT"
	// QualityCodeTooQuiet 表示整体 RMS 低于模型 ADR 阈值。
	QualityCodeTooQuiet QualityCode = "VOICE_TOO_QUIET"
	// QualityCodeClipped 表示削波样本比例超过模型 ADR 阈值。
	QualityCodeClipped QualityCode = "VOICE_CLIPPED"
)

// QualityThresholds 是必须由模型 ADR 明确提供的内嵌质量阈值。
type QualityThresholds struct {
	MinimumVoiceMS   int64
	SilenceAmplitude int16
	MinimumRMS       float64
	MaximumSilence   float64
	MaximumClipping  float64
}

// QualityMetrics 是可持久化且不包含路径或音频正文的指标 schema。
type QualityMetrics struct {
	SchemaVersion            int     `json:"schema_version"`
	TotalDurationMS          int64   `json:"total_duration_ms"`
	EffectiveVoiceDurationMS int64   `json:"effective_voice_duration_ms"`
	Peak                     float64 `json:"peak"`
	RMS                      float64 `json:"rms"`
	SilenceRatio             float64 `json:"silence_ratio"`
	ClippingRatio            float64 `json:"clipping_ratio"`
	AllZero                  bool    `json:"all_zero"`
}

// QualityAssessment 描述音频阶段是否通过以及可解释指标；不代表 embedding 已可用。
type QualityAssessment struct {
	Passed  bool
	Code    QualityCode
	Metrics QualityMetrics
}

// ProductionQualityThresholds 返回经 CAM++ 官方样本与会前录音交互确认的固定质量门。
func ProductionQualityThresholds() QualityThresholds {
	return QualityThresholds{
		MinimumVoiceMS: 2000, SilenceAmplitude: 328, MinimumRMS: 0.01,
		MaximumSilence: 0.65, MaximumClipping: 0.01,
	}
}

// AnalyzeQuality 使用 ADR 阈值计算并判定规范化单声道 PCM 的音频质量。
func AnalyzeQuality(samples []int16, sampleRate int, thresholds QualityThresholds) (QualityAssessment, error) {
	if err := validateQualityThresholds(thresholds); err != nil {
		return QualityAssessment{}, err
	}
	if sampleRate <= 0 || len(samples) == 0 {
		return QualityAssessment{}, fmt.Errorf("质量分析需要非空 PCM 和有效采样率")
	}
	metrics := calculateQualityMetrics(samples, sampleRate, thresholds.SilenceAmplitude)
	code := assessQualityMetrics(metrics, thresholds)
	return QualityAssessment{Passed: code == "", Code: code, Metrics: metrics}, nil
}

// validateQualityThresholds 阻止未经 ADR 确认的零值或越界阈值被静默使用。
func validateQualityThresholds(thresholds QualityThresholds) error {
	if thresholds.MinimumVoiceMS <= 0 || thresholds.SilenceAmplitude <= 0 || thresholds.MinimumRMS <= 0 {
		return fmt.Errorf("声纹质量阈值尚未完成 ADR 配置")
	}
	if thresholds.MaximumSilence <= 0 || thresholds.MaximumSilence >= 1 || thresholds.MaximumClipping <= 0 || thresholds.MaximumClipping >= 1 {
		return fmt.Errorf("声纹质量比例阈值必须位于 0 到 1 之间")
	}
	return nil
}

// calculateQualityMetrics 单次扫描计算时长、幅度、静音和削波指标。
func calculateQualityMetrics(samples []int16, sampleRate int, silenceAmplitude int16) QualityMetrics {
	var sumSquares float64
	var peak int32
	var effectiveCount int64
	var clippingCount int64
	for _, sample := range samples {
		amplitude := int32(sample)
		if amplitude < 0 {
			amplitude = -amplitude
		}
		if amplitude > peak {
			peak = amplitude
		}
		if amplitude > int32(silenceAmplitude) {
			effectiveCount++
		}
		if amplitude >= math.MaxInt16 {
			clippingCount++
		}
		normalized := float64(sample) / math.MaxInt16
		sumSquares += normalized * normalized
	}
	total := int64(len(samples))
	return QualityMetrics{
		SchemaVersion:            1,
		TotalDurationMS:          total * 1000 / int64(sampleRate),
		EffectiveVoiceDurationMS: effectiveCount * 1000 / int64(sampleRate),
		Peak:                     float64(peak) / math.MaxInt16,
		RMS:                      math.Sqrt(sumSquares / float64(total)),
		SilenceRatio:             float64(total-effectiveCount) / float64(total),
		ClippingRatio:            float64(clippingCount) / float64(total),
		AllZero:                  peak == 0,
	}
}

// assessQualityMetrics 按稳定优先级返回首个用户可处理的质量原因。
func assessQualityMetrics(metrics QualityMetrics, thresholds QualityThresholds) QualityCode {
	if metrics.AllZero || metrics.EffectiveVoiceDurationMS == 0 {
		return QualityCodeNoSpeech
	}
	if metrics.EffectiveVoiceDurationMS < thresholds.MinimumVoiceMS || metrics.SilenceRatio > thresholds.MaximumSilence {
		return QualityCodeTooShort
	}
	if metrics.RMS < thresholds.MinimumRMS {
		return QualityCodeTooQuiet
	}
	if metrics.ClippingRatio > thresholds.MaximumClipping {
		return QualityCodeClipped
	}
	return ""
}
