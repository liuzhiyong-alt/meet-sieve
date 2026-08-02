package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/config"
	workspace "meet-sieve/internal/service/workspace"
)

// TestWorkspaceService_UseThenSaveCopiedWorkspace 验证首次 use 切入新库，普通保存副本路径只影响下次启动。
func TestWorkspaceService_UseThenSaveCopiedWorkspace(t *testing.T) {
	base := t.TempDir()
	store := &memoryLocatorStore{}
	service := workspace.NewWorkspaceService(newInspector(t, base, 11*gibibyte), newInitializer(t, base), store, "", "", func() bool { return false })
	activePath := filepath.Join(base, "active")

	settings, err := service.UseWorkspace(activePath)
	if err != nil {
		t.Fatalf("首次 use 工作目录失败：%v", err)
	}
	if settings.ActivePath == "" || settings.ActivePath != settings.SavedPath || settings.RestartRequired {
		t.Fatalf("首次 use 后设置状态不正确：%+v", settings)
	}
	activePath = settings.ActivePath
	copiedPath := copyWorkspaceDatabase(t, activePath, filepath.Join(base, "copied"))

	settings, err = service.SaveWorkspacePath(copiedPath)
	if err != nil {
		t.Fatalf("保存手动副本路径失败：%v", err)
	}
	if settings.ActivePath != activePath || settings.SavedPath == activePath || !settings.RestartRequired {
		t.Fatalf("保存副本路径不应切换当前数据库：%+v", settings)
	}
	if store.locator.WorkspacePath != settings.SavedPath {
		t.Fatalf("locator 未保存副本路径：%+v", store.locator)
	}
	assertDirectoryExists(t, filepath.Join(copiedPath, "data", "backups"))
	assertDirectoryExists(t, filepath.Join(copiedPath, "data", "voice-samples"))
	assertDirectoryExists(t, filepath.Join(copiedPath, "meetings"))
}

// TestWorkspaceService_BlocksSaveDuringMeeting 验证服务端在会议进行中拒绝保存，且不触发目录初始化。
func TestWorkspaceService_BlocksSaveDuringMeeting(t *testing.T) {
	base := t.TempDir()
	store := &memoryLocatorStore{}
	service := workspace.NewWorkspaceService(newInspector(t, base, 11*gibibyte), newInitializer(t, base), store, "/active", "/active", func() bool { return true })
	blockedPath := filepath.Join(base, "must-not-create")

	_, err := service.SaveWorkspacePath(blockedPath)
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeWorkspaceChangeBlocked.ErrorCode {
		t.Fatalf("会议中保存必须返回 WORKSPACE_CHANGE_BLOCKED：%v", err)
	}
	if _, err := os.Stat(blockedPath); !os.IsNotExist(err) {
		t.Fatalf("会议中保存不得检查或初始化新目录：err=%v", err)
	}
}

// TestWorkspaceService_LocatorFailureKeepsActivePath 验证 locator 保存失败不会回滚已完成初始化，也不会改当前/已保存路径。
func TestWorkspaceService_LocatorFailureKeepsActivePath(t *testing.T) {
	base := t.TempDir()
	store := &memoryLocatorStore{saveErr: errors.New("write failed")}
	service := workspace.NewWorkspaceService(newInspector(t, base, 11*gibibyte), newInitializer(t, base), store, "/active", "/active", func() bool { return false })
	workspacePath := filepath.Join(base, "initialized-but-unsaved")

	settings, err := service.SaveWorkspacePath(workspacePath)
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeLocatorWriteFailed.ErrorCode {
		t.Fatalf("locator 保存失败必须映射稳定错误：%v", err)
	}
	if settings.ActivePath != "/active" || settings.SavedPath != "/active" {
		t.Fatalf("locator 失败不得改变内存路径：%+v", settings)
	}
	if _, err := os.Stat(filepath.Join(workspacePath, "data", "meetings.db")); err != nil {
		t.Fatalf("locator 失败后已验证的初始化数据库应保留：%v", err)
	}
}

// memoryLocatorStore 是服务测试使用的最小 locator 持久化边界。
type memoryLocatorStore struct {
	locator config.Locator
	saveErr error
}

// Load 返回当前内存 locator。
func (store *memoryLocatorStore) Load() (config.Locator, bool, error) {
	return store.locator, store.locator.WorkspacePath != "", nil
}

// Save 保存 locator 或注入原子写失败。
func (store *memoryLocatorStore) Save(locator config.Locator) error {
	if store.saveErr != nil {
		return store.saveErr
	}
	store.locator = locator
	return nil
}

// copyWorkspaceDatabase 构造用户手动复制的最小有效工作目录副本。
func copyWorkspaceDatabase(t *testing.T, sourcePath string, targetPath string) string {
	t.Helper()
	sourceDatabase := filepath.Join(sourcePath, "data", "meetings.db")
	targetDatabase := filepath.Join(targetPath, "data", "meetings.db")
	content, err := os.ReadFile(sourceDatabase)
	if err != nil {
		t.Fatalf("读取源数据库失败：%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetDatabase), 0o700); err != nil {
		t.Fatalf("创建副本 data 目录失败：%v", err)
	}
	if err := os.WriteFile(targetDatabase, content, 0o600); err != nil {
		t.Fatalf("写入副本数据库失败：%v", err)
	}
	return targetPath
}
