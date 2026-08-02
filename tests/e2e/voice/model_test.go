package voice_test

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	voiceonnx "meet-sieve/internal/adapter/voice/onnx"
	"meet-sieve/internal/infra/assets"
	"meet-sieve/internal/port"
	voiceservice "meet-sieve/internal/service/voice"
)

// TestSpeakerEmbedding_RealCAMPlusONNXRuntime 验证官方模型包通过 Go FBank 与 ONNX Runtime 1.26.0 真实推理。
func TestSpeakerEmbedding_RealCAMPlusONNXRuntime(t *testing.T) {
	packagePath := os.Getenv("MEETSIEVE_VOICE_MODEL_PACKAGE")
	wavDir := os.Getenv("MEETSIEVE_VOICE_TEST_WAV_DIR")
	if packagePath == "" || wavDir == "" {
		t.Skip("未提供锁定模型包和官方 WAV 目录")
	}
	root := projectRoot(t)
	manifest := loadManifest(t, root)
	modelAsset, err := manifest.SelectVoiceModel("campplus")
	if err != nil {
		t.Fatalf("选择 CAM++ 资源失败：%v", err)
	}
	installRoot := t.TempDir()
	manager := voiceservice.NewModelManager(modelAsset, installRoot, filepath.Join(installRoot, "cache"), nil)
	if _, err := manager.Import(context.Background(), packagePath); err != nil {
		t.Fatalf("导入官方模型包失败：%v", err)
	}
	modelPath, ready := manager.ModelPath()
	if !ready {
		t.Fatal("导入后模型未就绪")
	}
	runtimeAsset, err := manifest.Select("onnxruntime", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("选择 ONNX Runtime 失败：%v", err)
	}
	libraryPath := filepath.Join(root, ".cache", "third_party", "extracted", runtimeAsset.OS+"-"+runtimeAsset.Arch, filepath.FromSlash(runtimeAsset.LibraryPath))
	environment := voiceonnx.NewRuntime(runtimeAsset, libraryPath)
	if _, err := environment.Start(); err != nil {
		t.Fatalf("启动真实 ONNX Runtime 失败：%v", err)
	}
	defer environment.Close()
	encoder, err := voiceonnx.NewEncoder(modelAsset, modelPath)
	if err != nil {
		t.Fatalf("创建真实 CAM++ encoder 失败：%v", err)
	}
	defer encoder.Close()

	embeddings := make([]port.Embedding, 0, 3)
	for _, filename := range []string{"speaker1_a_cn_16k.wav", "speaker1_b_cn_16k.wav", "speaker2_a_cn_16k.wav"} {
		embeddings = append(embeddings, encodeWAV(t, encoder, filepath.Join(wavDir, filename)))
	}
	same := cosine(embeddings[0], embeddings[1])
	different := cosine(embeddings[0], embeddings[2])
	if same < 0.5 || different > 0.1 || same-different < 0.4 {
		t.Fatalf("官方样本区分结果异常：same=%f different=%f", same, different)
	}
	repeated := encodeWAV(t, encoder, filepath.Join(wavDir, "speaker1_a_cn_16k.wav"))
	if value := cosine(embeddings[0], repeated); value < 0.999999 {
		t.Fatalf("重复编码不稳定：cosine=%f", value)
	}
	if referencePath := os.Getenv("MEETSIEVE_VOICE_REFERENCE_EMBEDDING"); referencePath != "" {
		reference := readEmbedding(t, referencePath, modelAsset.EmbeddingDimension)
		if value := cosine(embeddings[0], reference); value < 0.995 {
			t.Fatalf("Go/Python embedding 偏差过大：cosine=%f", value)
		}
		t.Logf("Go/Python embedding cosine=%f", cosine(embeddings[0], reference))
	}
	t.Logf("CAM++ Go/ORT 结果：same=%f different=%f", same, different)
}

// readEmbedding 读取 Python 官方链路生成的小端 float32 参考向量。
func readEmbedding(t *testing.T, path string, dimension int) port.Embedding {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || len(content) != dimension*4 {
		t.Fatalf("读取参考 embedding 失败：size=%d err=%v", len(content), err)
	}
	result := make(port.Embedding, dimension)
	for index := range result {
		result[index] = math.Float32frombits(binary.LittleEndian.Uint32(content[index*4:]))
	}
	return result
}

// encodeWAV 规范化官方 WAV 并转为模型所需的 [-1,1) float PCM。
func encodeWAV(t *testing.T, encoder *voiceonnx.Encoder, path string) port.Embedding {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取官方 WAV 失败：%v", err)
	}
	normalized, err := voiceservice.NormalizeWAV(context.Background(), content)
	if err != nil {
		t.Fatalf("规范化官方 WAV 失败：%v", err)
	}
	samples := make([]float32, len(normalized.Samples))
	for index, sample := range normalized.Samples {
		samples[index] = float32(sample) / 32768
	}
	embedding, err := encoder.Encode(context.Background(), port.AudioPCM{Samples: samples, SampleRate: 16000})
	if err != nil {
		t.Fatalf("编码官方 WAV 失败：%v", err)
	}
	return embedding
}

// cosine 计算两个 embedding 的余弦相似度。
func cosine(left port.Embedding, right port.Embedding) float64 {
	var dot, leftNorm, rightNorm float64
	for index := range left {
		dot += float64(left[index] * right[index])
		leftNorm += float64(left[index] * left[index])
		rightNorm += float64(right[index] * right[index])
	}
	return dot / math.Sqrt(leftNorm*rightNorm)
}

// loadManifest 读取仓库中的真实资源锁。
func loadManifest(t *testing.T, root string) assets.Manifest {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "third_party", "assets.lock.json"))
	if err != nil {
		t.Fatalf("读取资源锁失败：%v", err)
	}
	manifest, err := assets.ParseManifest(content)
	if err != nil {
		t.Fatalf("解析资源锁失败：%v", err)
	}
	return manifest
}

// projectRoot 向上查找 go.mod 得到项目根目录。
func projectRoot(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("读取当前目录失败：%v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("未找到项目根目录")
		}
		current = parent
	}
}
