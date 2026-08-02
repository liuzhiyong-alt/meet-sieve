package voice_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"meet-sieve/internal/infra/apperr"
	voiceservice "meet-sieve/internal/service/voice"
)

// TestNormalizeWAV_PreservesCanonicalPCM 验证标准 16 kHz 单声道 PCM WAV 可被解析并保持样本值。
func TestNormalizeWAV_PreservesCanonicalPCM(t *testing.T) {
	inputSamples := []int16{-32768, -1024, 0, 1024, 32767}
	input := buildPCM16WAV(inputSamples, 16000, 1)

	result, err := voiceservice.NormalizeWAV(context.Background(), input)
	if err != nil {
		t.Fatalf("规范化标准 WAV 失败：%v", err)
	}
	if result.SampleRate != 16000 || result.Channels != 1 || result.BitDepth != 16 {
		t.Fatalf("规范化格式不正确：%+v", result)
	}
	if len(result.Samples) != len(inputSamples) {
		t.Fatalf("样本数量不正确：got %d want %d", len(result.Samples), len(inputSamples))
	}
	for index, sample := range inputSamples {
		if result.Samples[index] != sample {
			t.Fatalf("样本 %d 不正确：got %d want %d", index, result.Samples[index], sample)
		}
	}
	if len(result.WAV) != len(input) {
		t.Fatalf("标准输入输出大小不一致：got %d want %d", len(result.WAV), len(input))
	}
	digest := sha256.Sum256(result.WAV)
	if result.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("规范化 WAV 哈希不正确：got %s", result.SHA256)
	}
}

// TestNormalizeWAV_DownmixesStereo 验证左右声道使用确定性平均值下混为单声道。
func TestNormalizeWAV_DownmixesStereo(t *testing.T) {
	input := buildPCM16WAV([]int16{1000, -1000, 3000, 1000}, 16000, 2)

	result, err := voiceservice.NormalizeWAV(context.Background(), input)
	if err != nil {
		t.Fatalf("规范化双声道 WAV 失败：%v", err)
	}
	want := []int16{0, 2000}
	if len(result.Samples) != len(want) {
		t.Fatalf("下混样本数量不正确：got %d want %d", len(result.Samples), len(want))
	}
	for index := range want {
		if result.Samples[index] != want[index] {
			t.Fatalf("下混样本 %d 不正确：got %d want %d", index, result.Samples[index], want[index])
		}
	}
}

// TestNormalizeWAV_ConvertsPCM24 验证 24-bit PCM 按高 16 位确定性量化。
func TestNormalizeWAV_ConvertsPCM24(t *testing.T) {
	input := buildIntegerPCMWAV([]int32{-8388608, -256, 0, 256, 8388352}, 16000, 1, 24)

	result, err := voiceservice.NormalizeWAV(context.Background(), input)
	if err != nil {
		t.Fatalf("规范化 24-bit WAV 失败：%v", err)
	}
	want := []int16{-32768, -1, 0, 1, 32767}
	assertSamples(t, result.Samples, want)
}

// TestNormalizeWAV_ConvertsPCM32 验证 32-bit PCM 按高 16 位确定性量化。
func TestNormalizeWAV_ConvertsPCM32(t *testing.T) {
	input := buildIntegerPCMWAV([]int32{-2147483648, -65536, 0, 65536, 2147418112}, 16000, 1, 32)

	result, err := voiceservice.NormalizeWAV(context.Background(), input)
	if err != nil {
		t.Fatalf("规范化 32-bit WAV 失败：%v", err)
	}
	want := []int16{-32768, -1, 0, 1, 32767}
	assertSamples(t, result.Samples, want)
}

// TestNormalizeWAV_RejectsUnsupportedFormats 验证解析依据文件内容而非扩展名，并拒绝浮点编码。
func TestNormalizeWAV_RejectsUnsupportedFormats(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "伪 WAV", input: []byte("not a wav")},
		{name: "IEEE float", input: buildWAVWithFormat([]int16{0, 1}, 16000, 1, 3)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := voiceservice.NormalizeWAV(context.Background(), test.input); err == nil {
				t.Fatal("期望拒绝不支持的 WAV 格式")
			}
		})
	}
}

// TestNormalizeWAV_RejectsTruncatedChunk 验证 chunk 声明超过实际文件时不会越界读取。
func TestNormalizeWAV_RejectsTruncatedChunk(t *testing.T) {
	input := buildPCM16WAV([]int16{1, 2}, 16000, 1)
	binary.LittleEndian.PutUint32(input[40:44], 1024)

	if _, err := voiceservice.NormalizeWAV(context.Background(), input); err == nil {
		t.Fatal("期望拒绝截断的 data chunk")
	}
}

