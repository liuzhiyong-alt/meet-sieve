package voice_test

import (
	"math"
	"testing"

	"meet-sieve/internal/port"
	voiceservice "meet-sieve/internal/service/voice"
)

// TestEncodeEmbeddingBlob_UsesLittleEndianFloat32 验证 embedding 持久化格式稳定且可逆。
func TestEncodeEmbeddingBlob_UsesLittleEndianFloat32(t *testing.T) {
	t.Parallel()

	input := port.Embedding{1, -2.5, 0.25}
	blob, err := voiceservice.EncodeEmbeddingBlob(input, 3)
	if err != nil {
		t.Fatalf("编码 embedding 失败：%v", err)
	}
	want := []byte{0, 0, 128, 63, 0, 0, 32, 192, 0, 0, 128, 62}
	if string(blob) != string(want) {
		t.Fatalf("embedding BLOB 不正确：%v", blob)
	}
	if _, err := voiceservice.EncodeEmbeddingBlob(port.Embedding{float32(math.NaN())}, 1); err == nil {
		t.Fatal("NaN embedding 应被拒绝")
	}
}
