package meeting

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestWAVPartWriterClosesReadyFile 验证安全关闭后 WAV header 与真实 PCM 数据一致。
func TestWAVPartWriterClosesReadyFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	partPath := filepath.Join(directory, "000001.wav.part")
	readyPath := filepath.Join(directory, "000001.wav")
	writer, err := NewWAVPartWriter(partPath, 32000)
	if err != nil {
		t.Fatalf("创建 WAV writer 失败：%v", err)
	}
	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	if err := writer.WritePCM(pcm); err != nil {
		t.Fatalf("写入 PCM 失败：%v", err)
	}
	result, err := writer.CloseReady(readyPath)
	if err != nil {
		t.Fatalf("安全关闭 WAV 失败：%v", err)
	}
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("读取完成 WAV 失败：%v", err)
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" || binary.LittleEndian.Uint32(data[40:44]) != uint32(len(pcm)) {
		t.Fatalf("WAV header 不正确：%v", data[:44])
	}
	if string(data[44:]) != string(pcm) || result.SampleCount != 4 || result.SizeBytes != int64(len(data)) {
		t.Fatalf("WAV 完成结果不正确：result=%+v data=%v", result, data)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("完成后不得遗留 part 文件：%v", err)
	}
}

// TestWAVPartWriterAbortLeavesRecoveryPart 验证失败关闭释放文件句柄但保留可恢复 part。
func TestWAVPartWriterAbortLeavesRecoveryPart(t *testing.T) {
	t.Parallel()

	partPath := filepath.Join(t.TempDir(), "000001.wav.part")
	writer, err := NewWAVPartWriter(partPath, 32000)
	if err != nil {
		t.Fatalf("创建 WAV writer 失败：%v", err)
	}
	if err := writer.WritePCM([]byte{1, 0}); err != nil {
		t.Fatalf("写入 PCM 失败：%v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("失败关闭 WAV part 失败：%v", err)
	}
	if _, err := os.Stat(partPath); err != nil {
		t.Fatalf("失败关闭必须保留 part：%v", err)
	}
	if err := writer.WritePCM([]byte{2, 0}); err == nil {
		t.Fatal("失败关闭后不得继续写入")
	}
}

// TestWAVPartWriterCloseReadyIsIdempotent 验证重复安全关闭不会再次改写或重命名文件。
func TestWAVPartWriterCloseReadyIsIdempotent(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writer, err := NewWAVPartWriter(filepath.Join(directory, "000001.wav.part"), 32000)
	if err != nil {
		t.Fatalf("创建 WAV writer 失败：%v", err)
	}
	if err := writer.WritePCM([]byte{1, 0}); err != nil {
		t.Fatalf("写入 PCM 失败：%v", err)
	}
	readyPath := filepath.Join(directory, "000001.wav")
	first, err := writer.CloseReady(readyPath)
	if err != nil {
		t.Fatalf("首次关闭失败：%v", err)
	}
	second, err := writer.CloseReady(readyPath)
	if err != nil || second != first {
		t.Fatalf("重复关闭必须返回首次结果：first=%+v second=%+v err=%v", first, second, err)
	}
}
