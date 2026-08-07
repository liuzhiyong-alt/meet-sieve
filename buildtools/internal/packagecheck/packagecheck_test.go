package packagecheck

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyLockedFile 验证资源大小和哈希必须同时匹配。
func TestVerifyLockedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "runtime.dll")
	content := []byte("locked-runtime")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("写入资源失败：%v", err)
	}
	digest := sha256.Sum256(content)
	expected := hex.EncodeToString(digest[:])
	if err := VerifyLockedFile(path, int64(len(content)), expected); err != nil {
		t.Fatalf("正确资源不应失败：%v", err)
	}
	if err := VerifyLockedFile(path, int64(len(content)+1), expected); err == nil || !strings.Contains(err.Error(), "大小") {
		t.Fatalf("错误大小应被拒绝：%v", err)
	}
}

// TestRejectEmbeddedVoiceModel 验证安装资源内的模型权重被拒绝。
func TestRejectEmbeddedVoiceModel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "model.onnx"), []byte("weight"), 0o600); err != nil {
		t.Fatalf("写入模型失败：%v", err)
	}
	if err := RejectEmbeddedVoiceModel(root); err == nil || !strings.Contains(err.Error(), "声纹模型") {
		t.Fatalf("模型权重应被拒绝：%v", err)
	}
}
