package gap

import "meet-sieve/internal/port"

// CandidateSegment 是映射到会议绝对时间线并裁回 core 的文件候选。
type CandidateSegment struct {
	Text        string `json:"text"`
	SpeakerID   string `json:"speaker_id"`
	StartSample int64  `json:"start_sample"`
	EndSample   int64  `json:"end_sample"`
}

// NormalizeToCore 把切片相对结果映射到会议样本并裁回核心范围。
func NormalizeToCore(segments []port.FileTranscriptionSegment, audioStart int64, coreStart int64, coreEnd int64) []CandidateSegment {
	result := make([]CandidateSegment, 0, len(segments))
	for _, segment := range segments {
		start := audioStart + segment.StartSample
		end := audioStart + segment.EndSample
		if !HasPositiveOverlap(start, end, coreStart, coreEnd) {
			continue
		}
		if start < coreStart {
			start = coreStart
		}
		if end > coreEnd {
			end = coreEnd
		}
		result = append(result, CandidateSegment{
			Text: segment.Text, SpeakerID: segment.SpeakerID, StartSample: start, EndSample: end,
		})
	}
	return result
}

// HasPositiveOverlap 判断两个半开区间是否存在正长度交集。
func HasPositiveOverlap(leftStart int64, leftEnd int64, rightStart int64, rightEnd int64) bool {
	return maxInt64(leftStart, rightStart) < minInt64(leftEnd, rightEnd)
}

// AggregateState 按冲突、处理中、失败、待处理、完成、无缺口顺序汇总会议状态。
func AggregateState(states []State) string {
	if len(states) == 0 {
		return "none"
	}
	present := make(map[State]bool, len(states))
	for _, state := range states {
		present[state] = true
	}
	for _, state := range []State{StateConflict, StateProcessing, StateFailed, StatePending} {
		if present[state] {
			return string(state)
		}
	}
	return string(StateCompleted)
}
