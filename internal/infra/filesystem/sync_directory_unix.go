//go:build !windows

package filesystem

import "os"

// syncDirectory 在 rename 后同步目录项，避免断电时只保留临时文件内容而丢失目标名称。
func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
