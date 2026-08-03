package resource

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var safeDispositionByte = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// FileStore 只打开会议 resources 根目录下的普通非 symlink 文件。
type FileStore struct{}

// NewFileStore 创建无状态的安全附件文件存储。
func NewFileStore() *FileStore { return &FileStore{} }

// Open 校验相对路径、根目录、Lstat 和打开后文件身份。
func (store *FileStore) Open(meetingDirectory string, relativePath string) (*os.File, os.FileInfo, error) {
	if store == nil || !filepath.IsAbs(meetingDirectory) || relativePath == "" || filepath.IsAbs(relativePath) {
		return nil, nil, fmt.Errorf("附件路径无效")
	}
	resourcesPath := filepath.Join(filepath.Clean(meetingDirectory), "resources")
	cleanRelative := filepath.Clean(filepath.FromSlash(relativePath))
	expectedPrefix := "resources" + string(filepath.Separator)
	if !strings.HasPrefix(cleanRelative, expectedPrefix) || filepath.Dir(cleanRelative) != "resources" {
		return nil, nil, fmt.Errorf("附件路径越界")
	}
	resourceInfo, err := os.Lstat(resourcesPath)
	if err != nil || !resourceInfo.IsDir() || resourceInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("附件根目录不安全")
	}
	targetPath := filepath.Join(filepath.Clean(meetingDirectory), cleanRelative)
	info, err := os.Lstat(targetPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("附件不是安全普通文件")
	}
	file, err := os.Open(targetPath)
	if err != nil {
		return nil, nil, fmt.Errorf("打开附件：%w", err)
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("附件在打开期间已变化")
	}
	return file, openedInfo, nil
}

// ServeDownload 输出 attachment disposition，并只允许 GET、HEAD 和单段 Range。
func ServeDownload(writer http.ResponseWriter, request *http.Request, file *os.File, originalName string, mediaType string) {
	if writer == nil || request == nil || file == nil {
		return
	}
	info, err := file.Stat()
	if err != nil {
		http.Error(writer, "attachment unavailable", http.StatusNotFound)
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Disposition", contentDisposition(originalName))
	if mediaType != "" {
		writer.Header().Set("Content-Type", mediaType)
	}
	if strings.Contains(request.Header.Get("Range"), ",") {
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", info.Size()))
		writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	http.ServeContent(writer, request, originalName, time.Time{}, file)
}

// contentDisposition 清理 CR/LF，同时提供 ASCII fallback 和 RFC 5987 UTF-8 文件名。
func contentDisposition(originalName string) string {
	clean := strings.ReplaceAll(strings.ReplaceAll(originalName, "\r", ""), "\n", "")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		clean = "attachment"
	}
	fallback := safeDispositionByte.ReplaceAllString(clean, "_")
	if fallback == "" {
		fallback = "attachment"
	}
	encoded := strings.ReplaceAll(url.PathEscape(clean), "+", "%20")
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + encoded
}
