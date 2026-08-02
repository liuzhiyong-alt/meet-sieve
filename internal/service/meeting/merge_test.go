package meeting

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/port"
)

// TestMergeWAVSegmentsPreservesPCMOrder 验证最终录音按分片顺序合并且不删除源分片。
func TestMergeWAVSegmentsPreservesPCMOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	segmentsDirectory := filepath.Join(root, "segments")
	recorder, err := NewSegmentRecorder(segmentsDirectory, 2, 1)
	if err != nil {
		t.Fatalf("创建分片录音器失败：%v", err)
	}
	if _, err := recorder.WriteFrame(port.AudioFrame{StartSample: 0, PCM: []byte{1, 0, 2, 0, 3, 0}}); err != nil {
		t.Fatalf("准备分片失败：%v", err)
	}
	if _, err := recorder.CloseCurrent(); err != nil {
		t.Fatalf("关闭尾片失败：%v", err)
	}
	segmentPaths := []string{filepath.Join(segmentsDirectory, "000001.wav"), filepath.Join(segmentsDirectory, "000002.wav")}
	readyPath := filepath.Join(root, "recording.wav")
	result, err := MergeWAVSegments(segmentPaths, readyPath, 1)
	if err != nil {
		t.Fatalf("合并 WAV 分片失败：%v", err)
	}
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("读取最终录音失败：%v", err)
	}
	if result.SampleCount != 3 || binary.LittleEndian.Uint32(data[40:44]) != 6 || string(data[44:]) != string([]byte{1, 0, 2, 0, 3, 0}) {
		t.Fatalf("最终录音内容不正确：result=%+v data=%v", result, data)
	}
	for _, path := range segmentPaths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("合并成功后仍必须保留分片：path=%s err=%v", path, err)
		}
	}
}
