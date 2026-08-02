package voice_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/assets"
	voiceservice "meet-sieve/internal/service/voice"
)

// TestModelManager_ImportsOnlyLockedOfficialPackage 验证离线导入按包和模型双重哈希原子安装。
func TestModelManager_ImportsOnlyLockedOfficialPackage(t *testing.T) {
	t.Parallel()

	archive, modelAsset := buildModelPackage(t)
	archivePath := filepath.Join(t.TempDir(), "official.zip")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatalf("写入模型包夹具失败：%v", err)
	}
	root := t.TempDir()
	manager := voiceservice.NewModelManager(modelAsset, root, filepath.Join(root, "cache"), nil)

	status, err := manager.Import(context.Background(), archivePath)
	if err != nil {
		t.Fatalf("导入官方模型包失败：%v", err)
	}
	if status.State != voiceservice.ModelStateReady || status.ModelVersion != modelAsset.Version {
		t.Fatalf("安装状态不正确：%+v", status)
	}
	modelPath := filepath.Join(root, modelAsset.ID, modelAsset.Version, modelAsset.ModelPath)
	if content, err := os.ReadFile(modelPath); err != nil || string(content) != "locked-model" {
		t.Fatalf("模型文件未原子安装：content=%q err=%v", content, err)
	}

	corruptPath := filepath.Join(t.TempDir(), "corrupt.zip")
	corrupt := append([]byte(nil), archive...)
	corrupt[len(corrupt)-1] ^= 0xff
	if err := os.WriteFile(corruptPath, corrupt, 0o600); err != nil {
		t.Fatalf("写入损坏模型包失败：%v", err)
	}
	if _, err := manager.Import(context.Background(), corruptPath); err == nil {
		t.Fatal("损坏模型包不应被导入")
	}
	if status := manager.Status(); status.State != voiceservice.ModelStateReady {
		t.Fatalf("失败导入不应破坏已有模型：%+v", status)
	}
}

// buildModelPackage 创建与资源锁一致的最小官方包夹具。
func buildModelPackage(t *testing.T) ([]byte, assets.VoiceModelAsset) {
	t.Helper()
	model := []byte("locked-model")
	modelSHA := sha256Hex(model)
	manifest := []byte(`{
  "schema_version": 1,
  "model_id": "model-id",
  "model_version": "1.0.0",
  "model_file": "model.onnx",
  "model_sha256": "` + modelSHA + `",
  "model_size_bytes": 12,
  "license": "Apache-2.0",
  "license_file": "LICENSE",
  "notice_file": "NOTICE",
  "opset": 11,
  "input": {"name":"feature","dtype":"float32","shape":["batch","frames",80]},
  "output": {"name":"embedding","dtype":"float32","shape":["batch",192]}
}`)
	entries := map[string][]byte{
		"model.onnx": model, "manifest.json": manifest,
		"LICENSE": []byte("license"), "NOTICE": []byte("notice"),
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range []string{"LICENSE", "NOTICE", "manifest.json", "model.onnx"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("创建 ZIP 条目失败：%v", err)
		}
		if _, err := entry.Write(entries[name]); err != nil {
			t.Fatalf("写入 ZIP 条目失败：%v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 ZIP 失败：%v", err)
	}
	archive := buffer.Bytes()
	return archive, assets.VoiceModelAsset{
		ID: "campplus", Version: "1.0.0", ModelID: "model-id",
		URL:           "https://github.com/example/releases/download/model/model.zip",
		ArchiveSHA256: sha256Hex(archive), ArchiveSize: int64(len(archive)),
		ModelPath: "model.onnx", ModelSHA256: modelSHA, ModelSize: int64(len(model)),
		ManifestPath: "manifest.json", ManifestSHA256: sha256Hex(manifest),
		LicensePath: "LICENSE", LicenseSHA256: sha256Hex(entries["LICENSE"]),
		NoticePath: "NOTICE", NoticeSHA256: sha256Hex(entries["NOTICE"]),
		License: "Apache-2.0", Opset: 11, InputName: "feature", OutputName: "embedding",
		FeatureBins: 80, EmbeddingDimension: 192,
	}
}

// sha256Hex 返回测试内容的小写 SHA-256。
func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
