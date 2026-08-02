package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomic 将内容写入与目标同目录的临时文件，并在 fsync 后原子替换目标。
func WriteAtomic(path string, content []byte, permission os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".meetsieve-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(permission); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("替换目标文件失败: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("同步目标目录失败: %w", err)
	}
	return nil
}
