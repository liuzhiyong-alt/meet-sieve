package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/filesystem"
)

// TestCanonicalizePath_RejectsRelativeAndResolvesExistingSymlink 验证用户输入必须绝对且已有符号链接保存真实路径。
func TestCanonicalizePath_RejectsRelativeAndResolvesExistingSymlink(t *testing.T) {
	if _, err := filesystem.CanonicalizePath("~/Meetings"); err == nil {
		t.Fatal("家目录缩写必须被拒绝")
	}

	realDirectory := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("创建真实目录失败：%v", err)
	}
	link := filepath.Join(filepath.Dir(realDirectory), "link")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Skipf("当前文件系统不支持符号链接：%v", err)
	}
	path, err := filesystem.CanonicalizePath(link)
	if err != nil {
		t.Fatalf("规范化符号链接失败：%v", err)
	}
	realPath, err := filesystem.CanonicalizePath(realDirectory)
	if err != nil {
		t.Fatalf("规范化真实目录失败：%v", err)
	}
	if path.String() != realPath.String() {
		t.Fatalf("符号链接未解析为真实路径：got %q, want %q", path.String(), realPath.String())
	}
}

// TestCanonicalizePath_UsesNearestExistingParentAndSemanticContainment 验证不存在目标仍能基于真实父目录安全规范化。
func TestCanonicalizePath_UsesNearestExistingParentAndSemanticContainment(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filesystem.CanonicalizePath(root)
	if err != nil {
		t.Fatalf("规范化根目录失败：%v", err)
	}
	candidate, err := filesystem.CanonicalizePath(filepath.Join(root, "new", "workspace"))
	if err != nil {
		t.Fatalf("规范化不存在目标失败：%v", err)
	}
	if candidate.String() != filepath.Join(canonicalRoot.String(), "new", "workspace") {
		t.Fatalf("不存在目标路径不正确：%q", candidate.String())
	}

	if !canonicalRoot.Contains(candidate) {
		t.Fatal("根目录应包含其后代")
	}
	similar, err := filesystem.CanonicalizePath(root + "-similar")
	if err != nil {
		t.Fatalf("规范化相似名称失败：%v", err)
	}
	if canonicalRoot.Contains(similar) {
		t.Fatal("名称相似目录不能被视为根目录后代")
	}
}
