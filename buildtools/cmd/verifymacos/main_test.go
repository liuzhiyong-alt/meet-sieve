package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// TestVerifyMachOArm64 验证当前 arm64 测试程序可被架构门禁识别。
func TestVerifyMachOArm64(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("该测试只验证当前支持的 macOS arm64 构建宿主")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("定位测试程序失败：%v", err)
	}
	if err := verifyMachOArm64(executable); err != nil {
		t.Fatalf("当前 arm64 测试程序应通过：%v", err)
	}
}

// TestVerifyMachOArm64RejectsInvalidFile 验证普通文件不能冒充 macOS 应用二进制。
func TestVerifyMachOArm64RejectsInvalidFile(t *testing.T) {
	path := t.TempDir() + "/not-macho"
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("写入普通文件失败：%v", err)
	}
	if err := verifyMachOArm64(path); err == nil || !strings.Contains(err.Error(), "Mach-O") {
		t.Fatalf("普通文件应被拒绝：%v", err)
	}
}
