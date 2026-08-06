package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerifyInstallerScript_RequiresSharedSingleInstanceContract 验证 Windows 校验器拒绝缺失单实例契约的 NSIS 脚本。
func TestVerifyInstallerScript_RequiresSharedSingleInstanceContract(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "project.nsi")
	if err := os.WriteFile(path, []byte(validInstallerScript), 0o600); err != nil {
		t.Fatalf("写入测试脚本失败：%v", err)
	}
	if err := verifyInstallerScript(path); err != nil {
		t.Fatalf("完整单实例契约不应被拒绝：%v", err)
	}

	missingUninstall := strings.Replace(validInstallerScript, "Function un.onInit\n    !insertmacro EnsureMeetSieveNotRunning\nFunctionEnd\n", "", 1)
	if err := os.WriteFile(path, []byte(missingUninstall), 0o600); err != nil {
		t.Fatalf("写入不完整测试脚本失败：%v", err)
	}
	if err := verifyInstallerScript(path); err == nil || !strings.Contains(err.Error(), "卸载") {
		t.Fatalf("缺少卸载检查应返回可诊断错误：%v", err)
	}
}

// TestVerifyInstallerScript_RejectsEmbeddedVoiceModel 验证安装器源码不能重新把模型二进制打进包内。
func TestVerifyInstallerScript_RejectsEmbeddedVoiceModel(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "project.nsi")
	source := validInstallerScript + "\nFile model.onnx\n"
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("写入测试脚本失败：%v", err)
	}
	if err := verifyInstallerScript(path); err == nil || !strings.Contains(err.Error(), "声纹模型") {
		t.Fatalf("内置模型应被静态门禁拒绝：%v", err)
	}
}

// TestVerifyResources_RejectsVoiceModel 验证 Windows 资源目录只携带运行时，不携带模型权重。
func TestVerifyResources_RejectsVoiceModel(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "models"), 0o700); err != nil {
		t.Fatalf("创建测试资源目录失败：%v", err)
	}
	for _, filename := range []string{"onnxruntime.dll", "ONNXRUNTIME-LICENSE.txt", "campplus.onnx"} {
		if err := os.WriteFile(filepath.Join(directory, filename), []byte("asset"), 0o600); err != nil {
			t.Fatalf("写入测试资源失败：%v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "models", "voice-matching-profile.json"), []byte("profile"), 0o600); err != nil {
		t.Fatalf("写入测试 profile 失败：%v", err)
	}
	if err := verifyResources(directory); err == nil || !strings.Contains(err.Error(), "声纹模型") {
		t.Fatalf("资源目录内置模型应被拒绝：%v", err)
	}
}

const validInstallerScript = `!define MEETSIEVE_INSTANCE_MUTEX "Global\MeetSieve.App.Instance.v1"
!define MEETSIEVE_INSTANCE_RUNNING_MESSAGE "MeetSieve 正在运行，请先结束会议并退出应用后再继续。"
!macro EnsureMeetSieveNotRunning
    System::Call 'kernel32::OpenMutexW(i 0x00100000, i 0, w "${MEETSIEVE_INSTANCE_MUTEX}") p .r0'
    System::Call 'kernel32::CloseHandle(p r0)'
    MessageBox MB_ICONEXCLAMATION|MB_OK "${MEETSIEVE_INSTANCE_RUNNING_MESSAGE}"
    Abort
!macroend
Function .onInit
    !insertmacro EnsureMeetSieveNotRunning
FunctionEnd
Function un.onInit
    !insertmacro EnsureMeetSieveNotRunning
FunctionEnd
File "/oname=voice-matching-profile.json" "voice-matching-profile.json"
`
