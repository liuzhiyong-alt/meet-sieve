package assets

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extract 解压已校验归档并确认动态库和许可证存在。
func Extract(asset Asset, archivePath string, cacheDir string) (string, error) {
	targetDir := filepath.Join(cacheDir, "extracted", asset.OS+"-"+asset.Arch)
	libraryPath := filepath.Join(targetDir, filepath.FromSlash(asset.LibraryPath))
	licensePath := filepath.Join(targetDir, filepath.FromSlash(asset.LicensePath))
	if isVerifiedFile(libraryPath, asset.LibrarySHA256, asset.LibrarySize) && filesExist(licensePath) {
		return libraryPath, nil
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return "", fmt.Errorf("清理解压目录失败: %w", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("创建解压目录失败: %w", err)
	}
	var err error
	switch asset.ArchiveType {
	case "tgz":
		err = extractTGZ(archivePath, targetDir)
	case "zip":
		err = extractZIP(archivePath, targetDir)
	default:
		err = fmt.Errorf("不支持的归档类型: %s", asset.ArchiveType)
	}
	if err != nil {
		_ = os.RemoveAll(targetDir)
		return "", err
	}
	if !isVerifiedFile(libraryPath, asset.LibrarySHA256, asset.LibrarySize) || !filesExist(licensePath) {
		_ = os.RemoveAll(targetDir)
		return "", fmt.Errorf("归档缺少动态库或许可证")
	}
	return libraryPath, nil
}

// extractTGZ 安全遍历 tgz 条目并写入目标目录。
func extractTGZ(archivePath string, targetDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开 tgz 失败: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("读取 gzip 失败: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取 tar 失败: %w", err)
		}
		if err := writeArchiveEntry(targetDir, header.Name, header.FileInfo(), reader); err != nil {
			return err
		}
	}
}

// extractZIP 安全遍历 zip 条目并写入目标目录。
func extractZIP(archivePath string, targetDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开 zip 失败: %w", err)
	}
	defer reader.Close()
	for _, entry := range reader.File {
		source, err := entry.Open()
		if err != nil {
			return fmt.Errorf("打开 zip 条目失败: %w", err)
		}
		writeErr := writeArchiveEntry(targetDir, entry.Name, entry.FileInfo(), source)
		closeErr := source.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return fmt.Errorf("关闭 zip 条目失败: %w", closeErr)
		}
	}
	return nil
}

// writeArchiveEntry 校验归档条目路径，并只落盘目录或普通文件。
func writeArchiveEntry(root string, name string, info os.FileInfo, source io.Reader) error {
	cleanName := filepath.Clean(filepath.FromSlash(name))
	if cleanName == "." || filepath.IsAbs(cleanName) || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return fmt.Errorf("归档条目路径不安全: %s", name)
	}
	target := filepath.Join(root, cleanName)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("归档条目逃逸目标目录: %s", name)
	}
	if info.IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("创建归档条目目录失败: %w", err)
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("创建归档条目失败: %w", err)
	}
	if _, err := io.Copy(file, source); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入归档条目失败: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭归档条目失败: %w", err)
	}
	return nil
}

// filesExist 检查所有路径均为存在的普通文件。
func filesExist(paths ...string) bool {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
