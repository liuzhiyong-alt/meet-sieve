package filesystem

import (
	"fmt"
	"os"
)

// ProbeWritable 在明确指定的目录内创建并清理探测文件，以确认真实写入权限。
func ProbeWritable(directory string) error {
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("读取待检测目录失败: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("待检测路径不是目录")
	}

	probe, err := os.CreateTemp(directory, ".meetsieve-write-probe-*")
	if err != nil {
		return fmt.Errorf("目录不可写: %w", err)
	}
	path := probe.Name()
	defer os.Remove(path)

	if _, err := probe.Write([]byte("ok")); err != nil {
		_ = probe.Close()
		return fmt.Errorf("写入探测文件失败: %w", err)
	}
	if err := probe.Sync(); err != nil {
		_ = probe.Close()
		return fmt.Errorf("同步探测文件失败: %w", err)
	}
	if err := probe.Close(); err != nil {
		return fmt.Errorf("关闭探测文件失败: %w", err)
	}
	return nil
}
