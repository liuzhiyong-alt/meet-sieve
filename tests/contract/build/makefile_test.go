package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRaceTargetIncludesStep5ConcurrencyPackages 验证 Step 5 的并发服务不会漏过 race 门禁。
func TestRaceTargetIncludesStep5ConcurrencyPackages(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("读取 Makefile 失败：%v", err)
	}

	makefile := string(content)
	for _, packagePath := range []string{
		"./internal/domain/speaker",
		"./internal/domain/correction",
		"./internal/service/speaker",
		"./internal/service/correction",
	} {
		if !strings.Contains(makefile, packagePath) {
			t.Errorf("test-race 必须覆盖 %s", packagePath)
		}
	}
}

// TestMakefileProvidesStep10PackageTargets 验证 Step 10 双平台构建、打包和校验入口稳定存在。
func TestMakefileProvidesStep10PackageTargets(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("读取 Makefile 失败：%v", err)
	}

	makefile := string(content)
	for _, target := range []string{
		"build-macos-arm64:",
		"package-macos:",
		"verify-macos-package:",
		"verify-windows-package:",
		"verify-package:",
	} {
		if !strings.Contains(makefile, target) {
			t.Errorf("Makefile 缺少 Step 10 稳定入口 %s", target)
		}
	}
	for _, variable := range []string{
		"BUILD_VERSION ?= 0.1.0-alpha.1",
		"WINDOWS_FILE_VERSION ?= 0.1.0.1",
	} {
		if !strings.Contains(makefile, variable) {
			t.Errorf("Makefile 缺少统一构建身份 %s", variable)
		}
	}
}

// TestWindowsManifestUsesFourPartProductVersion 验证 Windows assembly identity 不追加第五段版本。
func TestWindowsManifestUsesFourPartProductVersion(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "build", "windows", "wails.exe.manifest"))
	if err != nil {
		t.Fatalf("读取 Windows manifest 失败：%v", err)
	}
	source := string(content)
	if !strings.Contains(source, `version="{{.Info.ProductVersion}}"`) {
		t.Fatal("Windows assembly identity 必须直接使用四段 ProductVersion")
	}
	if strings.Contains(source, `version="{{.Info.ProductVersion}}.0"`) {
		t.Fatal("Windows assembly identity 不能追加第五段版本")
	}
}
