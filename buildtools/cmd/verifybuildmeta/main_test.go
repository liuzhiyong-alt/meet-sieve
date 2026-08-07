package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyBuildMetadata 验证 semantic version、Windows 数字版本和 Wails 配置必须一致。
func TestVerifyBuildMetadata(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "wails.json")
	if err := os.WriteFile(configPath, []byte(`{"info":{"productVersion":"0.1.0.1"}}`), 0o600); err != nil {
		t.Fatalf("写入 Wails 配置失败：%v", err)
	}
	if err := verifyBuildMetadata("0.1.0-alpha.1", "0.1.0.1", configPath); err != nil {
		t.Fatalf("合法构建身份不应失败：%v", err)
	}

	for _, testCase := range []struct {
		name        string
		version     string
		fileVersion string
		contains    string
	}{
		{name: "非法 semantic version", version: "alpha", fileVersion: "0.1.0.1", contains: "BUILD_VERSION"},
		{name: "非法 Windows 版本", version: "0.1.0-alpha.1", fileVersion: "0.1", contains: "WINDOWS_FILE_VERSION"},
		{name: "配置不一致", version: "0.1.0-alpha.1", fileVersion: "0.1.0.2", contains: "不一致"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := verifyBuildMetadata(testCase.version, testCase.fileVersion, configPath)
			if err == nil || !strings.Contains(err.Error(), testCase.contains) {
				t.Fatalf("预期包含 %q 的错误，实际 %v", testCase.contains, err)
			}
		})
	}
}
