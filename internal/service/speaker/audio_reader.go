package speaker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/models"
)

const canonicalWAVHeaderSize = 44

var (
	// ErrAudioEvidencePending 表示所需范围尚未进入 rolling buffer 或 ready 文件。
	ErrAudioEvidencePending = errors.New("音频证据尚未准备")
	// ErrAudioRangeInvalid 表示采样范围为空、越界或超过单次有界读取上限。
	ErrAudioRangeInvalid = errors.New("音频证据范围无效")
	// ErrAudioAssetUnsafe 表示数据库资产路径、状态或 WAV 格式不可信。
	ErrAudioAssetUnsafe = errors.New("音频资产不安全")
)

// AudioAssetSource 提供数据库已登记的完成分片与完整录音，不扫描目录猜测资产。
type AudioAssetSource interface {
	ListReadyMicrophoneAssets(ctx context.Context, meetingID string) ([]models.AudioAsset, error)
	ListReadyMixedAssets(ctx context.Context, meetingID string) ([]models.AudioAsset, error)
}

// MeetingAudioReader 按 rolling、完成分片、完整录音的顺序读取有界全局采样范围。
type MeetingAudioReader struct {
	workspaceRoot  string
	assets         AudioAssetSource
	rolling        *RollingBuffer
	maxReadSamples int64
}

// NewMeetingAudioReader 创建只允许工作目录内 ready PCM WAV 的范围读取器。
func NewMeetingAudioReader(root string, assets AudioAssetSource, rolling *RollingBuffer, maxReadSamples int64) (*MeetingAudioReader, error) {
	if !filepath.IsAbs(root) || assets == nil || maxReadSamples <= 0 {
		return nil, fmt.Errorf("会议音频读取器依赖无效")
	}
	return &MeetingAudioReader{workspaceRoot: root, assets: assets, rolling: rolling, maxReadSamples: maxReadSamples}, nil
}

