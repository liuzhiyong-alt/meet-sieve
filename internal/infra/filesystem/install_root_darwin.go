//go:build darwin

package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
)

// CurrentInstallRoot 返回当前 macOS 应用 bundle 根，开发运行时返回可执行文件父目录。
func CurrentInstallRoot() (CanonicalPath, error) {
	executable, err := os.Executable()
	if err != nil {
		return CanonicalPath{}, fmt.Errorf("读取当前可执行文件失败: %w", err)
	}
	return ResolveInstallRoot(executable)
}

// ResolveInstallRoot 按 macOS bundle 语义解析指定可执行文件的安装边界。
func ResolveInstallRoot(executable string) (CanonicalPath, error) {
	canonicalExecutable, err := CanonicalizePath(executable)
	if err != nil {
		return CanonicalPath{}, err
	}
	for directory := filepath.Dir(canonicalExecutable.String()); ; directory = filepath.Dir(directory) {
		if filepath.Ext(directory) == ".app" {
			return CanonicalizePath(directory)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return CanonicalizePath(filepath.Dir(canonicalExecutable.String()))
		}
	}
}
