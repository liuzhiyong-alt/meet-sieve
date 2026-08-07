package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	voiceonnx "meet-sieve/internal/adapter/voice/onnx"
	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/assets"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/internal/port"
	voiceservice "meet-sieve/internal/service/voice"
	"meet-sieve/third_party"
)

const officialVoiceModelID = "campplus"

// VoiceModuleStatus 合并模型文件、运行时和 encoder 的当前可用状态。
type VoiceModuleStatus struct {
	Model        voiceservice.ModelStatus
	Usable       bool
	Initializing bool
}

// VoiceModule 管理独立于会议工作目录的官方模型、ORT 环境和当前 encoder。
type VoiceModule struct {
	manager    *voiceservice.ModelManager
	runtime    *voiceonnx.Runtime
	modelAsset assets.VoiceModelAsset

	mu       sync.RWMutex
	encoder  *voiceonnx.Encoder
	started  bool
	starting bool
	lastErr  error
}

// NewVoiceModule 从内嵌资源锁和平台安装目录构造声纹运行模块。
func NewVoiceModule() (*VoiceModule, error) {
	manifest, modelAsset, runtimeAsset, appDataDir, libraryPath, err := resolveVoiceModuleResources()
	_ = manifest
	if err != nil {
		return nil, err
	}
	modelRoot := filepath.Join(appDataDir, "models", "voice")
	cacheDir := filepath.Join(appDataDir, "models", "cache")
	return &VoiceModule{
		manager: voiceservice.NewModelManager(modelAsset, modelRoot, cacheDir, nil),
		runtime: voiceonnx.NewRuntime(runtimeAsset, libraryPath), modelAsset: modelAsset,
	}, nil
}

// Start 初始化锁定 ONNX Runtime；模型缺失只保持声纹不可用，不影响应用启动。
func (module *VoiceModule) Start() error {
	if module == nil || module.runtime == nil || module.manager == nil {
		return fmt.Errorf("声纹运行模块依赖未初始化")
	}
	module.mu.Lock()
	if module.started || module.starting {
		err := module.lastErr
		module.mu.Unlock()
		return err
	}
	module.starting = true
	module.mu.Unlock()
	_, err := module.runtime.Start()
	module.mu.Lock()
	module.starting = false
	module.started = err == nil
	module.lastErr = err
	module.mu.Unlock()
	if err != nil {
		return err
	}
	if _, ready := module.manager.ModelPath(); !ready {
		return nil
	}
	return module.activateInstalledModel()
}

// Stop 先释放 encoder，再销毁全局 ONNX Runtime 环境。
func (module *VoiceModule) Stop() error {
	if module == nil {
		return nil
	}
	module.mu.Lock()
	encoder := module.encoder
	module.encoder = nil
	module.started = false
	module.mu.Unlock()
	if encoder != nil {
		if err := encoder.Close(); err != nil {
			return err
		}
	}
	return module.runtime.Close()
}

// Download 下载并安装唯一锁定官方包，使用设置页保存的本机代理端口，成功后立即激活 encoder。
func (module *VoiceModule) Download(ctx context.Context, proxyPort int) (VoiceModuleStatus, error) {
	client, err := assets.NewLocalProxyHTTPClient(proxyPort)
	if err != nil {
		return module.Status(), apperr.Dependency(apperr.CodeVoiceModelUnavailable, err, apperr.WithOp("voice.model.download"))
	}
	if _, err := module.manager.DownloadWithClient(ctx, client); err != nil {
		return module.Status(), apperr.Dependency(apperr.CodeVoiceModelUnavailable, err, apperr.WithOp("voice.model.download"))
	}
	if err := module.activateInstalledModel(); err != nil {
		return module.Status(), apperr.Dependency(apperr.CodeVoiceModelUnavailable, err, apperr.WithOp("voice.model.activate"))
	}
	return module.Status(), nil
}

// Import 安装用户选择的同一官方离线包，成功后立即激活 encoder。
func (module *VoiceModule) Import(ctx context.Context, archivePath string) (VoiceModuleStatus, error) {
	if _, err := module.manager.Import(ctx, archivePath); err != nil {
		return module.Status(), apperr.Dependency(apperr.CodeVoiceModelUnavailable, err, apperr.WithOp("voice.model.import"))
	}
	if err := module.activateInstalledModel(); err != nil {
		return module.Status(), apperr.Dependency(apperr.CodeVoiceModelUnavailable, err, apperr.WithOp("voice.model.activate"))
	}
	return module.Status(), nil
}

