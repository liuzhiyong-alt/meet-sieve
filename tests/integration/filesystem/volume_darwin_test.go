//go:build darwin

package filesystem_test

import (
	"testing"

	"meet-sieve/internal/infra/filesystem"
)

// TestDetectVolume_ClassifiesTemporaryDirectoryAsLocal 验证 macOS 真实临时目录所在卷可被识别为本地卷。
func TestDetectVolume_ClassifiesTemporaryDirectoryAsLocal(t *testing.T) {
	path, err := filesystem.CanonicalizePath(t.TempDir())
	if err != nil {
		t.Fatalf("规范化临时目录失败：%v", err)
	}
	kind, err := filesystem.DetectVolume(path)
	if err != nil {
		t.Fatalf("检测临时目录卷类型失败：%v", err)
	}
	if kind != filesystem.VolumeLocal {
		t.Fatalf("临时目录应位于本地卷：%q", kind)
	}
}
