package database

import (
	"fmt"
	"os"
	"path/filepath"
)

// MoveFileExclusive 在同一卷内将 source 原子移动至此前不存在的 destination，绝不覆盖既有文件。
func MoveFileExclusive(source string, destination string) error {
	if source == "" || destination == "" || !filepath.IsAbs(source) || !filepath.IsAbs(destination) {
		return fmt.Errorf("数据库切换路径必须为绝对路径")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("数据库切换目标已存在")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查数据库切换目标失败: %w", err)
	}
	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("原子移动数据库文件失败: %w", err)
	}
	return nil
}
