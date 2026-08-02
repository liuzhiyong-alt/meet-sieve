package meeting

import (
	"fmt"

	"meet-sieve/internal/port"
)

// PCMChunk 是单个 frame 经精确边界拆分后交给一个分片 writer 的连续 PCM。
type PCMChunk struct {
	SequenceNo       int
	StartSample      int64
	PCM              []byte
	CompletesSegment bool
}

// PCMSegmenter 按累计样本数拆分连续 PCM，不使用壁钟估算边界。
type PCMSegmenter struct {
	maxSamples       int64
	nextSample       int64
	sequenceNo       int
	samplesInSegment int64
}

// NewPCMSegmenter 创建固定最大样本数的连续分片器。
func NewPCMSegmenter(maxSamples int64) *PCMSegmenter {
	return &PCMSegmenter{maxSamples: maxSamples, sequenceNo: 1}
}

// Split 校验 frame 连续性，并在分片样本边界精确拆成一个或多个 chunk。
func (segmenter *PCMSegmenter) Split(frame port.AudioFrame) ([]PCMChunk, error) {
	if segmenter == nil || segmenter.maxSamples <= 0 {
		return nil, fmt.Errorf("PCM 分片器配置无效")
	}
	if len(frame.PCM)%2 != 0 {
		return nil, fmt.Errorf("PCM frame 样本边界不完整")
	}
	if frame.StartSample != segmenter.nextSample {
		return nil, fmt.Errorf("PCM frame 不连续：got=%d want=%d", frame.StartSample, segmenter.nextSample)
	}
	remaining := int64(len(frame.PCM) / 2)
	offsetSamples := int64(0)
	chunks := make([]PCMChunk, 0, 2)
	for remaining > 0 {
		capacity := segmenter.maxSamples - segmenter.samplesInSegment
		accepted := remaining
		if accepted > capacity {
			accepted = capacity
		}
		startByte := offsetSamples * 2
		endByte := startByte + accepted*2
		segmenter.samplesInSegment += accepted
		chunk := PCMChunk{
			SequenceNo: segmenter.sequenceNo, StartSample: segmenter.nextSample,
			PCM:              append([]byte(nil), frame.PCM[startByte:endByte]...),
			CompletesSegment: segmenter.samplesInSegment == segmenter.maxSamples,
		}
		chunks = append(chunks, chunk)
		segmenter.nextSample += accepted
		offsetSamples += accepted
		remaining -= accepted
		if chunk.CompletesSegment {
			segmenter.sequenceNo++
			segmenter.samplesInSegment = 0
		}
	}
	return chunks, nil
}
