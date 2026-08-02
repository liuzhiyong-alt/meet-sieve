package app

import (
	"testing"

	"meet-sieve/internal/port"
	speakerservice "meet-sieve/internal/service/speaker"
)

// TestWriteRollingFrameResetsForNextMeeting 验证小端 PCM 转换准确且新会议从零样本重置范围。
func TestWriteRollingFrameResetsForNextMeeting(t *testing.T) {
	buffer, err := speakerservice.NewRollingBuffer(4)
	if err != nil {
		t.Fatalf("创建 rolling buffer 失败：%v", err)
	}
	if err = writeRollingFrame(buffer, port.AudioFrame{StartSample: 0, PCM: []byte{1, 0, 254, 255}}); err != nil {
		t.Fatalf("写入第一场 PCM 失败：%v", err)
	}
	first, ok := buffer.Read(0, 2)
	if !ok || len(first) != 2 || first[0] != 1 || first[1] != -2 {
		t.Fatalf("PCM16 转换错误：%v ok=%t", first, ok)
	}
	if err = writeRollingFrame(buffer, port.AudioFrame{StartSample: 0, PCM: []byte{3, 0}}); err != nil {
		t.Fatalf("重置并写入下一场 PCM 失败：%v", err)
	}
	second, ok := buffer.Read(0, 1)
	if !ok || len(second) != 1 || second[0] != 3 {
		t.Fatalf("下一场 rolling 范围错误：%v ok=%t", second, ok)
	}
}
