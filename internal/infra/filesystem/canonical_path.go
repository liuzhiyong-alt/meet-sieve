package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CanonicalPath 表示已解析符号链接并可用于安全比较的绝对路径。
type CanonicalPath struct {
	value string
}

// CanonicalizePath 将用户提供的绝对路径规范化；不存在的末尾组件基于最近存在父目录附加。
func CanonicalizePath(path string) (CanonicalPath, error) {
	if !filepath.IsAbs(path) {
		return CanonicalPath{}, fmt.Errorf("路径必须是绝对路径")
	}
	canonical, err := canonicalizeAbsolutePath(filepath.Clean(path))
	if err != nil {
		return CanonicalPath{}, err
	}
	return CanonicalPath{value: canonical}, nil
}

// String 返回保存到 locator 或数据库前的真实绝对路径。
func (path CanonicalPath) String() string {
	return path.value
}

// Contains 按平台路径边界判断 candidate 是否等于或位于当前路径内部。
func (path CanonicalPath) Contains(candidate CanonicalPath) bool {
	root := normalizePathForComparison(path.value)
	target := normalizePathForComparison(candidate.value)
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// canonicalizeAbsolutePath 在不创建目录的前提下解析现有父目录和末尾不存在组件。
func canonicalizeAbsolutePath(path string) (string, error) {
	current := path
	var missing []string
	for {
		canonical, err := canonicalExistingPath(current)
		if err == nil {
			return appendMissingPathComponents(canonical, missing), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("解析路径失败: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("找不到路径的已存在父目录: %s", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// nearestExistingCanonicalPath 返回给定路径向上最近的已存在规范路径，不创建任何目录。
func nearestExistingCanonicalPath(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		canonical, err := canonicalExistingPath(current)
		if err == nil {
			return canonical, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("找不到路径的已存在父目录: %s", path)
		}
		current = parent
	}
}

// appendMissingPathComponents 按原有顺序附加尚不存在的路径组件。
func appendMissingPathComponents(parent string, missing []string) string {
	result := parent
	for index := len(missing) - 1; index >= 0; index-- {
		result = filepath.Join(result, missing[index])
	}
	return result
}

// normalizePathForComparison 仅在 Windows 采用大小写不敏感的路径比较语义。
func normalizePathForComparison(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
