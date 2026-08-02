package onnx

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"testing"

	voiceservice "meet-sieve/internal/service/voice"
)

// TestExtractFBank_MatchesOfficialTorchaudioReference 验证特征与 3D-Speaker 正式推理链路一致。
func TestExtractFBank_MatchesOfficialTorchaudioReference(t *testing.T) {
	t.Parallel()

	samples := make([]float32, 16000)
	for index := range samples {
		samples[index] = float32(index%97-48) / 100
	}
	features, err := extractFBank(samples)
	if err != nil {
		t.Fatalf("提取 FBank 失败：%v", err)
	}
	if len(features) != 98*80 {
		t.Fatalf("FBank shape 不正确：got %d, want %d", len(features), 98*80)
	}
	references := map[int]float32{
		0:          -0.2749509811,
		10:         0.0003485680,
		10*80 + 20: 0.0005378723,
		73*80 + 8:  -1.9066338539,
	}
	for index, expected := range references {
		if difference := math.Abs(float64(features[index] - expected)); difference > 2e-4 {
			t.Fatalf("FBank[%d] 与官方参考不一致：got %.9f want %.9f diff %.9f", index, features[index], expected, difference)
		}
	}
}

// TestExtractFBank_OfficialWAVReference 用临时 Python 参考向量定位全量特征偏差。
func TestExtractFBank_OfficialWAVReference(t *testing.T) {
	wavPath := os.Getenv("MEETSIEVE_FBANK_WAV")
	referencePath := os.Getenv("MEETSIEVE_FBANK_REFERENCE")
	if wavPath == "" || referencePath == "" {
		t.Skip("未提供官方 WAV 与 FBank 参考向量")
	}
	wav, err := os.ReadFile(wavPath)
	if err != nil {
		t.Fatalf("读取 WAV 失败：%v", err)
	}
	normalized, err := voiceservice.NormalizeWAV(context.Background(), wav)
	if err != nil {
		t.Fatalf("规范化 WAV 失败：%v", err)
	}
	samples := make([]float32, len(normalized.Samples))
	for index, sample := range normalized.Samples {
		samples[index] = float32(sample) / 32768
	}
	actual, err := extractFBank(samples)
	if err != nil {
		t.Fatalf("提取 FBank 失败：%v", err)
	}
	referenceBytes, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("读取参考向量失败：%v", err)
	}
	if len(referenceBytes) != len(actual)*4 {
		t.Fatalf("参考向量长度不一致：got %d want %d", len(referenceBytes), len(actual)*4)
	}
	maxDifference := 0.0
	maxIndex := 0
	for index, value := range actual {
		expected := math.Float32frombits(binary.LittleEndian.Uint32(referenceBytes[index*4:]))
		difference := math.Abs(float64(value - expected))
		if difference > maxDifference {
			maxDifference = difference
			maxIndex = index
		}
	}
	if maxDifference > 2e-4 {
		t.Fatalf("全量 FBank 偏差过大：index=%d diff=%f", maxIndex, maxDifference)
	}
}
