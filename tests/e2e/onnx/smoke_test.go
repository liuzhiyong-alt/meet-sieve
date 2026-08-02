package onnx_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"meet-sieve/internal/adapter/voice/onnx"
	"meet-sieve/internal/infra/assets"
)

// TestRuntimeSmoke_InitializesAndDestroysRealRuntime 验证官方 ONNX Runtime 动态库真实初始化和销毁。
func TestRuntimeSmoke_InitializesAndDestroysRealRuntime(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(projectRoot(t), "third_party", "assets.lock.json"))
	if err != nil {
		t.Fatalf("读取资源锁失败：%v", err)
	}
	manifest, err := assets.ParseManifest(data)
	if err != nil {
		t.Fatalf("解析资源锁失败：%v", err)
	}
	asset, err := manifest.Select("onnxruntime", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("选择当前平台资源失败：%v", err)
	}
	libraryPath := filepath.Join(
		projectRoot(t),
		".cache",
		"third_party",
		"extracted",
		asset.OS+"-"+asset.Arch,
		filepath.FromSlash(asset.LibraryPath),
	)
	environment := onnx.NewRuntime(asset, libraryPath)
	version, err := environment.Start()
	if err != nil {
		t.Fatalf("初始化真实 ONNX Runtime 失败：%v", err)
	}
	if version != asset.Version {
		t.Fatalf("运行时版本不一致：got %s, want %s", version, asset.Version)
	}
	t.Logf("ONNX Runtime 真实初始化版本：%s", version)
	if err := environment.Close(); err != nil {
		t.Fatalf("销毁真实 ONNX Runtime 失败：%v", err)
	}
	if err := environment.Close(); err != nil {
		t.Fatalf("重复销毁应幂等：%v", err)
	}
}

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
