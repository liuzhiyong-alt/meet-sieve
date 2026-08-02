// Command assets 下载、校验并解压资源锁中的第三方二进制。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	"meet-sieve/internal/infra/assets"
)

// main 执行资源准备命令并通过退出码报告结果。
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run 解析命令参数，并按资源锁准备目标平台的第三方资源。
func run() error {
	manifestPath := flag.String("manifest", "third_party/assets.lock.json", "资源锁路径")
	cacheDir := flag.String("cache", ".cache/third_party", "资源 cache 目录")
	all := flag.Bool("all", false, "准备资源锁中的全部目标平台")
	flag.Parse()

	data, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("读取资源锁失败: %w", err)
	}
	manifest, err := assets.ParseManifest(data)
	if err != nil {
		return err
	}
	selected, err := selectAssets(manifest, *all)
	if err != nil {
		return err
	}

	downloader := assets.NewDownloader(nil)
	for _, asset := range selected {
		archivePath, err := downloader.Fetch(context.Background(), asset, *cacheDir)
		if err != nil {
			return fmt.Errorf("准备 %s/%s 资源失败: %w", asset.OS, asset.Arch, err)
		}
		libraryPath, err := assets.Extract(asset, archivePath, *cacheDir)
		if err != nil {
			return fmt.Errorf("解压 %s/%s 资源失败: %w", asset.OS, asset.Arch, err)
		}
		fmt.Printf("%s/%s %s\n", asset.OS, asset.Arch, libraryPath)
	}
	return nil
}

// selectAssets 返回全部资源，或只返回当前运行平台的 ONNX Runtime 资源。
func selectAssets(manifest assets.Manifest, all bool) ([]assets.Asset, error) {
	if all {
		return manifest.Assets, nil
	}
	asset, err := manifest.Select("onnxruntime", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	return []assets.Asset{asset}, nil
}
