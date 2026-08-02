package meeting

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestRepairWAVPartUsesPersistedFileLength 验证崩溃后按完整 int16 数据长度修复过期 header。
func TestRepairWAVPartUsesPersistedFileLength(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	partPath := filepath.Join(directory, "000001.wav.part")
	readyPath := filepath.Join(directory, "000001.wav")
	writer, err := NewWAVPartWriter(partPath, 32000)
	if err != nil {
		t.Fatalf("创建 WAV writer 失败：%v", err)
	}
	if err := writer.WritePCM([]byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("写入未检查点 PCM 失败：%v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("模拟崩溃关闭失败：%v", err)
	}

	result, err := RepairWAVPart(partPath, readyPath)
	if err != nil {
		t.Fatalf("修复 WAV part 失败：%v", err)
	}
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("读取修复 WAV 失败：%v", err)
	}
	if result.SampleCount != 2 || binary.LittleEndian.Uint32(data[40:44]) != 4 || string(data[44:]) != string([]byte{1, 0, 2, 0}) {
		t.Fatalf("修复 WAV 内容不正确：result=%+v data=%v", result, data)
	}
}
