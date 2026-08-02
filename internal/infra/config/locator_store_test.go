package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestLocatorStore_SaveKeepsOldBytesWhenAtomicWriteFails 验证保存失败不会覆盖原 locator。
func TestLocatorStore_SaveKeepsOldBytesWhenAtomicWriteFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	oldContent := []byte(`{"schema_version":1,"workspace_path":"/old"}`)
	if err := os.WriteFile(path, oldContent, 0o600); err != nil {
		t.Fatalf("准备旧 locator 失败：%v", err)
	}
	store := &LocatorStore{
		path: path,
		atomicWrite: func(string, []byte, os.FileMode) error {
			return errors.New("模拟原子替换失败")
		},
	}

	err := store.Save(Locator{SchemaVersion: LocatorSchemaVersion, WorkspacePath: "/new"})
	if err == nil {
		t.Fatal("原子写失败必须返回错误")
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("读取旧 locator 失败：%v", readErr)
	}
	if string(content) != string(oldContent) {
		t.Fatalf("保存失败后旧 locator 被改写：%s", content)
	}
}
