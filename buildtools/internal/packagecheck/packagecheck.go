// Package packagecheck 提供双平台安装资源共用的哈希、profile 和模型权重门禁。
package packagecheck

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/assets"
)

// LoadManifest 读取并严格解析第三方资源锁。
func LoadManifest(path string) (assets.Manifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return assets.Manifest{}, fmt.Errorf("读取资源锁失败: %w", err)
	}
	return assets.ParseManifest(content)
}

// VerifyLockedFile 校验普通文件的大小和 SHA-256 与资源锁一致。
func VerifyLockedFile(path string, expectedSize int64, expectedSHA256 string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("读取锁定资源失败 %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() != expectedSize {
		return fmt.Errorf("锁定资源大小不一致 %s: expected=%d actual=%d", path, expectedSize, info.Size())
	}
	actual, err := FileSHA256(path)
	if err != nil {
		return err
	}
	if actual != expectedSHA256 {
		return fmt.Errorf("锁定资源 SHA-256 不一致 %s", path)
	}
	return nil
}

// VerifyLicense 校验安装包携带的许可证是非空普通文件。
func VerifyLicense(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("读取许可证失败 %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("许可证必须是非空普通文件: %s", path)
	}
	return nil
}

// VerifyVoiceProfile 校验正式 profile 绑定到资源锁中的 campplus 模型身份。
func VerifyVoiceProfile(path string, manifest assets.Manifest) error {
	model, err := manifest.SelectVoiceModel("campplus")
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取正式声纹 profile 失败: %w", err)
	}
	expected := speakerdomain.ModelIdentity{
		ID: model.ModelID, Version: model.Version, SHA256: model.ModelSHA256, Dimension: model.EmbeddingDimension,
	}
	if _, err := speakerdomain.ParseMatchingProfile(content, expected); err != nil {
		return fmt.Errorf("正式声纹 profile 与资源锁不一致: %w", err)
	}
	return nil
}

// RejectEmbeddedVoiceModel 拒绝安装资源目录携带声纹 ONNX 权重。
func RejectEmbeddedVoiceModel(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		lower := strings.ToLower(entry.Name())
		if strings.HasSuffix(lower, ".onnx") || strings.Contains(lower, "campplus") || strings.Contains(lower, "voice-model") {
			return fmt.Errorf("安装资源不得内置声纹模型: %s", path)
		}
		return nil
	})
}

// FileSHA256 以流式读取方式计算普通文件 SHA-256。
func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开文件计算 SHA-256 失败 %s: %w", path, err)
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("计算文件 SHA-256 失败 %s: %w", path, err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
