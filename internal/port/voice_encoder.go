package port

import "context"

// AudioPCM 是声纹编码器接收的标准 PCM。
type AudioPCM struct {
	Samples    []float32
	SampleRate int
}

// Embedding 是声纹模型输出的特征向量。
type Embedding []float32

// ModelInfo 描述声纹模型稳定身份。
type ModelInfo struct {
	ID        string
	Version   string
	Dimension int
	SHA256    string
}

// VoiceEncoder 定义本地声纹特征提取能力。
type VoiceEncoder interface {
	Encode(ctx context.Context, pcm AudioPCM) (Embedding, error)
	ModelInfo() ModelInfo
}
