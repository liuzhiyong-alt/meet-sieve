// Package audio 定义 MeetSieve 标准音频格式的纯领域校验与编码。
package audio

import (
	"encoding/binary"
	"fmt"
)

const (
	// SampleRate 是统一音频时间线的固定采样率。
	SampleRate = 16000
	// BitDepth 是统一 PCM 样本位深。
	BitDepth = 16
	// Channels 是统一单声道通道数。
	Channels = 1
	// WAVHeaderSize 是固定 PCM WAV header 字节数。
	WAVHeaderSize = 44
)

// DecodeCanonicalWAV 校验固定 16 kHz、16-bit、mono PCM WAV 并返回 PCM 副本。
func DecodeCanonicalWAV(data []byte) ([]byte, error) {
	if len(data) < WAVHeaderSize || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" ||
		string(data[12:16]) != "fmt " || string(data[36:40]) != "data" {
		return nil, fmt.Errorf("录音 WAV header 无效")
	}
	dataSize := int(binary.LittleEndian.Uint32(data[40:44]))
	if binary.LittleEndian.Uint32(data[4:8]) != uint32(36+dataSize) || len(data) != WAVHeaderSize+dataSize {
		return nil, fmt.Errorf("录音长度与 WAV header 不一致")
	}
	if binary.LittleEndian.Uint16(data[20:22]) != 1 ||
		binary.LittleEndian.Uint16(data[22:24]) != Channels ||
		binary.LittleEndian.Uint32(data[24:28]) != SampleRate ||
		binary.LittleEndian.Uint16(data[34:36]) != BitDepth || dataSize%2 != 0 {
		return nil, fmt.Errorf("录音 WAV 格式不一致")
	}
	return append([]byte(nil), data[WAVHeaderSize:]...), nil
}

// EncodeCanonicalWAVHeader 为指定样本数生成固定 PCM WAV header。
func EncodeCanonicalWAVHeader(sampleCount int64) ([]byte, error) {
	dataSize := sampleCount * 2
	if sampleCount < 0 || dataSize > int64(^uint32(0))-36 {
		return nil, fmt.Errorf("WAV 数据超过 RIFF 上限")
	}
	header := make([]byte, WAVHeaderSize)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], Channels)
	binary.LittleEndian.PutUint32(header[24:28], SampleRate)
	binary.LittleEndian.PutUint32(header[28:32], SampleRate*2)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], BitDepth)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))
	return header, nil
}
