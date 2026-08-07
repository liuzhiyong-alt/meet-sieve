package assets

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"meet-sieve/internal/infra/filesystem"
)

const downloadTimeout = 5 * time.Minute

// Downloader 下载资源归档，并在哈希通过后原子写入 cache。
type Downloader struct {
	client *http.Client
}

// NewDownloader 创建使用指定 HTTP client 的下载器。
func NewDownloader(client *http.Client) *Downloader {
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}
	return &Downloader{client: client}
}

// NewLocalProxyHTTPClient 创建不继承进程环境的下载客户端；零端口表示直接访问外网。
func NewLocalProxyHTTPClient(proxyPort int) (*http.Client, error) {
	if proxyPort < 0 || proxyPort > 65535 {
		return nil, fmt.Errorf("本机代理端口不合法")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// 设置页的零值语义是直连，不能让开发终端环境中的代理悄然改变该行为。
	transport.Proxy = nil
	if proxyPort > 0 {
		proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort))}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Timeout: downloadTimeout, Transport: transport}, nil
}

// Fetch 下载或复用已校验的归档，返回 cache 中的绝对路径。
func (d *Downloader) Fetch(ctx context.Context, asset Asset, cacheDir string) (string, error) {
	filename, err := asset.ArchiveFilename()
	if err != nil {
		return "", err
	}
	return d.fetch(ctx, asset.URL, filename, asset.ArchiveSHA256, asset.ArchiveSize, cacheDir)
}

// FetchVoiceModel 下载或复用已校验的官方声纹模型包。
func (d *Downloader) FetchVoiceModel(ctx context.Context, asset VoiceModelAsset, cacheDir string) (string, error) {
	filename, err := asset.ArchiveFilename()
	if err != nil {
		return "", err
	}
	return d.fetch(ctx, asset.URL, filename, asset.ArchiveSHA256, asset.ArchiveSize, cacheDir)
}

// fetch 统一执行受锁定 URL、大小和哈希约束的下载。
func (d *Downloader) fetch(ctx context.Context, rawURL string, filename string, expectedSHA string, expectedSize int64, cacheDir string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("创建资源 cache 失败: %w", err)
	}
	target := filepath.Join(cacheDir, filename)
	if isVerifiedFile(target, expectedSHA, expectedSize) {
		return target, nil
	}
	return d.download(ctx, rawURL, expectedSHA, expectedSize, target)
}

// download 将响应写入临时文件，完整校验后再原子替换目标文件。
func (d *Downloader) download(ctx context.Context, rawURL string, expectedSHA string, expectedSize int64, target string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建资源下载请求失败: %w", err)
	}
	response, err := d.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("下载第三方资源失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载第三方资源失败: HTTP %d", response.StatusCode)
	}

	temporary, err := os.CreateTemp(filepath.Dir(target), ".asset-download-*")
	if err != nil {
		return "", fmt.Errorf("创建资源临时文件失败: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := copyAndSync(temporary, response.Body); err != nil {
		return "", err
	}
	if !isVerifiedFile(temporaryPath, expectedSHA, expectedSize) {
		return "", fmt.Errorf("资源大小或 SHA-256 校验失败")
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return "", fmt.Errorf("保存已校验资源失败: %w", err)
	}
	return target, nil
}

// VerifyFile 检查文件大小和 SHA-256 是否与锁定值一致。
func VerifyFile(path string, expectedSHA string, expectedSize int64) bool {
	return isVerifiedFile(path, expectedSHA, expectedSize)
}

// copyAndSync 完整复制响应内容，并在关闭前同步到磁盘。
func copyAndSync(target *os.File, source io.Reader) error {
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return fmt.Errorf("写入资源临时文件失败: %w", err)
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		return fmt.Errorf("同步资源临时文件失败: %w", err)
	}
	if err := target.Close(); err != nil {
		return fmt.Errorf("关闭资源临时文件失败: %w", err)
	}
	return nil
}

// isVerifiedFile 检查文件大小和 SHA-256 是否与资源锁一致。
func isVerifiedFile(path string, expectedSHA string, expectedSize int64) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() != expectedSize {
		return false
	}
	actualSHA, err := filesystem.SHA256File(path)
	return err == nil && actualSHA == expectedSHA
}