// Encoder 返回当前可用编码器，不触发下载、扫描或自动回退。
func (module *VoiceModule) Encoder() (port.VoiceEncoder, error) {
	if module == nil {
		return nil, apperr.Dependency(apperr.CodeVoiceModelUnavailable, nil, apperr.WithOp("voice.model.encoder"))
	}
	module.mu.RLock()
	encoder := module.encoder
	module.mu.RUnlock()
	if encoder == nil {
		return nil, apperr.Dependency(apperr.CodeVoiceModelUnavailable, nil, apperr.WithOp("voice.model.encoder"))
	}
	return encoder, nil
}

// Status 返回设置页使用的无副作用状态快照。
func (module *VoiceModule) Status() VoiceModuleStatus {
	if module == nil || module.manager == nil {
		return VoiceModuleStatus{Model: voiceservice.ModelStatus{State: voiceservice.ModelStateUnavailable}}
	}
	module.mu.RLock()
	usable := module.started && module.encoder != nil && module.lastErr == nil
	initializing := module.starting
	module.mu.RUnlock()
	return VoiceModuleStatus{Model: module.manager.Status(), Usable: usable, Initializing: initializing}
}

// activateInstalledModel 校验当前文件并原子替换 encoder；旧 encoder 在切换后释放。
func (module *VoiceModule) activateInstalledModel() error {
	module.mu.RLock()
	started := module.started
	module.mu.RUnlock()
	if !started {
		return fmt.Errorf("ONNX Runtime 尚未就绪")
	}
	modelPath, ready := module.manager.ModelPath()
	if !ready {
		return fmt.Errorf("官方声纹模型尚未安装")
	}
	encoder, err := voiceonnx.NewEncoder(module.modelAsset, modelPath)
	if err != nil {
		module.mu.Lock()
		module.lastErr = err
		module.mu.Unlock()
		return err
	}
	module.mu.Lock()
	previous := module.encoder
	module.encoder = encoder
	module.lastErr = nil
	module.mu.Unlock()
	if previous != nil {
		return previous.Close()
	}
	return nil
}

// resolveVoiceModuleResources 解析内嵌资产、每用户目录和平台动态库路径。
func resolveVoiceModuleResources() (assets.Manifest, assets.VoiceModelAsset, assets.Asset, string, string, error) {
	manifest, err := assets.ParseManifest(thirdparty.AssetsLockJSON)
	if err != nil {
		return assets.Manifest{}, assets.VoiceModelAsset{}, assets.Asset{}, "", "", err
	}
	modelAsset, err := manifest.SelectVoiceModel(officialVoiceModelID)
	if err != nil {
		return manifest, assets.VoiceModelAsset{}, assets.Asset{}, "", "", err
	}
	runtimeAsset, err := manifest.Select("onnxruntime", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return manifest, modelAsset, assets.Asset{}, "", "", err
	}
	appDataDir, err := filesystem.CurrentAppDataDir()
	if err != nil {
		return manifest, modelAsset, runtimeAsset, "", "", err
	}
	installRoot, err := filesystem.CurrentInstallRoot()
	if err != nil {
		return manifest, modelAsset, runtimeAsset, appDataDir, "", err
	}
	libraryPath := filesystem.ResolveBundledONNXLibrary(installRoot.String(), runtime.GOOS)
	if _, err := os.Stat(libraryPath); err != nil && buildinfo.Current().BuildMode == "development" {
		libraryPath = developmentONNXLibrary(runtimeAsset)
	}
	return manifest, modelAsset, runtimeAsset, appDataDir, libraryPath, nil
}

// developmentONNXLibrary 只查找仓库开发命令的固定 cache 相对位置。
func developmentONNXLibrary(asset assets.Asset) string {
	workingDirectory, _ := os.Getwd()
	for _, root := range []string{workingDirectory, filepath.Clean(filepath.Join(workingDirectory, "..", ".."))} {
		candidate := filepath.Join(root, ".cache", "third_party", "extracted", asset.OS+"-"+asset.Arch, filepath.FromSlash(asset.LibraryPath))
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(workingDirectory, ".missing-onnx-runtime")
}
