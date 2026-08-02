package workspace_test

import (
	"errors"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/filesystem"
	workspace "meet-sieve/internal/service/workspace"
)

// TestPathPolicy_RejectsUnsafeVolumeAndInstallSubtree 验证工作目录路径策略拒绝不可信位置并允许安装目录外本地卷。
func TestPathPolicy_RejectsUnsafeVolumeAndInstallSubtree(t *testing.T) {
	base := t.TempDir()
	installRoot, err := filesystem.CanonicalizePath(filepath.Join(base, "MeetSieve.app"))
	if err != nil {
		t.Fatalf("规范化安装目录失败：%v", err)
	}
	policy := workspace.NewPathPolicy(installRoot, func(filesystem.CanonicalPath) (filesystem.VolumeKind, error) {
		return filesystem.VolumeLocal, nil
	})

	assertPathPolicyCode(t, policy, "relative", apperr.CodeWorkspacePathInvalid)
	assertPathPolicyCode(t, policy, filepath.Join(installRoot.String(), "Contents"), apperr.CodeWorkspaceInstallPathForbidden)

	accepted, appErr := policy.ValidateCandidate(filepath.Join(base, "MeetSieve.app-data"))
	if appErr != nil {
		t.Fatalf("安装目录外名称相似路径不应被拒绝：%v", appErr)
	}
	if accepted.String() == "" {
		t.Fatal("已接受路径必须返回规范化结果")
	}
}

// TestPathPolicy_RejectsNetworkOrUnknownVolume 验证网络卷和未知卷均不会进入后续初始化。
func TestPathPolicy_RejectsNetworkOrUnknownVolume(t *testing.T) {
	root, err := filesystem.CanonicalizePath(t.TempDir())
	if err != nil {
		t.Fatalf("规范化安装根失败：%v", err)
	}
	for _, kind := range []filesystem.VolumeKind{filesystem.VolumeNetwork, filesystem.VolumeUnknown} {
		policy := workspace.NewPathPolicy(root, func(filesystem.CanonicalPath) (filesystem.VolumeKind, error) {
			return kind, nil
		})
		assertPathPolicyCode(t, policy, filepath.Join(t.TempDir(), "workspace"), apperr.CodeWorkspaceUnsupportedVolume)
	}
}

// TestPathPolicy_AcceptsMissingPathOnExistingLocalParent 验证不存在工作目录按最近存在父目录判断本地卷。
func TestPathPolicy_AcceptsMissingPathOnExistingLocalParent(t *testing.T) {
	installRoot, err := filesystem.CanonicalizePath(filepath.Join(t.TempDir(), "install"))
	if err != nil {
		t.Fatalf("规范化安装目录失败：%v", err)
	}
	parent := t.TempDir()
	policy := workspace.NewPathPolicy(installRoot, nil)
	if _, err := policy.ValidateCandidate(filepath.Join(parent, "new-workspace")); err != nil {
		t.Fatalf("现有本地父目录下的不存在路径应可用于初始化：%v", err)
	}
}

// assertPathPolicyCode 验证路径策略返回指定的稳定 AppError。
func assertPathPolicyCode(t *testing.T, policy *workspace.PathPolicy, candidate string, want apperr.Code) {
	t.Helper()
	_, err := policy.ValidateCandidate(candidate)
	if err == nil {
		t.Fatalf("候选 %q 应被拒绝", candidate)
	}
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != want.ErrorCode {
		t.Fatalf("候选 %q 错误码不正确：%v", candidate, err)
	}
}
