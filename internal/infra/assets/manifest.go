// Package assets 负责第三方二进制资源的锁定、下载、校验和解压。
package assets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Manifest 是 third_party/assets.lock.json 的严格结构。
type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	Assets        []Asset           `json:"assets"`
	VoiceModels   []VoiceModelAsset `json:"voice_models,omitempty"`
}

// Asset 描述一个平台上的已锁第三方资源。
type Asset struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	URL           string `json:"url"`
	ArchiveType   string `json:"archive_type"`
	ArchiveSHA256 string `json:"archive_sha256"`
	ArchiveSize   int64  `json:"archive_size"`
	LibraryPath   string `json:"library_path"`
	LibrarySHA256 string `json:"library_sha256"`
	LibrarySize   int64  `json:"library_size"`
	LicensePath   string `json:"license_path"`
	License       string `json:"license"`
}

// VoiceModelAsset 描述平台无关、由 MeetSieve 发布的锁定声纹模型包。
type VoiceModelAsset struct {
	ID                       string `json:"id"`
	Version                  string `json:"version"`
	ModelID                  string `json:"model_id"`
	UpstreamVersion          string `json:"upstream_version"`
	UpstreamRevision         string `json:"upstream_revision"`
	UpstreamCheckpointSHA256 string `json:"upstream_checkpoint_sha256"`
	URL                      string `json:"url"`
	ArchiveSHA256            string `json:"archive_sha256"`
	ArchiveSize              int64  `json:"archive_size"`
	ModelPath                string `json:"model_path"`
	ModelSHA256              string `json:"model_sha256"`
	ModelSize                int64  `json:"model_size"`
	ManifestPath             string `json:"manifest_path"`
	ManifestSHA256           string `json:"manifest_sha256"`
	LicensePath              string `json:"license_path"`
	LicenseSHA256            string `json:"license_sha256"`
	NoticePath               string `json:"notice_path"`
	NoticeSHA256             string `json:"notice_sha256"`
	License                  string `json:"license"`
	Opset                    int    `json:"opset"`
	InputName                string `json:"input_name"`
	OutputName               string `json:"output_name"`
	FeatureBins              int    `json:"feature_bins"`
	EmbeddingDimension       int    `json:"embedding_dimension"`
}

