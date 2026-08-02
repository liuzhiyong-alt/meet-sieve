package meeting

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RepairWAVPart 按已落盘的完整 int16 样本修复 header，并安全完成为 `.wav`。
func RepairWAVPart(partPath string, readyPath string) (WAVWriteResult, error) {
	if !strings.HasSuffix(partPath, ".wav.part") || !strings.HasSuffix(readyPath, ".wav") || filepath.Dir(partPath) != filepath.Dir(readyPath) {
		return WAVWriteResult{}, fmt.Errorf("WAV 恢复路径无效")
	}
	file, err := os.OpenFile(partPath, os.O_RDWR, 0)
	if err != nil {
		return WAVWriteResult{}, fmt.Errorf("打开待恢复 WAV part 失败: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil || info.Size() < wavHeaderSize {
		return WAVWriteResult{}, fmt.Errorf("待恢复 WAV part 太短")
	}
	header := make([]byte, wavHeaderSize)
	if _, err := file.ReadAt(header, 0); err != nil && err != io.EOF {
		return WAVWriteResult{}, fmt.Errorf("读取待恢复 WAV header 失败: %w", err)
	}
	if !isCanonicalWAVHeader(header) {
		return WAVWriteResult{}, fmt.Errorf("待恢复 WAV header 格式无效")
	}
	payloadSize := info.Size() - wavHeaderSize
	if payloadSize%2 != 0 {
		payloadSize--
		if err := file.Truncate(wavHeaderSize + payloadSize); err != nil {
			return WAVWriteResult{}, fmt.Errorf("截断不完整 PCM 样本失败: %w", err)
		}
	}
	sampleCount := payloadSize / 2
	updatedHeader, err := encodeRecordingWAVHeader(sampleCount)
	if err != nil {
		return WAVWriteResult{}, err
	}
	if _, err := file.WriteAt(updatedHeader, 0); err != nil {
		return WAVWriteResult{}, fmt.Errorf("修复 WAV header 失败: %w", err)
	}
	if err := file.Sync(); err != nil {
		return WAVWriteResult{}, fmt.Errorf("同步修复 WAV 失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return WAVWriteResult{}, fmt.Errorf("关闭修复 WAV 失败: %w", err)
	}
	closed = true
	if err := os.Rename(partPath, readyPath); err != nil {
		return WAVWriteResult{}, fmt.Errorf("完成修复 WAV 重命名失败: %w", err)
	}
	if err := syncDirectory(filepath.Dir(readyPath)); err != nil {
		return WAVWriteResult{}, err
	}
	return WAVWriteResult{SampleCount: sampleCount, SizeBytes: wavHeaderSize + payloadSize}, nil
}

// isCanonicalWAVHeader 校验可恢复文件的固定格式字段，不信任其中的旧 data size。
func isCanonicalWAVHeader(header []byte) bool {
	return len(header) == wavHeaderSize && string(header[0:4]) == "RIFF" && string(header[8:12]) == "WAVE" &&
		string(header[12:16]) == "fmt " && string(header[36:40]) == "data"
}
