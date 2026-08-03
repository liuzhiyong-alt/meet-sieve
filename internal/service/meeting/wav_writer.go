package meeting

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domainaudio "meet-sieve/internal/domain/audio"
)

const (
	recordingSampleRate = domainaudio.SampleRate
	recordingBitDepth   = domainaudio.BitDepth
	recordingChannels   = domainaudio.Channels
	wavHeaderSize       = domainaudio.WAVHeaderSize
)

// WAVWriteResult 描述完成分片可与文件系统核对的样本数和字节数。
type WAVWriteResult struct {
	SampleCount int64
	SizeBytes   int64
}

// WAVPartWriter 以 `.wav.part` 写入固定 16 kHz、16-bit、mono PCM，并定期持久化 header。
type WAVPartWriter struct {
	file              *os.File
	partPath          string
	sampleCount       int64
	checkpointSamples int64
	nextCheckpoint    int64
	closed            bool
	completedPath     string
	completedResult   WAVWriteResult
}

// NewWAVPartWriter 创建独占 `.wav.part` 文件并写入零样本 WAV header。
func NewWAVPartWriter(partPath string, checkpointSamples int64) (*WAVPartWriter, error) {
	if !strings.HasSuffix(partPath, ".wav.part") || checkpointSamples <= 0 {
		return nil, fmt.Errorf("WAV part 路径或检查点无效")
	}
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建 WAV part 文件失败: %w", err)
	}
	writer := &WAVPartWriter{
		file: file, partPath: partPath, checkpointSamples: checkpointSamples, nextCheckpoint: checkpointSamples,
	}
	if err := writer.writeHeader(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return writer, nil
}

// WritePCM 追加完整 int16 PCM 样本，并在跨过检查点时同步 header 与文件内容。
func (writer *WAVPartWriter) WritePCM(pcm []byte) error {
	if writer == nil || writer.file == nil || writer.closed {
		return fmt.Errorf("WAV writer 已关闭")
	}
	if len(pcm) == 0 {
		return nil
	}
	if len(pcm)%2 != 0 {
		return fmt.Errorf("PCM 样本边界不完整")
	}
	if _, err := writer.file.Write(pcm); err != nil {
		return fmt.Errorf("写入 WAV PCM 失败: %w", err)
	}
	writer.sampleCount += int64(len(pcm) / 2)
	if writer.sampleCount < writer.nextCheckpoint {
		return nil
	}
	if err := writer.syncCheckpoint(); err != nil {
		return err
	}
	writer.nextCheckpoint = (writer.sampleCount/writer.checkpointSamples + 1) * writer.checkpointSamples
	return nil
}

// Checkpoint 立即更新 header 并同步当前 PCM，用于首帧等业务提交点。
func (writer *WAVPartWriter) Checkpoint() error {
	if writer == nil || writer.file == nil || writer.closed {
		return fmt.Errorf("WAV writer 已关闭")
	}
	return writer.syncCheckpoint()
}

// CloseReady 更新最终 header、同步文件、原子重命名并同步父目录。
func (writer *WAVPartWriter) CloseReady(readyPath string) (WAVWriteResult, error) {
	if writer != nil && writer.completedPath != "" {
		if writer.completedPath == readyPath {
			return writer.completedResult, nil
		}
		return WAVWriteResult{}, fmt.Errorf("WAV writer 已完成到其他路径")
	}
	if writer == nil || writer.file == nil || writer.closed {
		return WAVWriteResult{}, fmt.Errorf("WAV writer 已关闭")
	}
	if !strings.HasSuffix(readyPath, ".wav") || filepath.Dir(readyPath) != filepath.Dir(writer.partPath) {
		return WAVWriteResult{}, fmt.Errorf("WAV 完成路径无效")
	}
	if err := writer.syncCheckpoint(); err != nil {
		return WAVWriteResult{}, err
	}
	if err := writer.file.Close(); err != nil {
		return WAVWriteResult{}, fmt.Errorf("关闭 WAV part 文件失败: %w", err)
	}
	writer.closed = true
	if err := os.Rename(writer.partPath, readyPath); err != nil {
		return WAVWriteResult{}, fmt.Errorf("完成 WAV 原子重命名失败: %w", err)
	}
	if err := syncDirectory(filepath.Dir(readyPath)); err != nil {
		return WAVWriteResult{}, err
	}
	info, err := os.Stat(readyPath)
	if err != nil {
		return WAVWriteResult{}, fmt.Errorf("读取完成 WAV 信息失败: %w", err)
	}
	writer.completedPath = readyPath
	writer.completedResult = WAVWriteResult{SampleCount: writer.sampleCount, SizeBytes: info.Size()}
	return writer.completedResult, nil
}

// Abort 释放活动文件句柄并保留 `.wav.part`，供下次启动按真实文件长度恢复。
func (writer *WAVPartWriter) Abort() error {
	if writer == nil || writer.file == nil || writer.closed {
		return nil
	}
	writer.closed = true
	if err := writer.file.Close(); err != nil {
		return fmt.Errorf("关闭未完成 WAV part 失败: %w", err)
	}
	return nil
}

// syncCheckpoint 按当前样本数更新 header，并把文件内容同步到稳定存储。
func (writer *WAVPartWriter) syncCheckpoint() error {
	if err := writer.writeHeader(); err != nil {
		return err
	}
	if err := writer.file.Sync(); err != nil {
		return fmt.Errorf("同步 WAV 文件失败: %w", err)
	}
	return nil
}

// writeHeader 写入与当前样本数一致的标准 PCM WAV header，并恢复文件尾写入位置。
func (writer *WAVPartWriter) writeHeader() error {
	header, err := encodeRecordingWAVHeader(writer.sampleCount)
	if err != nil {
		return err
	}
	if _, err := writer.file.WriteAt(header, 0); err != nil {
		return fmt.Errorf("更新 WAV header 失败: %w", err)
	}
	if _, err := writer.file.Seek(0, 2); err != nil {
		return fmt.Errorf("恢复 WAV 写入位置失败: %w", err)
	}
	return nil
}

// encodeRecordingWAVHeader 生成与指定样本数一致的固定格式 WAV header。
func encodeRecordingWAVHeader(sampleCount int64) ([]byte, error) {
	return domainaudio.EncodeCanonicalWAVHeader(sampleCount)
}

// syncDirectory 同步原子重命名所在目录，确保目录项进入稳定存储。
func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("打开 WAV 目录失败: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("同步 WAV 目录失败: %w", err)
	}
	return nil
}