// ParseManifest 严格解析资源锁，拒绝未知字段和不完整资源。
func ParseManifest(data []byte) (Manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("解析资源锁失败: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate 检查资源锁的版本、平台、下载来源和完整性字段。
func (m Manifest) Validate() error {
	if m.SchemaVersion != 1 && m.SchemaVersion != 2 {
		return fmt.Errorf("不支持的资源锁版本: %d", m.SchemaVersion)
	}
	if len(m.Assets) == 0 {
		return fmt.Errorf("资源锁不能为空")
	}
	seen := make(map[string]struct{}, len(m.Assets))
	for index, asset := range m.Assets {
		if err := asset.validate(); err != nil {
			return fmt.Errorf("资源 assets[%d] 不合法: %w", index, err)
		}
		key := asset.ID + "/" + asset.OS + "/" + asset.Arch
		if _, exists := seen[key]; exists {
			return fmt.Errorf("资源平台重复: %s", key)
		}
		seen[key] = struct{}{}
	}
	if m.SchemaVersion == 1 && len(m.VoiceModels) > 0 {
		return fmt.Errorf("资源锁版本 1 不支持声纹模型")
	}
	voiceSeen := make(map[string]struct{}, len(m.VoiceModels))
	for index, model := range m.VoiceModels {
		if err := model.validate(); err != nil {
			return fmt.Errorf("资源 voice_models[%d] 不合法: %w", index, err)
		}
		if _, exists := voiceSeen[model.ID]; exists {
			return fmt.Errorf("声纹模型重复: %s", model.ID)
		}
		voiceSeen[model.ID] = struct{}{}
	}
	return nil
}

// Select 返回指定资源在目标平台的锁定记录。
func (m Manifest) Select(id string, targetOS string, targetArch string) (Asset, error) {
	for _, asset := range m.Assets {
		if asset.ID == id && asset.OS == targetOS && asset.Arch == targetArch {
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("资源 %s 不支持平台 %s/%s", id, targetOS, targetArch)
}

// SelectVoiceModel 返回指定稳定 ID 的官方声纹模型包。
func (m Manifest) SelectVoiceModel(id string) (VoiceModelAsset, error) {
	for _, model := range m.VoiceModels {
		if model.ID == id {
			return model, nil
		}
	}
	return VoiceModelAsset{}, fmt.Errorf("声纹模型 %s 未登记", id)
}

// ArchiveFilename 返回 URL 中锁定的归档文件名。
func (a Asset) ArchiveFilename() (string, error) {
	return archiveFilename(a.URL)
}

// ArchiveFilename 返回官方模型包 URL 中锁定的文件名。
func (a VoiceModelAsset) ArchiveFilename() (string, error) {
	return archiveFilename(a.URL)
}

// archiveFilename 安全提取 HTTPS URL 的末级文件名。
func archiveFilename(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("解析资源 URL 失败: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return "", fmt.Errorf("资源 URL 缺少文件名")
	}
	return parts[len(parts)-1], nil
}

// validate 校验单个平台资源的来源、归档、动态库和许可证字段。
func (a Asset) validate() error {
	if a.ID == "" || a.Version == "" || a.OS == "" || a.Arch == "" {
		return fmt.Errorf("id、version、os、arch 不能为空")
	}
	if a.URL == "" || a.ArchiveSHA256 == "" || a.ArchiveSize <= 0 {
		return fmt.Errorf("url、archive_sha256、archive_size 不能为空")
	}
	if len(a.ArchiveSHA256) != 64 {
		return fmt.Errorf("archive_sha256 必须是 64 位十六进制")
	}
	if a.ArchiveType != "tgz" && a.ArchiveType != "zip" {
		return fmt.Errorf("不支持的归档类型: %s", a.ArchiveType)
	}
	if a.LibraryPath == "" || len(a.LibrarySHA256) != 64 || a.LibrarySize <= 0 {
		return fmt.Errorf("library_path、library_sha256、library_size 不合法")
	}
	if a.LicensePath == "" || a.License == "" {
		return fmt.Errorf("license_path、license 不能为空")
	}
	parsed, err := url.Parse(a.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" {
		return fmt.Errorf("资源 URL 必须来自 github.com 的 HTTPS 地址")
	}
	return nil
}

// validate 校验官方声纹模型的来源、身份、文件和推理契约。
func (a VoiceModelAsset) validate() error {
	if a.ID == "" || a.Version == "" || a.ModelID == "" || a.UpstreamVersion == "" {
		return fmt.Errorf("id、version、model_id、upstream_version 不能为空")
	}
	if len(a.UpstreamRevision) != 40 || !validSHA256(a.UpstreamCheckpointSHA256) {
		return fmt.Errorf("上游 revision 或 checkpoint SHA-256 不合法")
	}
	if !validSHA256(a.ArchiveSHA256) || a.ArchiveSize <= 0 || !validSHA256(a.ModelSHA256) || a.ModelSize <= 0 {
		return fmt.Errorf("模型包或 ONNX 的大小、SHA-256 不合法")
	}
	if a.ModelPath != "model.onnx" || a.ManifestPath != "manifest.json" || a.LicensePath != "LICENSE" || a.NoticePath != "NOTICE" {
		return fmt.Errorf("官方模型包文件名不符合固定契约")
	}
	if !validSHA256(a.ManifestSHA256) || !validSHA256(a.LicenseSHA256) || !validSHA256(a.NoticeSHA256) {
		return fmt.Errorf("manifest、许可证或 NOTICE SHA-256 不合法")
	}
	if a.License == "" || a.Opset <= 0 || a.InputName == "" || a.OutputName == "" || a.FeatureBins <= 0 || a.EmbeddingDimension <= 0 {
		return fmt.Errorf("许可证或推理契约不完整")
	}
	parsed, err := url.Parse(a.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || !strings.Contains(parsed.Path, "/releases/download/") {
		return fmt.Errorf("模型包 URL 必须来自 github.com Releases")
	}
	filename, err := a.ArchiveFilename()
	if err != nil || !strings.HasSuffix(filename, ".zip") {
		return fmt.Errorf("模型包必须是固定 ZIP 文件")
	}
	return nil
}

// validSHA256 检查哈希是否为 64 位小写十六进制。
func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
