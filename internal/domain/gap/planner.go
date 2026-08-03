package gap

import (
	"fmt"
	"sort"
)

const (
	// DefaultSampleRate 是 MeetSieve 统一音频时间线采样率。
	DefaultSampleRate = 16000
	// DefaultContextSamples 是补转写切片单侧最多保留的 500 ms 上下文。
	DefaultContextSamples = 8000
	// DefaultMaxDurationSamples 是单请求最多包含的 9 分钟核心音频。
	DefaultMaxDurationSamples = 9 * 60 * DefaultSampleRate
	// DefaultMaxWAVBytes 为 Base64 前本地 WAV 的 18 MiB 安全上限。
	DefaultMaxWAVBytes = 18 * 1024 * 1024
)

// Range 描述标准音频时间线上的半开 gap 区间。
type Range struct {
	ID          string
	StartSample int64
	EndSample   int64
}

// PlanInput 描述生成补转写切片计划所需的固定录音边界。
type PlanInput struct {
	Gaps         []Range
	RecordingEnd int64
	SampleRate   int
	MaxWAVBytes  int64
	PCM          []int16
}

// Slice 描述一次 provider 请求的核心范围和带上下文音频范围。
type Slice struct {
	GapIDs           []string
	CoreStartSample  int64
	CoreEndSample    int64
	AudioStartSample int64
	AudioEndSample   int64
}

// Plan 把 gap 转换为排序、合并、拆分且有界的文件识别切片。
func Plan(input PlanInput) ([]Slice, error) {
	if err := validatePlanInput(input); err != nil {
		return nil, err
	}
	merged := mergeRanges(input.Gaps)
	maxAudioSamples, err := maxAudioSamples(input.SampleRate, input.MaxWAVBytes)
	if err != nil {
		return nil, err
	}
	result := make([]Slice, 0, len(merged))
	for _, item := range merged {
		result = append(result, splitRange(item, input, maxAudioSamples)...)
	}
	return result, nil
}

type mergedRange struct {
	ids   []string
	start int64
	end   int64
}

// validatePlanInput 拒绝不能安全映射到本地录音的范围。
func validatePlanInput(input PlanInput) error {
	if input.RecordingEnd <= 0 || input.SampleRate != DefaultSampleRate || len(input.Gaps) == 0 {
		return fmt.Errorf("gap 计划输入无效")
	}
	if len(input.PCM) > 0 && int64(len(input.PCM)) < input.RecordingEnd {
		return fmt.Errorf("gap 计划 PCM 不完整")
	}
	for _, item := range input.Gaps {
		if item.ID == "" || item.StartSample < 0 || item.EndSample <= item.StartSample || item.EndSample > input.RecordingEnd {
			return fmt.Errorf("gap 范围无效")
		}
	}
	return nil
}

// mergeRanges 只合并重叠或首尾相接的 gap，并固定稳定排序。
func mergeRanges(ranges []Range) []mergedRange {
	ordered := append([]Range(nil), ranges...)
	sort.Slice(ordered, func(left int, right int) bool {
		if ordered[left].StartSample != ordered[right].StartSample {
			return ordered[left].StartSample < ordered[right].StartSample
		}
		if ordered[left].EndSample != ordered[right].EndSample {
			return ordered[left].EndSample < ordered[right].EndSample
		}
		return ordered[left].ID < ordered[right].ID
	})
	merged := make([]mergedRange, 0, len(ordered))
	for _, item := range ordered {
		last := len(merged) - 1
		if last < 0 || item.StartSample > merged[last].end {
			merged = append(merged, mergedRange{ids: []string{item.ID}, start: item.StartSample, end: item.EndSample})
			continue
		}
		merged[last].ids = append(merged[last].ids, item.ID)
		if item.EndSample > merged[last].end {
			merged[last].end = item.EndSample
		}
	}
	return merged
}

