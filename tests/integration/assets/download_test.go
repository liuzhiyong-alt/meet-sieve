package assets_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"meet-sieve/internal/infra/assets"
)

// TestDownloader_FetchesAndReusesVerifiedCache 验证下载内容通过哈希后才进入 cache，且可安全复用。
func TestDownloader_FetchesAndReusesVerifiedCache(t *testing.T) {
	t.Parallel()

	content := []byte("verified-runtime")
	var requests atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(content))),
			Header:     make(http.Header),
		}, nil
	})}

	asset := downloadAsset("https://github.com/example/runtime.tgz", content)
	cacheDir := t.TempDir()
	downloader := assets.NewDownloader(client)
	first, err := downloader.Fetch(context.Background(), asset, cacheDir)
	if err != nil {
		t.Fatalf("首次下载失败：%v", err)
	}
	second, err := downloader.Fetch(context.Background(), asset, cacheDir)
	if err != nil {
		t.Fatalf("复用 cache 失败：%v", err)
	}
	if first != second || requests.Load() != 1 {
		t.Fatalf("cache 没有复用：first=%s second=%s requests=%d", first, second, requests.Load())
	}
}

// TestDownloader_RemovesTemporaryFileWhenHashMismatch 验证哈希不一致时不保留归档或临时文件。
func TestDownloader_RemovesTemporaryFileWhenHashMismatch(t *testing.T) {
	t.Parallel()

	content := []byte("corrupted")
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(content))),
			Header:     make(http.Header),
		}, nil
	})}

	asset := downloadAsset("https://github.com/example/runtime.tgz", content)
	asset.ArchiveSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cacheDir := t.TempDir()
	if _, err := assets.NewDownloader(client).Fetch(context.Background(), asset, cacheDir); err == nil {
		t.Fatal("哈希不一致应下载失败")
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("读取 cache 失败：%v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("失败下载留下文件：%v", filepath.Join(cacheDir, entries[0].Name()))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func downloadAsset(url string, content []byte) assets.Asset {
	digest := sha256.Sum256(content)
	return assets.Asset{
		ID:            "onnxruntime",
		Version:       "1.26.0",
		OS:            "darwin",
		Arch:          "arm64",
		URL:           url,
		ArchiveType:   "tgz",
		ArchiveSHA256: hex.EncodeToString(digest[:]),
		ArchiveSize:   int64(len(content)),
		LibraryPath:   "lib/runtime.dylib",
		LibrarySHA256: hex.EncodeToString(digest[:]),
		LibrarySize:   int64(len(content)),
		LicensePath:   "LICENSE",
		License:       "MIT",
	}
}
