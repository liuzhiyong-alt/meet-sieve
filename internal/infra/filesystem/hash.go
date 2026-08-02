package filesystem

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// SHA256File 以流式方式计算文件的 SHA-256 十六进制摘要。
func SHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开哈希文件失败: %w", err)
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("读取哈希文件失败: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
