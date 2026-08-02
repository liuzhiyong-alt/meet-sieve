package singleinstance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDesktopMain_ChecksSingleInstanceBeforeBootstrap 验证第二实例不会触发工作目录或 SQLite 初始化。
func TestDesktopMain_ChecksSingleInstanceBeforeBootstrap(t *testing.T) {
	path := filepath.Join("..", "..", "..", "cmd", "meetsieve", "main.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取桌面入口失败：%v", err)
	}
	source := string(content)

	acquireIndex := strings.Index(source, "singleinstance.Acquire()")
	bootstrapIndex := strings.Index(source, "application.NewBootstrap(")
	if acquireIndex < 0 || bootstrapIndex < 0 || acquireIndex > bootstrapIndex {
		t.Fatal("桌面入口必须在 Bootstrap 前取得单实例所有权")
	}
	if !strings.Contains(source, "outcome == singleinstance.OutcomeAlreadyRunning") {
		t.Fatal("桌面入口必须在已有实例时结束第二进程")
	}
}

// TestDesktopMain_RegistersWindowActivationHandler 验证 Windows 管道 activate 能唤起 Wails 主窗口。
func TestDesktopMain_RegistersWindowActivationHandler(t *testing.T) {
	mainSource := readContractSource(t, filepath.Join("..", "..", "..", "cmd", "meetsieve", "main.go"))
	appSource := readContractSource(t, filepath.Join("..", "..", "..", "cmd", "meetsieve", "app.go"))
	if !strings.Contains(mainSource, "lease.SetActivationHandler") {
		t.Fatal("桌面入口必须在 Wails 启动后注册 activate 处理函数")
	}
	if !strings.Contains(appSource, "runtime.WindowUnminimise") || !strings.Contains(appSource, "runtime.WindowShow") {
		t.Fatal("activate 处理函数必须取消最小化并显示 Wails 主窗口")
	}
}

// TestDesktopMain_DeclaresDesktopWindowBounds 验证工作目录引导在默认和最小桌面尺寸下都具备可用布局空间。
func TestDesktopMain_DeclaresDesktopWindowBounds(t *testing.T) {
	mainSource := readContractSource(t, filepath.Join("..", "..", "..", "cmd", "meetsieve", "main.go"))
	compactSource := strings.ReplaceAll(mainSource, " ", "")
	for _, requirement := range []string{"Width:1280", "Height:800", "MinWidth:1024", "MinHeight:720"} {
		if !strings.Contains(compactSource, requirement) {
			t.Fatalf("Wails 桌面窗口必须声明 %s", requirement)
		}
	}
}

// readContractSource 读取仓库源文件并统一处理失败。
func readContractSource(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v", path, err)
	}
	return string(content)
}
