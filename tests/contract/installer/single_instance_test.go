package installer_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	expectedMutexDefinition = `!define MEETSIEVE_INSTANCE_MUTEX "Global\MeetSieve.App.Instance.v1"`
	expectedRunningMessage  = `!define MEETSIEVE_INSTANCE_RUNNING_MESSAGE "MeetSieve 正在运行，请先结束会议并退出应用后再继续。"`
)

// TestProjectNSIS_BlocksInstallAndUninstallWhenAppRuns 验证安装器和卸载器共用单实例阻断契约。
func TestProjectNSIS_BlocksInstallAndUninstallWhenAppRuns(t *testing.T) {
	t.Parallel()

	source := loadProjectNSIS(t)
	for _, expected := range []string{
		expectedMutexDefinition,
		expectedRunningMessage,
		"!macro EnsureMeetSieveNotRunning",
		"Function .onInit\n    !insertmacro EnsureMeetSieveNotRunning",
		"Function un.onInit\n    !insertmacro EnsureMeetSieveNotRunning",
		"kernel32::OpenMutexW",
		"kernel32::CloseHandle",
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("project.nsi 缺少单实例契约片段：%q", expected)
		}
	}
	if strings.Contains(source, "taskkill") || strings.Contains(source, "TerminateProcess") {
		t.Fatal("安装器不能强制结束正在运行的 MeetSieve")
	}
}

// loadProjectNSIS 读取仓库中的 NSIS 源文件，避免依赖构建产物。
func loadProjectNSIS(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位当前测试文件")
	}
	path := filepath.Join(filepath.Dir(filename), "../../../build/windows/installer/project.nsi")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 project.nsi 失败：%v", err)
	}
	return string(data)
}
