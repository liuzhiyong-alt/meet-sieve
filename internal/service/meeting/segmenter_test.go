package meeting

import (
	"testing"

	"meet-sieve/internal/port"
)

// TestPCMSegmenterSplitsCrossBoundaryFrame 验证跨分片 frame 在精确样本边界拆开。
func TestPCMSegmenterSplitsCrossBoundaryFrame(t *testing.T) {
	t.Parallel()

	segmenter := NewPCMSegmenter(4)
	chunks, err := segmenter.Split(port.AudioFrame{
		StartSample: 0,
		PCM:         []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0},
	})
	if err != nil {
		t.Fatalf("拆分 PCM frame 失败：%v", err)
	}
	if len(chunks) != 2 || chunks[0].SequenceNo != 1 || chunks[0].StartSample != 0 || len(chunks[0].PCM) != 8 || !chunks[0].CompletesSegment {
		t.Fatalf("首分片 chunk 不正确：%+v", chunks)
	}
	if chunks[1].SequenceNo != 2 || chunks[1].StartSample != 4 || len(chunks[1].PCM) != 4 || chunks[1].CompletesSegment {
		t.Fatalf("尾分片 chunk 不正确：%+v", chunks)
	}
}

// TestPCMSegmenterRejectsDiscontinuity 验证丢帧或重叠不会被静默填零或覆盖。
func TestPCMSegmenterRejectsDiscontinuity(t *testing.T) {
	t.Parallel()

	segmenter := NewPCMSegmenter(4)
	if _, err := segmenter.Split(port.AudioFrame{StartSample: 1, PCM: []byte{1, 0}}); err == nil {
		t.Fatal("首帧起点不连续必须失败")
	}
}
