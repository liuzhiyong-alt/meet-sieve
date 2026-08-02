package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"meet-sieve/internal/infra/config"
)

// TestLocatorStore_LoadMissingAndSaveMinimalLocator 验证 locator 不存在不初始化工作目录，保存后可严格读回。
func TestLocatorStore_LoadMissingAndSaveMinimalLocator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "system", "config.json")
	store := config.NewLocatorStore(path)

	if _, configured, err := store.Load(); err != nil || configured {
		t.Fatalf("不存在 locator 应返回未配置：configured=%t err=%v", configured, err)
	}

	want := config.Locator{SchemaVersion: config.LocatorSchemaVersion, WorkspacePath: "/Volumes/Meetings"}
	if err := store.Save(want); err != nil {
		t.Fatalf("保存 locator 失败：%v", err)
	}
	got, configured, err := store.Load()
	if err != nil || !configured || got != want {
		t.Fatalf("保存后 locator 读回不正确：got=%#v configured=%t err=%v", got, configured, err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 locator 文件失败：%v", err)
	}
	if string(content) != `{"schema_version":1,"workspace_path":"/Volumes/Meetings"}` {
		t.Fatalf("locator JSON 不够最小：%s", content)
	}
	if runtime.GOOS == "darwin" && contentMode(t, path) != 0o600 {
		t.Fatalf("macOS locator 权限不正确：%#o", contentMode(t, path))
	}
}

// contentMode 返回文件权限位。
func contentMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取 locator 权限失败：%v", err)
	}
	return info.Mode().Perm()
}
