package onnx

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"

	"meet-sieve/internal/infra/assets"
	"meet-sieve/internal/port"

	ort "github.com/yalue/onnxruntime_go"
)

// Encoder 使用固定 CAM++ ONNX session 提取 192 维说话人 embedding。
type Encoder struct {
	asset   assets.VoiceModelAsset
	session *ort.DynamicAdvancedSession

	mu     sync.Mutex
	closed bool
}

// NewEncoder 在已初始化的 ONNX Runtime 环境中校验模型契约并创建 session。
func NewEncoder(asset assets.VoiceModelAsset, modelPath string) (*Encoder, error) {
	if !assets.VerifyFile(modelPath, asset.ModelSHA256, asset.ModelSize) {
		return nil, fmt.Errorf("CAM++ ONNX 大小或 SHA-256 不匹配")
	}
	if err := verifyModelContract(asset, modelPath); err != nil {
		return nil, err
	}
	session, err := ort.NewDynamicAdvancedSession(
		modelPath, []string{asset.InputName}, []string{asset.OutputName}, nil,
	)
	if err != nil {
		return nil, fmt.Errorf("创建 CAM++ ONNX session 失败: %w", err)
	}
	return &Encoder{asset: asset, session: session}, nil
}

// Encode 将 16kHz 单声道 PCM 转换为当前模型 embedding。
func (encoder *Encoder) Encode(ctx context.Context, pcm port.AudioPCM) (port.Embedding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pcm.SampleRate != fbankSampleRate || len(pcm.Samples) < fbankFrameSize {
		return nil, fmt.Errorf("CAM++ 需要非空 16kHz 单声道 PCM")
	}
	if !finiteSamples(pcm.Samples) {
		return nil, fmt.Errorf("PCM 包含 NaN 或 Inf")
	}
	features, err := extractFBank(pcm.Samples)
	if err != nil {
		return nil, err
	}
	frameCount := len(features) / fbankBinCount
	input, err := ort.NewTensor(ort.NewShape(1, int64(frameCount), fbankBinCount), features)
	if err != nil {
		return nil, fmt.Errorf("创建 CAM++ 输入 tensor 失败: %w", err)
	}
	defer input.Destroy()

	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	if encoder.closed || encoder.session == nil {
		return nil, fmt.Errorf("CAM++ encoder 已关闭")
	}
	outputs := []ort.Value{nil}
	if err := encoder.session.Run([]ort.Value{input}, outputs); err != nil {
		return nil, fmt.Errorf("执行 CAM++ 推理失败: %w", err)
	}
	if outputs[0] == nil {
		return nil, fmt.Errorf("CAM++ 未返回 embedding")
	}
	defer outputs[0].Destroy()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return decodeEmbedding(outputs[0], encoder.asset.EmbeddingDimension)
}

// ModelInfo 返回与 embedding 一起持久化的稳定模型四元组。
func (encoder *Encoder) ModelInfo() port.ModelInfo {
	return port.ModelInfo{
		ID: encoder.asset.ModelID, Version: encoder.asset.Version,
		Dimension: encoder.asset.EmbeddingDimension, SHA256: encoder.asset.ModelSHA256,
	}
}

// Close 幂等释放 ONNX session；调用方必须先停止新的编码请求。
func (encoder *Encoder) Close() error {
	if encoder == nil {
		return nil
	}
	encoder.mu.Lock()
	defer encoder.mu.Unlock()
	if encoder.closed {
		return nil
	}
	encoder.closed = true
	if encoder.session == nil {
		return nil
	}
	err := encoder.session.Destroy()
	encoder.session = nil
	return err
}

// verifyModelContract 校验 ONNX 节点、dtype、shape 和自描述 metadata。
func verifyModelContract(asset assets.VoiceModelAsset, modelPath string) error {
	inputs, outputs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return fmt.Errorf("读取 CAM++ 输入输出失败: %w", err)
	}
	if len(inputs) != 1 || len(outputs) != 1 ||
		!matchesTensor(inputs[0], asset.InputName, []int64{-1, -1, int64(asset.FeatureBins)}) ||
		!matchesTensor(outputs[0], asset.OutputName, []int64{-1, int64(asset.EmbeddingDimension)}) {
		return fmt.Errorf("CAM++ 输入输出契约不匹配")
	}
	metadata, err := ort.GetModelMetadata(modelPath)
	if err != nil {
		return fmt.Errorf("读取 CAM++ metadata 失败: %w", err)
	}
	defer metadata.Destroy()
	expected := map[string]string{
		"meetsieve.model_id": asset.ModelID, "meetsieve.model_version": asset.Version,
		"meetsieve.embedding_dimension": strconv.Itoa(asset.EmbeddingDimension),
		"meetsieve.sample_rate":         "16000", "meetsieve.feature": "fbank",
		"meetsieve.feature_bins": strconv.Itoa(asset.FeatureBins), "meetsieve.mean_normalization": "true",
	}
	for key, expectedValue := range expected {
		actual, exists, err := metadata.LookupCustomMetadataMap(key)
		if err != nil || !exists || actual != expectedValue {
			return fmt.Errorf("CAM++ metadata %s 不匹配", key)
		}
	}
	return nil
}

// matchesTensor 校验单个 float32 tensor 的名称和动态 shape。
func matchesTensor(info ort.InputOutputInfo, name string, dimensions []int64) bool {
	if info.Name != name || info.OrtValueType != ort.ONNXTypeTensor || info.DataType != ort.TensorElementDataTypeFloat {
		return false
	}
	if len(info.Dimensions) != len(dimensions) {
		return false
	}
	for index, expected := range dimensions {
		if info.Dimensions[index] != expected {
			return false
		}
	}
	return true
}

// decodeEmbedding 校验 ORT 动态输出并复制到不依赖 tensor 生命周期的切片。
func decodeEmbedding(value ort.Value, dimension int) (port.Embedding, error) {
	tensor, ok := value.(*ort.Tensor[float32])
	if !ok || len(tensor.GetShape()) != 2 || tensor.GetShape()[0] != 1 || tensor.GetShape()[1] != int64(dimension) {
		return nil, fmt.Errorf("CAM++ embedding shape 不正确")
	}
	data := tensor.GetData()
	if len(data) != dimension || !finiteSamples(data) {
		return nil, fmt.Errorf("CAM++ embedding 包含非法数值")
	}
	return append(port.Embedding(nil), data...), nil
}

// finiteSamples 检查音频或 embedding 不包含非有限值。
func finiteSamples(values []float32) bool {
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
	}
	return true
}
