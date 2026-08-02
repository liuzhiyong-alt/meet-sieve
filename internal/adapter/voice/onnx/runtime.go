// Package onnx 封装 ONNX Runtime 全局环境的校验、初始化和销毁。
package onnx

import (
	"fmt"
	"os"
	"sync"

	"meet-sieve/internal/infra/assets"
	"meet-sieve/internal/infra/filesystem"

	ort "github.com/yalue/onnxruntime_go"
)

var (
	environmentMu     sync.Mutex
	environmentActive bool
)

// Runtime 管理一次 ONNX Runtime 环境生命周期。
type Runtime struct {
	asset       assets.Asset
	libraryPath string
	started     bool
}

// NewRuntime 创建使用已锁资源的 ONNX Runtime。
func NewRuntime(asset assets.Asset, libraryPath string) *Runtime {
	return &Runtime{asset: asset, libraryPath: libraryPath}
}

// Start 校验动态库后初始化 ONNX Runtime，并返回真实运行时版本。
func (r *Runtime) Start() (string, error) {
	environmentMu.Lock()
	defer environmentMu.Unlock()

	if r.started {
		return "", fmt.Errorf("ONNX Runtime 实例已经启动")
	}
	if environmentActive || ort.IsInitialized() {
		return "", fmt.Errorf("ONNX Runtime 全局环境已被占用")
	}
	if err := verifyLibrary(r.asset, r.libraryPath); err != nil {
		return "", err
	}

	ort.SetSharedLibraryPath(r.libraryPath)
	if err := ort.InitializeEnvironment(ort.WithLogLevelWarning()); err != nil {
		return "", fmt.Errorf("初始化 ONNX Runtime 失败: %w", err)
	}
	version := ort.GetVersion()
	if version != r.asset.Version {
		_ = ort.DestroyEnvironment()
		return "", fmt.Errorf("ONNX Runtime 版本不一致: got %s, want %s", version, r.asset.Version)
	}
	r.started = true
	environmentActive = true
	return version, nil
}

// Close 幂等销毁当前实例启动的 ONNX Runtime 环境。
func (r *Runtime) Close() error {
	environmentMu.Lock()
	defer environmentMu.Unlock()

	if !r.started {
		return nil
	}
	if err := ort.DestroyEnvironment(); err != nil {
		return fmt.Errorf("销毁 ONNX Runtime 失败: %w", err)
	}
	r.started = false
	environmentActive = false
	return nil
}

// verifyLibrary 在加载前校验动态库大小和 SHA-256，阻止使用未锁定二进制。
func verifyLibrary(asset assets.Asset, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("ONNX Runtime 动态库缺失: %w", err)
		}
		return fmt.Errorf("读取 ONNX Runtime 动态库失败: %w", err)
	}
	if info.Size() != asset.LibrarySize {
		return fmt.Errorf("ONNX Runtime 动态库大小不一致")
	}
	actualSHA, err := filesystem.SHA256File(path)
	if err != nil {
		return err
	}
	if actualSHA != asset.LibrarySHA256 {
		return fmt.Errorf("ONNX Runtime 动态库 SHA-256 不一致")
	}
	return nil
}
