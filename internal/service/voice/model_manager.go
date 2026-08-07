package voice

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"meet-sieve/internal/infra/assets"
)

const maxModelMetadataSize = 64 * 1024

// ModelState 描述官方声纹模型在本机的安装状态。
type ModelState string

const (
	// ModelStateUnavailable 表示模型尚未安装。
	ModelStateUnavailable ModelState = "unavailable"
	// ModelStateInstalling 表示官方包正在下载、校验或安装。
	ModelStateInstalling ModelState = "installing"
	// ModelStateReady 表示当前模型已通过完整性校验。
	ModelStateReady ModelState = "ready"
	// ModelStateCorrupt 表示安装目录存在但内容不符合锁定身份。
	ModelStateCorrupt ModelState = "corrupt"
)

// ModelStatus 是设置页可安全展示的官方模型状态。
type ModelStatus struct {
	State        ModelState
	ModelID      string
	ModelVersion string
	ModelSize    int64
	InstallPath  string
}

// ModelManager 统一实现官方下载、离线导入和启动复验。
type ModelManager struct {
	asset      assets.VoiceModelAsset
	rootDir    string
	cacheDir   string
	downloader *assets.Downloader

	operationMu sync.Mutex
	stateMu     sync.RWMutex
	installing  bool
}

// NewModelManager 创建只接受一个内置锁定模型包的管理器。
func NewModelManager(asset assets.VoiceModelAsset, rootDir string, cacheDir string, client *http.Client) *ModelManager {
	return &ModelManager{
		asset: asset, rootDir: rootDir, cacheDir: cacheDir,
		downloader: assets.NewDownloader(client),
	}
}

// Status 重新核验本机模型；不会联网或修改文件。
func (manager *ModelManager) Status() ModelStatus {
	if manager == nil {
		return ModelStatus{State: ModelStateUnavailable}
	}
	manager.stateMu.RLock()
	installing := manager.installing
	manager.stateMu.RUnlock()
	if installing {
		return manager.status(ModelStateInstalling)
	}
	target := manager.installDir()
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return manager.status(ModelStateUnavailable)
	}
	if err := manager.verifyInstall(target); err != nil {
		return manager.status(ModelStateCorrupt)
	}
	return manager.status(ModelStateReady)
}

// Download 下载锁定 GitHub Release 包，并复用离线导入的同一安装门禁。
func (manager *ModelManager) Download(ctx context.Context) (ModelStatus, error) {
	if manager == nil {
		return ModelStatus{}, fmt.Errorf("声纹模型管理器尚未准备")
	}
	return manager.download(ctx, manager.downloader)
}

// DownloadWithClient 使用调用方指定的 HTTP 客户端下载锁定官方包。
func (manager *ModelManager) DownloadWithClient(ctx context.Context, client *http.Client) (ModelStatus, error) {
	if manager == nil {
		return ModelStatus{}, fmt.Errorf("声纹模型管理器尚未准备")
	}
	return manager.download(ctx, assets.NewDownloader(client))
}

// download 串行执行下载、校验和安装，避免并发操作覆盖同一模型目录。
func (manager *ModelManager) download(ctx context.Context, downloader *assets.Downloader) (ModelStatus, error) {
	if downloader == nil {
		return ModelStatus{}, fmt.Errorf("声纹模型下载器尚未准备")
	}
	manager.operationMu.Lock()
	defer manager.operationMu.Unlock()
	manager.setInstalling(true)
	defer manager.setInstalling(false)

	archivePath, err := downloader.FetchVoiceModel(ctx, manager.asset, manager.cacheDir)
	if err != nil {
		return manager.status(ModelStateUnavailable), fmt.Errorf("下载官方声纹模型失败: %w", err)
	}
	return manager.install(ctx, archivePath)
}

// Import 校验并安装用户选择的同一字节级官方离线模型包。
func (manager *ModelManager) Import(ctx context.Context, archivePath string) (ModelStatus, error) {
	if manager == nil {
		return ModelStatus{}, fmt.Errorf("声纹模型管理器尚未准备")
	}
	manager.operationMu.Lock()
	defer manager.operationMu.Unlock()
	manager.setInstalling(true)
	defer manager.setInstalling(false)
	return manager.install(ctx, archivePath)
}

