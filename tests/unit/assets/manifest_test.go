package assets_test

import (
	"testing"

	"meet-sieve/internal/infra/assets"
)

const validManifest = `{
  "schema_version": 1,
  "assets": [{
    "id": "onnxruntime",
    "version": "1.26.0",
    "os": "darwin",
    "arch": "arm64",
    "url": "https://github.com/example/resource.tgz",
    "archive_type": "tgz",
    "archive_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "archive_size": 10,
    "library_path": "lib/runtime.dylib",
    "library_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "library_size": 5,
    "license_path": "LICENSE",
    "license": "MIT"
  }]
}`

// TestParseManifest_SelectsSupportedPlatform 验证严格资源锁可以选择已登记平台。
func TestParseManifest_SelectsSupportedPlatform(t *testing.T) {
	t.Parallel()

	manifest, err := assets.ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("解析合法资源锁失败：%v", err)
	}
	asset, err := manifest.Select("onnxruntime", "darwin", "arm64")
	if err != nil {
		t.Fatalf("选择资源失败：%v", err)
	}
	if asset.Version != "1.26.0" {
		t.Fatalf("资源版本不正确：%s", asset.Version)
	}
}

// TestParseManifest_RejectsUnknownField 验证未知字段不会被静默忽略。
func TestParseManifest_RejectsUnknownField(t *testing.T) {
	t.Parallel()

	data := []byte(`{"schema_version":1,"unexpected":true,"assets":[]}`)
	if _, err := assets.ParseManifest(data); err == nil {
		t.Fatal("未知字段应解析失败")
	}
}

// TestManifest_SelectRejectsUnsupportedPlatform 验证未登记平台会明确失败。
func TestManifest_SelectRejectsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	manifest, err := assets.ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("解析合法资源锁失败：%v", err)
	}
	if _, err := manifest.Select("onnxruntime", "windows", "amd64"); err == nil {
		t.Fatal("未登记平台应选择失败")
	}
}

// TestParseManifest_SelectsLockedVoiceModel 验证平台无关声纹模型包必须携带完整模型身份。
func TestParseManifest_SelectsLockedVoiceModel(t *testing.T) {
	t.Parallel()

	data := []byte(`{
  "schema_version": 2,
  "assets": [{
    "id": "onnxruntime", "version": "1.26.0", "os": "darwin", "arch": "arm64",
    "url": "https://github.com/example/runtime.tgz", "archive_type": "tgz",
    "archive_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "archive_size": 10,
    "library_path": "lib/runtime.dylib", "library_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "library_size": 5, "license_path": "LICENSE", "license": "MIT"
  }],
  "voice_models": [{
    "id": "campplus", "version": "1.0.0-ms1",
    "model_id": "iic/speech_campplus_sv_zh-cn_16k-common", "upstream_version": "v1.0.0",
    "upstream_revision": "065629c313eaf1a01c65c640c46d77e61e9607b4",
    "upstream_checkpoint_sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
    "url": "https://github.com/example/releases/download/voice-model-campplus-1.0.0-ms1/meetsieve-voice-campplus-1.0.0-ms1.zip",
    "archive_sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "archive_size": 20,
    "model_path": "model.onnx", "model_sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
    "model_size": 15, "manifest_path": "manifest.json",
    "manifest_sha256": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
    "license_path": "LICENSE", "license_sha256": "1111111111111111111111111111111111111111111111111111111111111111",
    "notice_path": "NOTICE", "notice_sha256": "2222222222222222222222222222222222222222222222222222222222222222",
    "license": "Apache-2.0", "opset": 11, "input_name": "feature", "output_name": "embedding",
    "feature_bins": 80, "embedding_dimension": 192
  }]
}`)

	manifest, err := assets.ParseManifest(data)
	if err != nil {
		t.Fatalf("解析声纹模型资源锁失败：%v", err)
	}
	model, err := manifest.SelectVoiceModel("campplus")
	if err != nil {
		t.Fatalf("选择声纹模型失败：%v", err)
	}
	if model.EmbeddingDimension != 192 || model.ModelPath != "model.onnx" {
		t.Fatalf("声纹模型契约不正确：%+v", model)
	}
}
