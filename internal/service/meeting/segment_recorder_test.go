package meeting

import (
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/port"
)

// TestSegmentRecorderPersistsCompletedAndTailSegments 验证跨界分片与停止尾片均生成完整元数据。
func TestSegmentRecorderPersistsCompletedAndTailSegments(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "segments")
	recorder, err := NewSegmentRecorder(directory, 4, 2)
	if err != nil {
		t.Fatalf("创建分片录音器失败：%v", err)
	}
	completed, err := recorder.WriteFrame(port.AudioFrame{
		StartSample: 0, PCM: []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0},
	})
	if err != nil {
		t.Fatalf("写入跨界 frame 失败：%v", err)
	}
	if len(completed) != 1 || completed[0].SequenceNo != 1 || completed[0].StartSample != 0 || completed[0].EndSample != 4 || len(completed[0].SHA256) != 64 {
		t.Fatalf("完成分片元数据不正确：%+v", completed)
	}
	tail, err := recorder.CloseCurrent()
	if err != nil || tail == nil || tail.SequenceNo != 2 || tail.StartSample != 4 || tail.EndSample != 6 || len(tail.SHA256) != 64 {
		t.Fatalf("尾分片元数据不正确：tail=%+v err=%v", tail, err)
	}
	for _, name := range []string{"000001.wav", "000002.wav"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("分片文件不存在：name=%s err=%v", name, err)
		}
	}
}