// Read 返回 `[startSample,endSample)` PCM；不会补零或按墙上时钟估算偏移。
func (reader *MeetingAudioReader) Read(ctx context.Context, meetingID string, startSample int64, endSample int64) ([]int16, error) {
	if reader == nil || meetingID == "" || startSample < 0 || endSample <= startSample || endSample-startSample > reader.maxReadSamples {
		return nil, ErrAudioRangeInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if reader.rolling != nil {
		if samples, ok := reader.rolling.Read(startSample, endSample); ok {
			return samples, nil
		}
	}
	segments, err := reader.assets.ListReadyMicrophoneAssets(ctx, meetingID)
	if err != nil {
		return nil, fmt.Errorf("查询 ready 录音分片失败：%w", err)
	}
	if samples, complete, readErr := reader.readFromSegments(ctx, segments, startSample, endSample); readErr != nil {
		return nil, readErr
	} else if complete {
		return samples, nil
	}
	mixed, err := reader.assets.ListReadyMixedAssets(ctx, meetingID)
	if err != nil {
		return nil, fmt.Errorf("查询完整录音失败：%w", err)
	}
	for _, asset := range mixed {
		if asset.StartSample <= startSample && asset.EndSample >= endSample {
			return reader.readAssetRange(ctx, asset, startSample, endSample)
		}
	}
	return nil, ErrAudioEvidencePending
}

// readFromSegments 按全局起点排序后精确拼接覆盖范围，遇到 gap 时留给 mixed 回退。
func (reader *MeetingAudioReader) readFromSegments(ctx context.Context, assets []models.AudioAsset, startSample int64, endSample int64) ([]int16, bool, error) {
	sorted := append([]models.AudioAsset(nil), assets...)
	sort.Slice(sorted, func(left int, right int) bool {
		if sorted[left].StartSample == sorted[right].StartSample {
			return sorted[left].SequenceNo < sorted[right].SequenceNo
		}
		return sorted[left].StartSample < sorted[right].StartSample
	})
	result := make([]int16, 0, endSample-startSample)
	cursor := startSample
	for _, asset := range sorted {
		if asset.EndSample <= cursor {
			continue
		}
		if asset.StartSample > cursor {
			return nil, false, nil
		}
		readEnd := minInt64(asset.EndSample, endSample)
		piece, err := reader.readAssetRange(ctx, asset, cursor, readEnd)
		if err != nil {
			return nil, false, err
		}
		result = append(result, piece...)
		cursor = readEnd
		if cursor == endSample {
			return result, true, nil
		}
	}
	return nil, false, nil
}

// readAssetRange 校验数据库事实和固定 WAV header，再仅 ReadAt 所需 PCM 字节。
func (reader *MeetingAudioReader) readAssetRange(ctx context.Context, asset models.AudioAsset, startSample int64, endSample int64) ([]int16, error) {
	if err := validateAudioAsset(asset, startSample, endSample); err != nil {
		return nil, err
	}
	path, err := filesystem.ResolveWithinRoot(reader.workspaceRoot, filepath.FromSlash(asset.RelativePath))
	if err != nil {
		return nil, fmt.Errorf("%w: 路径越界", ErrAudioAssetUnsafe)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: 文件不可读", ErrAudioAssetUnsafe)
	}
	defer file.Close()
	if err := validateWAVFile(file, asset); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	length := endSample - startSample
	data := make([]byte, length*2)
	offset := int64(canonicalWAVHeaderSize) + (startSample-asset.StartSample)*2
	if _, err := file.ReadAt(data, offset); err != nil {
		return nil, fmt.Errorf("%w: PCM 范围截断", ErrAudioAssetUnsafe)
	}
	samples := make([]int16, length)
	for index := range samples {
		samples[index] = int16(binary.LittleEndian.Uint16(data[index*2:]))
	}
	return samples, nil
}

// validateAudioAsset 拒绝 part、非 ready、格式不一致和元数据范围外读取。
func validateAudioAsset(asset models.AudioAsset, startSample int64, endSample int64) error {
	if asset.State != "ready" || (asset.Kind != "microphone" && asset.Kind != "mixed") ||
		!strings.HasSuffix(asset.RelativePath, ".wav") || strings.HasSuffix(asset.RelativePath, ".wav.part") ||
		asset.SampleRate != 16000 || asset.BitDepth != 16 || asset.Channels != 1 ||
		asset.StartSample < 0 || asset.EndSample <= asset.StartSample || startSample < asset.StartSample || endSample > asset.EndSample {
		return ErrAudioAssetUnsafe
	}
	return nil
}

// validateWAVFile 核对普通文件大小、固定 PCM header 和数据库采样范围。
func validateWAVFile(file *os.File, asset models.AudioAsset) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != asset.SizeBytes ||
		info.Size() != canonicalWAVHeaderSize+(asset.EndSample-asset.StartSample)*2 {
		return fmt.Errorf("%w: 文件大小不一致", ErrAudioAssetUnsafe)
	}
	header := make([]byte, canonicalWAVHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("%w: WAV header 截断", ErrAudioAssetUnsafe)
	}
	dataSize := int64(binary.LittleEndian.Uint32(header[40:44]))
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" || string(header[12:16]) != "fmt " ||
		string(header[36:40]) != "data" || binary.LittleEndian.Uint32(header[4:8]) != uint32(36+dataSize) ||
		binary.LittleEndian.Uint16(header[20:22]) != 1 || binary.LittleEndian.Uint16(header[22:24]) != 1 ||
		binary.LittleEndian.Uint32(header[24:28]) != 16000 || binary.LittleEndian.Uint16(header[32:34]) != 2 ||
		binary.LittleEndian.Uint16(header[34:36]) != 16 || dataSize != (asset.EndSample-asset.StartSample)*2 {
		return fmt.Errorf("%w: WAV 格式不一致", ErrAudioAssetUnsafe)
	}
	return nil
}
