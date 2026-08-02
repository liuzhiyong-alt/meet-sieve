package meeting

import (
	"encoding/binary"
	"fmt"
	"os"
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
	if len(data) < wavHeaderSize || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" ||
		string(data[12:16]) != "fmt " || string(data[36:40]) != "data" {
		return nil, fmt.Errorf("录音分片 WAV header 无效")
	}
	dataSize := int(binary.LittleEndian.Uint32(data[40:44]))
	if binary.LittleEndian.Uint32(data[4:8]) != uint32(36+dataSize) || len(data) != wavHeaderSize+dataSize {
		return nil, fmt.Errorf("录音分片长度与 header 不一致")
	}
	if binary.LittleEndian.Uint16(data[20:22]) != 1 ||
		binary.LittleEndian.Uint16(data[22:24]) != recordingChannels ||
		binary.LittleEndian.Uint32(data[24:28]) != recordingSampleRate ||
		binary.LittleEndian.Uint16(data[34:36]) != recordingBitDepth || dataSize%2 != 0 {
		return nil, fmt.Errorf("录音分片格式不一致")
	}
	return append([]byte(nil), data[wavHeaderSize:]...), nil
}
