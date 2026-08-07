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

// TestProjectNSIS_ProvidesSafeChineseInstallContract 验证安装器提供中文、安全目录、组件和升级位置契约。
func TestProjectNSIS_ProvidesSafeChineseInstallContract(t *testing.T) {
	t.Parallel()

	source := loadProjectNSIS(t)
	for _, expected := range []string{
		`!insertmacro MUI_LANGUAGE "SimpChinese"`,
		"!insertmacro MUI_PAGE_COMPONENTS",
		`InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}"`,
		`InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"`,
		"Function ValidateInstallDirectory",
		`File "/oname=meetsieve-install.json"`,
		`File "/oname=meetsieve-files.json"`,
		`WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"`,
		`Section "桌面快捷方式"`,
		`Section "局域网访客防火墙规则"`,
		`profile=private`,
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("project.nsi 缺少安全安装契约片段：%q", expected)
		}
	}
}

// TestProjectNSIS_UninstallUsesExplicitProductManifest 验证卸载不递归删除安装目录或用户数据。
func TestProjectNSIS_UninstallUsesExplicitProductManifest(t *testing.T) {
	t.Parallel()

	source := loadProjectNSIS(t)
	for _, forbidden := range []string{
		`RMDir /r $INSTDIR`,
		`RMDir /r "$INSTDIR"`,
		`RMDir /r "$AppData`,
		`%LocalAppData%\MeetSieve`,
		"taskkill",
		"TerminateProcess",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("project.nsi 包含危险卸载行为：%q", forbidden)
		}
	}
	for _, expected := range []string{
		`Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"`,
		`Delete "$INSTDIR\onnxruntime.dll"`,
		`Delete "$INSTDIR\ONNXRUNTIME-LICENSE.txt"`,
		`Delete "$INSTDIR\models\voice-matching-profile.json"`,
		`Delete "$INSTDIR\meetsieve-install.json"`,
		`Delete "$INSTDIR\meetsieve-files.json"`,
		`RMDir "$INSTDIR\models"`,
		`RMDir "$INSTDIR"`,
	} {
		if !strings.Contains(source, expected) {
			t.Errorf("project.nsi 缺少显式卸载清单：%q", expected)
		}
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
