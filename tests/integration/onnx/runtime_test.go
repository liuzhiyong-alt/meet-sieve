package onnx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"meet-sieve/internal/adapter/voice/onnx"
	"meet-sieve/internal/infra/assets"
)

// TestRuntime_StartRejectsMissingLibrary 验证动态库缺失时返回独立诊断。
func TestRuntime_StartRejectsMissingLibrary(t *testing.T) {
	t.Parallel()

	runtime := onnx.NewRuntime(testAsset(), filepath.Join(t.TempDir(), "missing.dylib"))
	if _, err := runtime.Start(); err == nil || !strings.Contains(err.Error(), "动态库缺失") {
		t.Fatalf("缺失动态库诊断不正确：%v", err)
	}
}

// TestRuntime_StartRejectsHashMismatch 验证动态库内容被篡改时不会尝试加载。
func TestRuntime_StartRejectsHashMismatch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "runtime.dylib")
	if err := os.WriteFile(path, []byte("wrong"), 0o600); err != nil {
		t.Fatalf("准备错误动态库失败：%v", err)
	}
	asset := testAsset()
	asset.LibrarySize = int64(len("wrong"))
	runtime := onnx.NewRuntime(asset, path)
	if _, err := runtime.Start(); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("哈希错误诊断不正确：%v", err)
	}
}

func testAsset() assets.Asset {
	return assets.Asset{
		ID:            "onnxruntime",
		Version:       "1.26.0",
		OS:            "darwin",
		Arch:          "arm64",
		LibrarySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LibrarySize:   1,
	}
}