// maxAudioSamples 计算包含上下文的完整 WAV 可用样本上限。
func maxAudioSamples(sampleRate int, maxBytes int64) (int64, error) {
	if maxBytes == 0 {
		maxBytes = DefaultMaxWAVBytes
	}
	byteLimited := (maxBytes - 44) / 2
	durationLimited := int64(9 * 60 * sampleRate)
	if byteLimited <= 0 {
		return 0, fmt.Errorf("WAV 字节上限无效")
	}
	if byteLimited < durationLimited {
		return byteLimited, nil
	}
	return durationLimited, nil
}

// splitRange 按硬上限拆分核心范围，并为每个子片段独立补足上下文。
func splitRange(item mergedRange, input PlanInput, maxAudioSamples int64) []Slice {
	contextSamples := int64(input.SampleRate / 2)
	result := make([]Slice, 0, (item.end-item.start+maxAudioSamples-1)/maxAudioSamples)
	for start := item.start; start < item.end; {
		hardEnd := start + maxAudioSamples
		end := hardEnd
		if hardEnd > item.end {
			end = item.end
		} else if len(input.PCM) > 0 {
			// 把静音搜索目标前移两秒，允许在目标两侧寻找且仍不突破请求硬上限。
			target := hardEnd - int64(2*input.SampleRate)
			end = quietSplit(input.PCM, start, target, hardEnd, input.SampleRate)
		}
		audioStart, audioEnd := boundedAudioRange(start, end, input.RecordingEnd, contextSamples, maxAudioSamples)
		result = append(result, Slice{
			GapIDs: append([]string(nil), item.ids...), CoreStartSample: start, CoreEndSample: end,
			AudioStartSample: audioStart, AudioEndSample: audioEnd,
		})
		start = end
	}
	return result
}

// boundedAudioRange 在硬上限内尽量均衡保留前后上下文，核心范围永不被裁掉。
func boundedAudioRange(coreStart int64, coreEnd int64, recordingEnd int64, contextSamples int64, maxAudioSamples int64) (int64, int64) {
	audioStart := maxInt64(0, coreStart-contextSamples)
	audioEnd := minInt64(recordingEnd, coreEnd+contextSamples)
	excess := audioEnd - audioStart - maxAudioSamples
	if excess <= 0 {
		return audioStart, audioEnd
	}
	rightContext := audioEnd - coreEnd
	trimRight := minInt64(rightContext, (excess+1)/2)
	audioEnd -= trimRight
	excess -= trimRight
	leftContext := coreStart - audioStart
	trimLeft := minInt64(leftContext, excess)
	audioStart += trimLeft
	excess -= trimLeft
	if excess > 0 {
		audioEnd -= minInt64(audioEnd-coreEnd, excess)
	}
	return audioStart, audioEnd
}

// quietSplit 在目标点正负两秒内选择 RMS 最低的 200 ms 窗口中心，找不到时使用硬拆点。
func quietSplit(pcm []int16, coreStart int64, target int64, hardEnd int64, sampleRate int) int64 {
	window := int64(sampleRate / 5)
	radius := int64(2 * sampleRate)
	searchStart := maxInt64(coreStart+1, target-radius-window/2)
	searchEnd := minInt64(int64(len(pcm))-window, target+radius-window/2)
	searchEnd = minInt64(searchEnd, hardEnd-window/2)
	if searchEnd < searchStart {
		return hardEnd
	}
	step := int64(sampleRate / 100)
	bestStart := int64(-1)
	bestEnergy := uint64(^uint64(0))
	for start := searchStart; start <= searchEnd; start += step {
		var energy uint64
		for _, sample := range pcm[start : start+window] {
			value := int64(sample)
			energy += uint64(value * value)
		}
		if energy < bestEnergy {
			bestEnergy, bestStart = energy, start
		}
	}
	if bestStart < 0 {
		return hardEnd
	}
	return bestStart + window/2
}

// minInt64 返回两个样本点中的较小值。
func minInt64(left int64, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

// maxInt64 返回两个样本点中的较大值。
func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
