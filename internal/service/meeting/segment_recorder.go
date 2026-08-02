package meeting

import (
	"fmt"
	"os"
	"path/filepath"

	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/port"
)

// CompletedSegment 是已安全关闭且完成哈希校验的录音分片事实。
type CompletedSegment struct {
	SequenceNo  int
	Path        string
	StartSample int64
	EndSample   int64
	SizeBytes   int64
	SHA256      string
}

// SegmentRecorder 将连续 frame 拆分并写入按序命名的安全 WAV 文件。
type SegmentRecorder struct {
	directory         string
	checkpointSamples int64
	segmenter         *PCMSegmenter
	writer            *WAVPartWriter
	currentSequence   int
	currentStart      int64
}

// NewSegmentRecorder 创建分片录音器；首次写入时才创建目录和 part 文件。
func NewSegmentRecorder(directory string, maxSegmentSamples int64, checkpointSamples int64) (*SegmentRecorder, error) {
	if directory == "" || maxSegmentSamples <= 0 || checkpointSamples <= 0 {
		return nil, fmt.Errorf("分片录音器配置无效")
	}
	return &SegmentRecorder{
		directory: directory, checkpointSamples: checkpointSamples,
		segmenter: NewPCMSegmenter(maxSegmentSamples),
	}, nil
}

// WriteFrame 校验并写入一个连续 PCM frame，返回本次刚完成的分片。
func (recorder *SegmentRecorder) WriteFrame(frame port.AudioFrame) ([]CompletedSegment, error) {
	if recorder == nil || recorder.segmenter == nil {
		return nil, fmt.Errorf("分片录音器未初始化")
	}
	chunks, err := recorder.segmenter.Split(frame)
	if err != nil {
		return nil, err
	}
	completed := make([]CompletedSegment, 0, 1)
	for _, chunk := range chunks {
		if err := recorder.ensureWriter(chunk); err != nil {
			return nil, err
		}
		if err := recorder.writer.WritePCM(chunk.PCM); err != nil {
			return nil, err
		}
		if chunk.CompletesSegment {
			segment, err := recorder.closeWriter(chunk.StartSample + int64(len(chunk.PCM)/2))
			if err != nil {
				return nil, err
			}
			completed = append(completed, segment)
		}
	}
	return completed, nil
}

// CloseCurrent 安全关闭当前非空尾片；没有活动分片时返回 nil。
func (recorder *SegmentRecorder) CloseCurrent() (*CompletedSegment, error) {
	if recorder == nil || recorder.writer == nil {
		return nil, nil
	}
	result, err := recorder.writer.CloseReady(recorder.readyPath(recorder.currentSequence))
	if err != nil {
		return nil, err
	}
	segment, err := recorder.buildCompleted(result, recorder.currentStart+result.SampleCount)
	recorder.writer = nil
	if err != nil {
		return nil, err
	}
	return &segment, nil
}

// Checkpoint 立即持久化当前非空分片，用于首帧成功等业务提交点。
func (recorder *SegmentRecorder) Checkpoint() error {
	if recorder == nil || recorder.writer == nil {
		return nil
	}
	return recorder.writer.Checkpoint()
}

// Abort 释放当前 writer 并保留 part 文件，供异常恢复按真实长度对账。
func (recorder *SegmentRecorder) Abort() error {
	if recorder == nil || recorder.writer == nil {
		return nil
	}
	err := recorder.writer.Abort()
	recorder.writer = nil
	return err
}

// ensureWriter 为 chunk 所属序号创建独占 part 文件，并冻结该分片起始样本。
func (recorder *SegmentRecorder) ensureWriter(chunk PCMChunk) error {
	if recorder.writer != nil {
		if recorder.currentSequence != chunk.SequenceNo {
			return fmt.Errorf("分片 writer 序号不一致")
		}
		return nil
	}
	if err := os.MkdirAll(recorder.directory, 0o700); err != nil {
		return fmt.Errorf("创建录音分片目录失败: %w", err)
	}
	writer, err := NewWAVPartWriter(recorder.partPath(chunk.SequenceNo), recorder.checkpointSamples)
	if err != nil {
		return err
	}
	recorder.writer = writer
	recorder.currentSequence = chunk.SequenceNo
	recorder.currentStart = chunk.StartSample
	return nil
}

// closeWriter 安全关闭刚达到边界的完整分片。
func (recorder *SegmentRecorder) closeWriter(endSample int64) (CompletedSegment, error) {
	result, err := recorder.writer.CloseReady(recorder.readyPath(recorder.currentSequence))
	if err != nil {
		return CompletedSegment{}, err
	}
	segment, err := recorder.buildCompleted(result, endSample)
	recorder.writer = nil
	return segment, err
}

// buildCompleted 计算完成文件哈希并组装可落库元数据。
func (recorder *SegmentRecorder) buildCompleted(result WAVWriteResult, endSample int64) (CompletedSegment, error) {
	path := recorder.readyPath(recorder.currentSequence)
	digest, err := filesystem.SHA256File(path)
	if err != nil {
		return CompletedSegment{}, err
	}
	return CompletedSegment{
		SequenceNo: recorder.currentSequence, Path: path, StartSample: recorder.currentStart,
		EndSample: endSample, SizeBytes: result.SizeBytes, SHA256: digest,
	}, nil
}

// partPath 返回固定六位分片序号的未完成路径。
func (recorder *SegmentRecorder) partPath(sequence int) string {
	return filepath.Join(recorder.directory, fmt.Sprintf("%06d.wav.part", sequence))
}

// readyPath 返回固定六位分片序号的完成路径。
func (recorder *SegmentRecorder) readyPath(sequence int) string {
	return filepath.Join(recorder.directory, fmt.Sprintf("%06d.wav", sequence))
}
