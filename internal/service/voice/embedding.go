package voice

import (
	"encoding/binary"
	"fmt"
	"math"

	"meet-sieve/internal/port"
)

// EncodeEmbeddingBlob 将有限 float32 向量编码为小端 IEEE 754 连续字节。
func EncodeEmbeddingBlob(embedding port.Embedding, dimension int) ([]byte, error) {
	if dimension <= 0 || len(embedding) != dimension {
		return nil, fmt.Errorf("embedding 维度不正确")
	}
	result := make([]byte, dimension*4)
	for index, value := range embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, fmt.Errorf("embedding 包含 NaN 或 Inf")
		}
		binary.LittleEndian.PutUint32(result[index*4:], math.Float32bits(value))
	}
	return result, nil
}

// DecodeEmbeddingBlob 严格解码数据库中的小端 float32 embedding。
func DecodeEmbeddingBlob(blob []byte, dimension int) (port.Embedding, error) {
	if dimension <= 0 || len(blob) != dimension*4 {
		return nil, fmt.Errorf("embedding BLOB 长度不正确")
	}
	result := make(port.Embedding, dimension)
	for index := range result {
		value := math.Float32frombits(binary.LittleEndian.Uint32(blob[index*4:]))
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, fmt.Errorf("embedding BLOB 包含 NaN 或 Inf")
		}
		result[index] = value
	}
	return result, nil
}
