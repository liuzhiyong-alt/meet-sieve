package meeting

import (
	"fmt"
	"os"

	domainaudio "meet-sieve/internal/domain/audio"
)

// MergeWAVSegments 按输入顺序读取固定格式分片，并安全生成最终 WAV；源分片始终保留。
func MergeWAVSegments(segmentPaths []string, readyPath string, checkpointSamples int64) (WAVWriteResult, error) {
	if len(segmentPaths) == 0 {
		return WAVWriteResult{}, fmt.Errorf("没有可合并的录音分片")
	}
	writer, err := NewWAVPartWriter(readyPath+".part", checkpointSamples)
	if err != nil {
		return WAVWriteResult{}, err
	}
	completed := false
	defer func() {
		if !completed {
			_ = writer.Abort()
		}
	}()
	for _, segmentPath := range segmentPaths {
		pcm, err := readCanonicalWAV(segmentPath)
		if err != nil {
			return WAVWriteResult{}, err
		}
		if err := writer.WritePCM(pcm); err != nil {
			return WAVWriteResult{}, err
		}
	}
	result, err := writer.CloseReady(readyPath)
	completed = err == nil
	return result, err
}

// readCanonicalWAV 读取并校验 Step 3 固定格式 WAV，只返回 data chunk PCM。
func readCanonicalWAV(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取录音分片失败: %w", err)
	}
	pcm, err := domainaudio.DecodeCanonicalWAV(data)
	if err != nil {
		return nil, fmt.Errorf("校验录音分片失败: %w", err)
	}
	return pcm, nil
}
