package filesystem_test

import (
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/filesystem"
)

// TestWriteAtomic_ReplacesCompleteContent 验证原子写只在完整内容落盘后替换目标文件。
func TestWriteAtomic_ReplacesCompleteContent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "record.md")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("准备旧文件失败：%v", err)
	}
	if err := filesystem.WriteAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("原子写失败：%v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取结果失败：%v", err)
	}
	if string(content) != "new" {
		t.Fatalf("写入内容不完整：got %q", content)
	}
}
