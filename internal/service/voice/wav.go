// Package voice 编排会前声纹样本的解析、质量检测、持久化与编码流程。
package voice

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"meet-sieve/internal/infra/apperr"
)

const (
	canonicalSampleRate = 16000
	canonicalChannels   = 1
	canonicalBitDepth   = 16
	maxWAVDurationSecs  = 60
)

// NormalizedWAV 是统一为 16 kHz、16-bit、单声道后的声纹样本。
type NormalizedWAV struct {
	// WAV 是可直接落盘的标准 PCM WAV 字节。
	WAV []byte
	// Samples 是与 WAV data chunk 一致的有符号 16-bit 样本。
	Samples []int16
	// SampleRate 固定为 16000 Hz。
	SampleRate int
	// Channels 固定为单声道。
	Channels int
	// BitDepth 固定为 16-bit。
	BitDepth int
	// DurationMS 是按输出样本数计算的毫秒时长。
	DurationMS int64
	// SHA256 是规范化后正式 WAV 的小写十六进制摘要。
	SHA256 string
}

// NormalizeWAV 解析受控 PCM WAV，并输出声纹链路使用的统一格式。
func NormalizeWAV(ctx context.Context, input []byte) (NormalizedWAV, error) {
	if err := ctx.Err(); err != nil {
		return NormalizedWAV{}, err
	}
	format, data, err := parseWAV(input)
	if err != nil {
		return NormalizedWAV{}, apperr.Biz(apperr.CodeVoiceWAVInvalid, apperr.WithOp("voice.wav.parse"))
	}
	if !isSupportedPCMFormat(format) {
		return NormalizedWAV{}, apperr.Biz(apperr.CodeVoiceWAVInvalid, apperr.WithOp("voice.wav.format"))
	}
	frameCount := len(data) / format.blockAlign
	if int64(frameCount) > int64(format.sampleRate)*maxWAVDurationSecs {
		return NormalizedWAV{}, apperr.Biz(apperr.CodeVoiceDurationExceeded, apperr.WithOp("voice.wav.duration"))
	}
	samples, err := decodeIntegerPCM(ctx, data, format.bitDepth)
	if err != nil {
		return NormalizedWAV{}, err
	}
	if format.channels == 2 {
		samples = downmixStereo(samples)
	}
	samples, err = resamplePCM16(ctx, samples, format.sampleRate, canonicalSampleRate)
	if err != nil {
		return NormalizedWAV{}, err
	}
	if len(samples) == 0 {
		return NormalizedWAV{}, apperr.Biz(apperr.CodeVoiceWAVInvalid, apperr.WithOp("voice.wav.empty"))
	}
	result := NormalizedWAV{
		Samples:    samples,
		SampleRate: canonicalSampleRate,
		Channels:   canonicalChannels,
		BitDepth:   canonicalBitDepth,
		DurationMS: int64(len(samples)) * 1000 / canonicalSampleRate,
	}
	result.WAV = encodePCM16WAV(samples, result.SampleRate)
	digest := sha256.Sum256(result.WAV)
	result.SHA256 = hex.EncodeToString(digest[:])
	return result, nil
}

type wavFormat struct {
	audioFormat uint16
	channels    int
	sampleRate  int
	byteRate    int
	bitDepth    int
	blockAlign  int
}

// isSupportedPCMFormat 校验整数 PCM 的基础格式字段及常见声纹采样率。
func isSupportedPCMFormat(format wavFormat) bool {
	if format.audioFormat != 1 || (format.channels != 1 && format.channels != 2) {
		return false
	}
	if format.bitDepth != 16 && format.bitDepth != 24 && format.bitDepth != 32 {
		return false
	}
	if !isSupportedSampleRate(format.sampleRate) {
		return false
	}
	expectedAlign := format.channels * format.bitDepth / 8
	return format.blockAlign == expectedAlign && format.byteRate == format.sampleRate*expectedAlign
}

// isSupportedSampleRate 限定桌面录音和常见音频文件会出现的采样率。
func isSupportedSampleRate(sampleRate int) bool {
	switch sampleRate {
	case 8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 88200, 96000:
		return true
	default:
		return false
	}
}

