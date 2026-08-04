package deletion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Scan 使用 Lstat 遍历根目录，不跟随符号链接；knownPaths 使用规范相对路径标记已登记对象。
func Scan(root string, knownPaths map[string]bool) ([]Item, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("扫描根目录无效")
	}
	var items []Item
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil || validateRelativePath(relativePath) != nil {
			return fmt.Errorf("扫描目标越界")
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		itemType, err := classifyMode(info.Mode())
		if err != nil {
			return err
		}
		item := Item{ID: itemID(relativePath), RelativePath: relativePath, Type: itemType, Known: knownPaths[relativePath]}
		if itemType == ItemFile {
			item.SizeBytes = info.Size()
			digest, hashErr := HashFile(path)
			if hashErr != nil {
				return hashErr
			}
			item.SHA256 = digest
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return SortForDeletion(items), nil
}

// HashFile 流式计算普通文件内容摘要，供预览和执行时检测替换。
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// classifyMode 只允许普通文件、目录和符号链接，special file 在预览阶段阻断。
func classifyMode(mode fs.FileMode) (ItemType, error) {
	switch {
	case mode.IsRegular():
		return ItemFile, nil
	case mode.IsDir():
		return ItemDirectory, nil
	case mode&os.ModeSymlink != 0:
		return ItemSymlink, nil
	default:
		return "", fmt.Errorf("会议目录包含不支持的特殊文件")
	}
}

// itemID 从相对路径生成不泄漏绝对路径的稳定项目 ID。
func itemID(relativePath string) string {
	sum := sha256.Sum256([]byte(relativePath))
	return hex.EncodeToString(sum[:16])
}
