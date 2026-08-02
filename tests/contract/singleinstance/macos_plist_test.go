package singleinstance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	meetSieveBundleIdentifier = "com.meetsieve.app"
	plistBundleIdentifierKey  = "<key>CFBundleIdentifier</key>"
	plistSingleInstanceKey    = "<key>LSMultipleInstancesProhibited</key>"
)

// TestMacOSInfoPlists_DeclareStableSingleInstanceApp 验证开发和生产应用包均由 LaunchServices 管理单实例。
func TestMacOSInfoPlists_DeclareStableSingleInstanceApp(t *testing.T) {
	for _, path := range []string{
		filepath.Join("build", "darwin", "Info.plist"),
		filepath.Join("build", "darwin", "Info.dev.plist"),
	} {
		content, err := os.ReadFile(filepath.Join("..", "..", "..", path))
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", path, err)
		}

		assertPlistDeclaresSingleInstance(t, path, string(content))
	}
}

// assertPlistDeclaresSingleInstance 断言 Wails plist 模板保留固定 bundle ID 和单实例声明。
func assertPlistDeclaresSingleInstance(t *testing.T, path, content string) {
	t.Helper()
	if !strings.Contains(content, plistBundleIdentifierKey+"\n        <string>"+meetSieveBundleIdentifier+"</string>") {
		t.Fatalf("%s 未声明固定 bundle identifier %q", path, meetSieveBundleIdentifier)
	}
	if !strings.Contains(content, plistSingleInstanceKey+"\n        <true/>") {
		t.Fatalf("%s 未声明 LaunchServices 单实例", path)
	}
}
