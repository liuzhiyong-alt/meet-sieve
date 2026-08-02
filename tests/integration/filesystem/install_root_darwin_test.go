//go:build darwin

package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/filesystem"
)

// TestResolveInstallRoot_DarwinUsesBundleRootOrExecutableParent 验证 macOS bundle 与开发运行的安装边界。
func TestResolveInstallRoot_DarwinUsesBundleRootOrExecutableParent(t *testing.T) {
	base := t.TempDir()
	bundleRoot := filepath.Join(base, "MeetSieve.app")
	bundleExecutable := filepath.Join(bundleRoot, "Contents", "MacOS", "MeetSieve")
	if err := os.MkdirAll(filepath.Dir(bundleExecutable), 0o700); err != nil {
		t.Fatalf("创建 bundle 目录失败：%v", err)
	}
	if err := os.WriteFile(bundleExecutable, nil, 0o700); err != nil {
		t.Fatalf("创建 bundle 可执行文件失败：%v", err)
	}

	root, err := filesystem.ResolveInstallRoot(bundleExecutable)
	if err != nil {
		t.Fatalf("解析 bundle 安装根失败：%v", err)
	}
	wantRoot, err := filesystem.CanonicalizePath(bundleRoot)
	if err != nil {
		t.Fatalf("规范化 bundle 根失败：%v", err)
	}
	if root.String() != wantRoot.String() {
		t.Fatalf("bundle 安装根不正确：got %q want %q", root.String(), wantRoot.String())
	}

	developmentExecutable := filepath.Join(base, "bin", "MeetSieve")
	if err := os.MkdirAll(filepath.Dir(developmentExecutable), 0o700); err != nil {
		t.Fatalf("创建开发目录失败：%v", err)
	}
	if err := os.WriteFile(developmentExecutable, nil, 0o700); err != nil {
		t.Fatalf("创建开发可执行文件失败：%v", err)
	}
	developmentRoot, err := filesystem.ResolveInstallRoot(developmentExecutable)
	if err != nil {
		t.Fatalf("解析开发安装根失败：%v", err)
	}
	wantDevelopmentRoot, err := filesystem.CanonicalizePath(filepath.Dir(developmentExecutable))
	if err != nil {
		t.Fatalf("规范化开发安装根失败：%v", err)
	}
	if developmentRoot.String() != wantDevelopmentRoot.String() {
		t.Fatalf("开发安装根不正确：got %q want %q", developmentRoot.String(), wantDevelopmentRoot.String())
	}
}