// ModelPath 返回已校验模型路径；不可用时不返回猜测路径。
func (manager *ModelManager) ModelPath() (string, bool) {
	if status := manager.Status(); status.State == ModelStateReady {
		return filepath.Join(manager.installDir(), manager.asset.ModelPath), true
	}
	return "", false
}

// install 在包哈希通过后解压到同卷 staging，再原子切换当前版本目录。
func (manager *ModelManager) install(ctx context.Context, archivePath string) (ModelStatus, error) {
	if err := ctx.Err(); err != nil {
		return manager.Status(), err
	}
	if !assets.VerifyFile(archivePath, manager.asset.ArchiveSHA256, manager.asset.ArchiveSize) {
		return manager.Status(), fmt.Errorf("官方模型包大小或 SHA-256 校验失败")
	}
	parent := filepath.Dir(manager.installDir())
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return manager.Status(), fmt.Errorf("创建声纹模型目录失败: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".install-*")
	if err != nil {
		return manager.Status(), fmt.Errorf("创建模型 staging 目录失败: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := manager.extractAndVerify(archivePath, staging); err != nil {
		return manager.Status(), err
	}
	if err := ctx.Err(); err != nil {
		return manager.Status(), err
	}
	if err := replaceDirectory(staging, manager.installDir()); err != nil {
		return manager.Status(), fmt.Errorf("切换声纹模型目录失败: %w", err)
	}
	return manager.status(ModelStateReady), nil
}

// extractAndVerify 只接收四个根级普通文件，并验证 manifest 与 ONNX 双重身份。
func (manager *ModelManager) extractAndVerify(archivePath string, staging string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开官方模型包失败: %w", err)
	}
	defer reader.Close()
	expected := manager.expectedEntries()
	if len(reader.File) != len(expected) {
		return fmt.Errorf("官方模型包文件数量不正确")
	}
	for _, entry := range reader.File {
		limit, exists := expected[entry.Name]
		if !exists || entry.FileInfo().IsDir() || !entry.Mode().IsRegular() || entry.UncompressedSize64 > uint64(limit) {
			return fmt.Errorf("官方模型包包含不允许的条目: %s", entry.Name)
		}
		if err := extractModelEntry(entry, filepath.Join(staging, entry.Name), limit); err != nil {
			return err
		}
		delete(expected, entry.Name)
	}
	if len(expected) != 0 {
		return fmt.Errorf("官方模型包缺少必要文件")
	}
	return manager.verifyInstall(staging)
}

// verifyInstall 核验安装目录的四个文件、manifest 契约和模型哈希。
func (manager *ModelManager) verifyInstall(directory string) error {
	lockedFiles := map[string]string{
		manager.asset.ManifestPath: manager.asset.ManifestSHA256,
		manager.asset.LicensePath:  manager.asset.LicenseSHA256,
		manager.asset.NoticePath:   manager.asset.NoticeSHA256,
	}
	for name, expectedSHA := range lockedFiles {
		path := filepath.Join(directory, name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || !assets.VerifyFile(path, expectedSHA, info.Size()) {
			return fmt.Errorf("模型安装文件 %s 缺失或损坏", name)
		}
	}
	if !assets.VerifyFile(filepath.Join(directory, manager.asset.ModelPath), manager.asset.ModelSHA256, manager.asset.ModelSize) {
		return fmt.Errorf("已安装 ONNX 大小或 SHA-256 不匹配")
	}
	content, err := os.ReadFile(filepath.Join(directory, manager.asset.ManifestPath))
	if err != nil {
		return fmt.Errorf("读取模型 manifest 失败: %w", err)
	}
	return verifyPackageManifest(content, manager.asset)
}

// expectedEntries 返回每个官方包条目的最大未压缩字节数。
func (manager *ModelManager) expectedEntries() map[string]int64 {
	return map[string]int64{
		manager.asset.ModelPath:    manager.asset.ModelSize,
		manager.asset.ManifestPath: maxModelMetadataSize,
		manager.asset.LicensePath:  maxModelMetadataSize,
		manager.asset.NoticePath:   maxModelMetadataSize,
	}
}

// installDir 返回固定模型 ID 与版本对应的安装目录。
func (manager *ModelManager) installDir() string {
	return filepath.Join(manager.rootDir, manager.asset.ID, manager.asset.Version)
}

// status 构造不暴露缓存或临时路径的状态。
func (manager *ModelManager) status(state ModelState) ModelStatus {
	return ModelStatus{
		State: state, ModelID: manager.asset.ModelID, ModelVersion: manager.asset.Version,
		ModelSize: manager.asset.ModelSize, InstallPath: manager.installDir(),
	}
}

// setInstalling 设置可并发读取的安装中状态。
func (manager *ModelManager) setInstalling(value bool) {
	manager.stateMu.Lock()
	manager.installing = value
	manager.stateMu.Unlock()
}

// packageManifest 是官方模型包 manifest 的严格结构。
type packageManifest struct {
	SchemaVersion            int             `json:"schema_version"`
	ModelID                  string          `json:"model_id"`
	ModelVersion             string          `json:"model_version"`
	UpstreamModelVersion     string          `json:"upstream_model_version,omitempty"`
	UpstreamSourceRevision   string          `json:"upstream_source_revision,omitempty"`
	UpstreamCheckpointSHA256 string          `json:"upstream_checkpoint_sha256,omitempty"`
	ModelFile                string          `json:"model_file"`
	ModelSHA256              string          `json:"model_sha256"`
	ModelSizeBytes           int64           `json:"model_size_bytes"`
	License                  string          `json:"license"`
	LicenseFile              string          `json:"license_file"`
	NoticeFile               string          `json:"notice_file"`
	PackagedAt               string          `json:"packaged_at,omitempty"`
	Opset                    int             `json:"opset"`
	Input                    tensorContract  `json:"input"`
	Output                   tensorContract  `json:"output"`
	Audio                    json.RawMessage `json:"audio,omitempty"`
}

// tensorContract 描述 ONNX 节点的稳定名称、dtype 和 shape。
type tensorContract struct {
	Name  string            `json:"name"`
	DType string            `json:"dtype"`
	Shape []json.RawMessage `json:"shape"`
}

// verifyPackageManifest 严格解析 manifest 并与应用内置资源锁逐项核对。
func verifyPackageManifest(content []byte, asset assets.VoiceModelAsset) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest packageManifest
	if err := decoder.Decode(&manifest); err != nil {
		return fmt.Errorf("解析模型 manifest 失败: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.ModelID != asset.ModelID || manifest.ModelVersion != asset.Version ||
		manifest.ModelFile != asset.ModelPath || manifest.ModelSHA256 != asset.ModelSHA256 || manifest.ModelSizeBytes != asset.ModelSize ||
		manifest.License != asset.License || manifest.LicenseFile != asset.LicensePath || manifest.NoticeFile != asset.NoticePath ||
		manifest.Opset != asset.Opset || manifest.Input.Name != asset.InputName || manifest.Output.Name != asset.OutputName ||
		manifest.Input.DType != "float32" || manifest.Output.DType != "float32" {
		return fmt.Errorf("模型 manifest 与内置资源锁不一致")
	}
	return verifyTensorShapes(manifest, asset)
}

// verifyTensorShapes 校验动态 batch/frame、FBank 维度和 embedding 维度。
func verifyTensorShapes(manifest packageManifest, asset assets.VoiceModelAsset) error {
	input, _ := json.Marshal(manifest.Input.Shape)
	output, _ := json.Marshal(manifest.Output.Shape)
	expectedInput := fmt.Sprintf(`["batch","frames",%d]`, asset.FeatureBins)
	expectedOutput := fmt.Sprintf(`["batch",%d]`, asset.EmbeddingDimension)
	if string(input) != expectedInput || string(output) != expectedOutput {
		return fmt.Errorf("模型输入输出 shape 与内置资源锁不一致")
	}
	return nil
}

// extractModelEntry 将受大小限制的普通文件同步写入 staging。
func extractModelEntry(entry *zip.File, target string, limit int64) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("打开模型包条目失败: %w", err)
	}
	defer source.Close()
	targetFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建模型 staging 文件失败: %w", err)
	}
	written, copyErr := io.Copy(targetFile, io.LimitReader(source, limit+1))
	syncErr := targetFile.Sync()
	closeErr := targetFile.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written > limit {
		return fmt.Errorf("写入模型 staging 文件失败")
	}
	return nil
}

// replaceDirectory 原子切换版本目录；失败时尽量恢复旧目录。
func replaceDirectory(staging string, target string) error {
	backup := target + ".previous"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	return os.RemoveAll(backup)
}