// parseWAV 按 RIFF chunk 边界读取 fmt 与 data，拒绝截断或越界声明。
func parseWAV(input []byte) (wavFormat, []byte, error) {
	if len(input) < 12 || string(input[:4]) != "RIFF" || string(input[8:12]) != "WAVE" {
		return wavFormat{}, nil, fmt.Errorf("文件不是有效的 RIFF/WAVE")
	}
	declaredSize := uint64(binary.LittleEndian.Uint32(input[4:8])) + 8
	if declaredSize != uint64(len(input)) {
		return wavFormat{}, nil, fmt.Errorf("WAV 文件长度与 RIFF 声明不一致")
	}
	var format wavFormat
	var data []byte
	for offset := 12; offset+8 <= len(input); {
		chunkSize := int(binary.LittleEndian.Uint32(input[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + chunkSize
		if chunkSize < 0 || chunkEnd < chunkStart || chunkEnd > len(input) {
			return wavFormat{}, nil, fmt.Errorf("WAV chunk 长度越界")
		}
		switch string(input[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 {
				return wavFormat{}, nil, fmt.Errorf("WAV fmt chunk 不完整")
			}
			format = wavFormat{
				audioFormat: binary.LittleEndian.Uint16(input[chunkStart : chunkStart+2]),
				channels:    int(binary.LittleEndian.Uint16(input[chunkStart+2 : chunkStart+4])),
				sampleRate:  int(binary.LittleEndian.Uint32(input[chunkStart+4 : chunkStart+8])),
				byteRate:    int(binary.LittleEndian.Uint32(input[chunkStart+8 : chunkStart+12])),
				bitDepth:    int(binary.LittleEndian.Uint16(input[chunkStart+14 : chunkStart+16])),
				blockAlign:  int(binary.LittleEndian.Uint16(input[chunkStart+12 : chunkStart+14])),
			}
		case "data":
			data = append([]byte(nil), input[chunkStart:chunkEnd]...)
		}
		offset = chunkEnd + chunkSize%2
	}
	if format.sampleRate == 0 || format.blockAlign <= 0 || data == nil || len(data)%format.blockAlign != 0 {
		return wavFormat{}, nil, fmt.Errorf("WAV 缺少有效 fmt 或 data chunk")
	}
	return format, data, nil
}

// decodeIntegerPCM 将小端整数 PCM 转为 16-bit，并在长输入中响应取消。
func decodeIntegerPCM(ctx context.Context, data []byte, bitDepth int) ([]int16, error) {
	bytesPerSample := bitDepth / 8
	if bytesPerSample < 2 || len(data)%bytesPerSample != 0 {
		return nil, fmt.Errorf("PCM 样本边界不完整")
	}
	samples := make([]int16, len(data)/bytesPerSample)
	for index := range samples {
		if index%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		offset := index * bytesPerSample
		switch bitDepth {
		case 16:
			samples[index] = int16(binary.LittleEndian.Uint16(data[offset:]))
		case 24:
			value := int32(uint32(data[offset]) | uint32(data[offset+1])<<8 | uint32(data[offset+2])<<16)
			value = value << 8 >> 8
			samples[index] = int16(value >> 8)
		case 32:
			samples[index] = int16(int32(binary.LittleEndian.Uint32(data[offset:])) >> 16)
		}
	}
	return samples, nil
}

// downmixStereo 按帧平均左右声道，并使用 int32 避免相加溢出。
func downmixStereo(interleaved []int16) []int16 {
	mono := make([]int16, len(interleaved)/2)
	for index := range mono {
		left := int32(interleaved[index*2])
		right := int32(interleaved[index*2+1])
		mono[index] = int16((left + right) / 2)
	}
	return mono
}

// encodePCM16WAV 生成不含扩展 chunk 的确定性标准 WAV。
func encodePCM16WAV(samples []int16, sampleRate int) []byte {
	dataSize := len(samples) * 2
	output := make([]byte, 44+dataSize)
	copy(output[0:4], "RIFF")
	binary.LittleEndian.PutUint32(output[4:8], uint32(36+dataSize))
	copy(output[8:12], "WAVE")
	copy(output[12:16], "fmt ")
	binary.LittleEndian.PutUint32(output[16:20], 16)
	binary.LittleEndian.PutUint16(output[20:22], 1)
	binary.LittleEndian.PutUint16(output[22:24], canonicalChannels)
	binary.LittleEndian.PutUint32(output[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(output[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(output[32:34], 2)
	binary.LittleEndian.PutUint16(output[34:36], canonicalBitDepth)
	copy(output[36:40], "data")
	binary.LittleEndian.PutUint32(output[40:44], uint32(dataSize))
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(output[44+index*2:], uint16(sample))
	}
	return output
}
