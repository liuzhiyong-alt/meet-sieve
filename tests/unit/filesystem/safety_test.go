package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/filesystem"
)

// TestSHA256File_ReturnsKnownDigest 验证文件哈希与已知 SHA-256 一致。
func TestSHA256File_ReturnsKnownDigest(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("MeetSieve"), 0o600); err != nil {
		t.Fatalf("准备哈希文件失败：%v", err)
	}
	actual, err := filesystem.SHA256File(path)
	if err != nil {
		t.Fatalf("计算哈希失败：%v", err)
	}
	const expected = "cb22d4a35c9516e48335050de4ceef632b027b28a7644df44a16f891039ea958"
	if actual != expected {
		t.Fatalf("哈希不一致：got %s, want %s", actual, expected)
	}
}

// TestSafeFilename_RemovesTraversalAndPlatformInvalidCharacters 验证安全文件名不会保留路径和平台非法字符。
func TestSafeFilename_RemovesTraversalAndPlatformInvalidCharacters(t *testing.T) {
	t.Parallel()

	actual := filesystem.SafeFilename("../会议:<记录>?*.md")
	if actual != "会议__记录___.md" {
		t.Fatalf("安全文件名不符合预期：got %q", actual)
	}
}

// TestResolveWithinRoot_RejectsTraversalAndSymlinkEscape 验证路径穿越和符号链接逃逸都会被拒绝。
func TestResolveWithinRoot_RejectsTraversalAndSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, err := filesystem.ResolveWithinRoot(root, "../outside"); err == nil {
		t.Fatal("路径穿越应被拒绝")
	}

	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("创建符号链接失败：%v", err)
	}
	if _, err := filesystem.ResolveWithinRoot(root, filepath.Join("link", "file.txt")); err == nil {
		t.Fatal("符号链接逃逸应被拒绝")
	}
}

// TestProbeWritable_UsesExplicitDirectory 验证可写性探测只操作调用方指定目录并清理探测文件。
func TestProbeWritable_UsesExplicitDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := filesystem.ProbeWritable(root); err != nil {
		t.Fatalf("可写性探测失败：%v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("读取探测目录失败：%v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("探测文件未清理：got %d entries", len(entries))
	}
}
