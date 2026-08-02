//go:build windows

package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
)

// CurrentInstallRoot 返回当前 Windows 可执行文件父目录的规范路径。
func CurrentInstallRoot() (CanonicalPath, error) {
	executable, err := os.Executable()
	if err != nil {
		return CanonicalPath{}, fmt.Errorf("读取当前可执行文件失败: %w", err)
	}
	return ResolveInstallRoot(executable)
}

// ResolveInstallRoot 将 Windows 可执行文件父目录作为安装边界；CanonicalizePath 会解析 reparse alias。
func ResolveInstallRoot(executable string) (CanonicalPath, error) {
	return CanonicalizePath(filepath.Dir(executable))
}
