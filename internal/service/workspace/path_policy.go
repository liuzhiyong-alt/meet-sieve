package workspace

import (
	"fmt"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/filesystem"
)

// VolumeDetector 定义工作目录路径的卷类型检测边界。
type VolumeDetector func(filesystem.CanonicalPath) (filesystem.VolumeKind, error)

// PathPolicy 在目录创建前统一校验工作目录候选与安装目录边界。
type PathPolicy struct {
	installRoot    filesystem.CanonicalPath
	volumeDetector VolumeDetector
}

// NewPathPolicy 创建工作目录路径策略；未注入检测器时使用当前平台真实卷检测。
func NewPathPolicy(installRoot filesystem.CanonicalPath, volumeDetector VolumeDetector) *PathPolicy {
	if volumeDetector == nil {
		volumeDetector = filesystem.DetectVolume
	}
	return &PathPolicy{installRoot: installRoot, volumeDetector: volumeDetector}
}

// ValidateCandidate 返回可安全使用的规范路径，或稳定的工作目录错误。
func (policy *PathPolicy) ValidateCandidate(input string) (filesystem.CanonicalPath, error) {
	candidate, err := filesystem.CanonicalizePath(input)
	if err != nil {
		return filesystem.CanonicalPath{}, apperr.Biz(apperr.CodeWorkspacePathInvalid, apperr.WithOp("workspace.path.canonicalize"))
	}
	if policy.installRoot.Contains(candidate) {
		return filesystem.CanonicalPath{}, apperr.Biz(apperr.CodeWorkspaceInstallPathForbidden, apperr.WithOp("workspace.path.install_boundary"))
	}
	if err := policy.validateVolume(candidate); err != nil {
		return filesystem.CanonicalPath{}, err
	}
	return candidate, nil
}

// ResolveWithinWorkspace 解析工作目录内相对路径，并拒绝绝对路径、穿越和符号链接逃逸。
func (policy *PathPolicy) ResolveWithinWorkspace(root filesystem.CanonicalPath, relativePath string) (string, error) {
	path, err := filesystem.ResolveWithinRoot(root.String(), relativePath)
	if err != nil {
		return "", apperr.Biz(apperr.CodeWorkspacePathInvalid, apperr.WithOp("workspace.path.relative"))
	}
	return path, nil
}

// validateVolume 只允许确认的本地卷；检测失败、网络卷和未知卷均 fail closed。
func (policy *PathPolicy) validateVolume(candidate filesystem.CanonicalPath) error {
	kind, err := policy.volumeDetector(candidate)
	if err != nil || kind != filesystem.VolumeLocal {
		return apperr.Biz(
			apperr.CodeWorkspaceUnsupportedVolume,
			apperr.WithOp("workspace.path.volume"),
			apperr.WithField("reason", volumeRejectionReason(kind, err)),
		)
	}
	return nil
}

// volumeRejectionReason 生成不包含真实路径的安全诊断字段。
func volumeRejectionReason(kind filesystem.VolumeKind, err error) string {
	if err != nil {
		return "detect_failed"
	}
	return fmt.Sprintf("volume_%s", kind)
}