// TestNormalizeWAV_RejectsLongerThanSixtySeconds 验证过长输入被拒绝而不是静默截断。
func TestNormalizeWAV_RejectsLongerThanSixtySeconds(t *testing.T) {
	input := buildPCM16WAV(make([]int16, 16000*60+1), 16000, 1)

	if _, err := voiceservice.NormalizeWAV(context.Background(), input); apperr.Normalize(err).ErrorCode != "VOICE_DURATION_EXCEEDED" {
		t.Fatal("期望拒绝超过 60 秒的 WAV")
	}
}

// TestNormalizeWAV_ResamplesToSixteenKilohertz 验证常见采样率被确定性转换为 16 kHz。
func TestNormalizeWAV_ResamplesToSixteenKilohertz(t *testing.T) {
	input := buildPCM16WAV([]int16{0, 1000, 2000, 3000, 4000, 5000, 6000, 7000}, 8000, 1)

	first, err := voiceservice.NormalizeWAV(context.Background(), input)
	if err != nil {
		t.Fatalf("重采样 8 kHz WAV 失败：%v", err)
	}
	second, err := voiceservice.NormalizeWAV(context.Background(), input)
	if err != nil {
		t.Fatalf("重复重采样失败：%v", err)
	}
	if len(first.Samples) != 16 {
		t.Fatalf("重采样样本数不正确：got %d want 16", len(first.Samples))
	}
	if !bytes.Equal(first.WAV, second.WAV) {
		t.Fatal("同一输入重复规范化的输出不一致")
	}
}

// TestNormalizeWAV_StopsWhenContextCanceled 验证已取消任务不会继续处理输入。
func TestNormalizeWAV_StopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := voiceservice.NormalizeWAV(ctx, buildPCM16WAV([]int16{1}, 16000, 1))
	if err != context.Canceled {
		t.Fatalf("取消错误不正确：got %v want %v", err, context.Canceled)
	}
}

// assertSamples 校验规范化样本与预期值完全一致。
func assertSamples(t *testing.T, got []int16, want []int16) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("样本数量不正确：got %d want %d", len(got), len(want))
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("样本 %d 不正确：got %d want %d", index, got[index], want[index])
		}
	}
}

// buildPCM16WAV 构造不含扩展 chunk 的最小 PCM WAV 测试输入。
func buildPCM16WAV(samples []int16, sampleRate int, channels int) []byte {
	return buildWAVWithFormat(samples, sampleRate, channels, 1)
}

// buildWAVWithFormat 构造带指定 WAV format code 的 16-bit 测试输入。
func buildWAVWithFormat(samples []int16, sampleRate int, channels int, audioFormat uint16) []byte {
	dataSize := len(samples) * 2
	buffer := make([]byte, 44+dataSize)
	copy(buffer[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buffer[4:8], uint32(36+dataSize))
	copy(buffer[8:12], "WAVE")
	copy(buffer[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buffer[16:20], 16)
	binary.LittleEndian.PutUint16(buffer[20:22], audioFormat)
	binary.LittleEndian.PutUint16(buffer[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buffer[24:28], uint32(sampleRate))
	byteRate := sampleRate * channels * 2
	binary.LittleEndian.PutUint32(buffer[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buffer[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(buffer[34:36], 16)
	copy(buffer[36:40], "data")
	binary.LittleEndian.PutUint32(buffer[40:44], uint32(dataSize))
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(buffer[44+index*2:], uint16(sample))
	}
	return buffer
}

// buildIntegerPCMWAV 构造 24-bit 或 32-bit 小端整数 PCM 测试输入。
func buildIntegerPCMWAV(samples []int32, sampleRate int, channels int, bitDepth int) []byte {
	bytesPerSample := bitDepth / 8
	dataSize := len(samples) * bytesPerSample
	padding := dataSize % 2
	buffer := make([]byte, 44+dataSize+padding)
	copy(buffer[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buffer[4:8], uint32(36+dataSize+padding))
	copy(buffer[8:12], "WAVE")
	copy(buffer[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buffer[16:20], 16)
	binary.LittleEndian.PutUint16(buffer[20:22], 1)
	binary.LittleEndian.PutUint16(buffer[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buffer[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buffer[28:32], uint32(sampleRate*channels*bytesPerSample))
	binary.LittleEndian.PutUint16(buffer[32:34], uint16(channels*bytesPerSample))
	binary.LittleEndian.PutUint16(buffer[34:36], uint16(bitDepth))
	copy(buffer[36:40], "data")
	binary.LittleEndian.PutUint32(buffer[40:44], uint32(dataSize))
	for index, sample := range samples {
		offset := 44 + index*bytesPerSample
		value := uint32(sample)
		buffer[offset] = byte(value)
		buffer[offset+1] = byte(value >> 8)
		buffer[offset+2] = byte(value >> 16)
		if bytesPerSample == 4 {
			buffer[offset+3] = byte(value >> 24)
		}
	}
	return buffer
}
