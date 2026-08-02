package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveWithinRoot 将相对路径解析到指定根目录，并拒绝路径穿越和符号链接逃逸。
func ResolveWithinRoot(root string, relativePath string) (string, error) {
	absoluteRoot, err := canonicalExistingPath(root)
	if err != nil {
		return "", fmt.Errorf("解析根目录失败: %w", err)
	}
	if filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("目标路径必须是相对路径")
	}

	target := filepath.Clean(filepath.Join(absoluteRoot, relativePath))
	canonicalParent, err := canonicalNearestExisting(filepath.Dir(target))
	if err != nil {
		return "", fmt.Errorf("解析目标父目录失败: %w", err)
	}
	if err := ensureWithinRoot(absoluteRoot, canonicalParent); err != nil {
		return "", err
	}
	if err := ensureWithinRoot(absoluteRoot, target); err != nil {
		return "", err
	}
	return target, nil
}

// canonicalExistingPath 返回已存在路径解析符号链接后的绝对路径。
func canonicalExistingPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

// canonicalNearestExisting 从目标父目录向上查找最近的已存在路径并解析符号链接。
func canonicalNearestExisting(path string) (string, error) {
	current := path
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
			return "", err
		}
		current = parent
	}
}

// ensureWithinRoot 校验目标路径解析后仍位于允许的根目录中。
func ensureWithinRoot(root string, target string) error {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("比较根目录失败: %w", err)
	}
	if relative == ".." || len(relative) > 3 && relative[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("目标路径超出允许的根目录")
	}
	return nil
}
