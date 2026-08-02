package speaker_test

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	speakerservice "meet-sieve/internal/service/speaker"
	"meet-sieve/models"
)

type fixedAudioAssets struct {
	microphone []models.AudioAsset
	mixed      []models.AudioAsset
}

// ListReadyMicrophoneAssets 返回测试登记的 ready 分片。
func (source fixedAudioAssets) ListReadyMicrophoneAssets(context.Context, string) ([]models.AudioAsset, error) {
	return source.microphone, nil
}

// ListReadyMixedAssets 返回测试登记的完整录音。
func (source fixedAudioAssets) ListReadyMixedAssets(context.Context, string) ([]models.AudioAsset, error) {
	return source.mixed, nil
}

// TestMeetingAudioReader_UsesRollingBeforeReadyWAV 验证实时 buffer 优先且不会触碰磁盘内容。
func TestMeetingAudioReader_UsesRollingBeforeReadyWAV(t *testing.T) {
	root := t.TempDir()
	buffer, _ := speakerservice.NewRollingBuffer(16)
	_ = buffer.Write(0, []int16{9, 8, 7, 6})
	asset := writeReadyAsset(t, root, "segments/one.wav", 0, []int16{1, 2, 3, 4}, "microphone")
	reader, err := speakerservice.NewMeetingAudioReader(root, fixedAudioAssets{microphone: []models.AudioAsset{asset}}, buffer, 16)
	if err != nil {
		t.Fatalf("创建音频读取器失败：%v", err)
	}
	got, err := reader.Read(context.Background(), "meeting", 1, 3)
	if err != nil || !samePCM(got, []int16{8, 7}) {
		t.Fatalf("rolling 优先级错误：got=%v err=%v", got, err)
	}
}

// TestMeetingAudioReader_JoinsReadySegmentsAndFallsBackToMixed 验证跨分片无静音拼接及完整录音回退。
func TestMeetingAudioReader_JoinsReadySegmentsAndFallsBackToMixed(t *testing.T) {
	root := t.TempDir()
	first := writeReadyAsset(t, root, "segments/one.wav", 0, []int16{0, 1, 2}, "microphone")
	second := writeReadyAsset(t, root, "segments/two.wav", 3, []int16{3, 4, 5}, "microphone")
	mixed := writeReadyAsset(t, root, "recording.wav", 0, []int16{0, 1, 2, 3, 4, 5}, "mixed")

	reader, _ := speakerservice.NewMeetingAudioReader(root, fixedAudioAssets{microphone: []models.AudioAsset{first, second}}, nil, 16)
	got, err := reader.Read(context.Background(), "meeting", 1, 5)
	if err != nil || !samePCM(got, []int16{1, 2, 3, 4}) {
		t.Fatalf("跨分片读取错误：got=%v err=%v", got, err)
	}
	reader, _ = speakerservice.NewMeetingAudioReader(root, fixedAudioAssets{mixed: []models.AudioAsset{mixed}}, nil, 16)
	got, err = reader.Read(context.Background(), "meeting", 2, 6)
	if err != nil || !samePCM(got, []int16{2, 3, 4, 5}) {
		t.Fatalf("完整录音回退错误：got=%v err=%v", got, err)
	}
}

// TestMeetingAudioReader_RejectsGapPartAndOversizedRange 验证不补零、不读 part 且内存范围有界。
func TestMeetingAudioReader_RejectsGapPartAndOversizedRange(t *testing.T) {
	root := t.TempDir()
	asset := writeReadyAsset(t, root, "segments/one.wav", 0, []int16{0, 1}, "microphone")
	reader, _ := speakerservice.NewMeetingAudioReader(root, fixedAudioAssets{microphone: []models.AudioAsset{asset}}, nil, 4)
	if _, err := reader.Read(context.Background(), "meeting", 0, 3); !errors.Is(err, speakerservice.ErrAudioEvidencePending) {
		t.Fatalf("分片缺口必须返回 pending：%v", err)
	}
	if _, err := reader.Read(context.Background(), "meeting", 0, 5); !errors.Is(err, speakerservice.ErrAudioRangeInvalid) {
		t.Fatalf("超出上限必须拒绝：%v", err)
	}
	part := asset
	part.RelativePath += ".part"
	reader, _ = speakerservice.NewMeetingAudioReader(root, fixedAudioAssets{microphone: []models.AudioAsset{part}}, nil, 4)
	if _, err := reader.Read(context.Background(), "meeting", 0, 2); !errors.Is(err, speakerservice.ErrAudioAssetUnsafe) {
		t.Fatalf("part 资产必须拒绝：%v", err)
	}
}

// writeReadyAsset 写入固定 16 kHz/16-bit/mono WAV，并返回对应数据库事实。
func writeReadyAsset(t *testing.T, root string, relative string, start int64, samples []int16, kind string) models.AudioAsset {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("创建测试音频目录失败：%v", err)
	}
	content := encodeTestWAV(samples)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入测试 WAV 失败：%v", err)
	}
	return models.AudioAsset{
		ID: "11111111-1111-4111-8111-111111111111", MeetingID: "meeting", Kind: kind, SequenceNo: 1,
		RelativePath: filepath.ToSlash(relative), StartSample: start, EndSample: start + int64(len(samples)),
		SampleRate: 16000, BitDepth: 16, Channels: 1, SizeBytes: int64(len(content)), State: "ready",
	}
}

// encodeTestWAV 生成测试读取器接受的最小标准 WAV。
func encodeTestWAV(samples []int16) []byte {
	content := make([]byte, 44+len(samples)*2)
	copy(content[0:4], "RIFF")
	binary.LittleEndian.PutUint32(content[4:8], uint32(len(content)-8))
	copy(content[8:12], "WAVE")
	copy(content[12:16], "fmt ")
	binary.LittleEndian.PutUint32(content[16:20], 16)
	binary.LittleEndian.PutUint16(content[20:22], 1)
	binary.LittleEndian.PutUint16(content[22:24], 1)
	binary.LittleEndian.PutUint32(content[24:28], 16000)
	binary.LittleEndian.PutUint32(content[28:32], 32000)
	binary.LittleEndian.PutUint16(content[32:34], 2)
	binary.LittleEndian.PutUint16(content[34:36], 16)
	copy(content[36:40], "data")
	binary.LittleEndian.PutUint32(content[40:44], uint32(len(samples)*2))
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(content[44+index*2:], uint16(sample))
	}
	return content
}

// samePCM 比较读取器返回的短样本序列。
func samePCM(left []int16, right []int16) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
