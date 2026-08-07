// Command verifywindows 静态校验 Windows PE、CGO 链接和 NSIS 产物。
package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/pe"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"meet-sieve/buildtools/internal/packagecheck"
	"meet-sieve/internal/infra/singleinstance"
)

const (
	windowsGUISubsystem = 2
	installerScriptPath = "build/windows/installer/project.nsi"
	assetsLockPath      = "third_party/assets.lock.json"
)

type installMarker struct {
	SchemaVersion int    `json:"schema_version"`
	ProductID     string `json:"product_id"`
	Version       string `json:"version"`
	Arch          string `json:"arch"`
	Commit        string `json:"commit"`
	BuildTime     string `json:"build_time"`
}

type installedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type installedFileManifest struct {
	SchemaVersion int             `json:"schema_version"`
	Files         []installedFile `json:"files"`
}

// main 执行 Windows 产物静态校验并通过退出码报告结果。
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run 解析产物路径并依次执行可执行文件、资源和安装包校验。
func run() error {
	executable := flag.String("exe", "build/bin/MeetSieve.exe", "Windows GUI exe")
	installer := flag.String("installer", "", "NSIS installer；为空时自动查找")
	resources := flag.String("resources", "build/bin/windows-resources", "Windows 运行资源目录")
	flag.Parse()

	if err := verifyInstallerScript(installerScriptPath); err != nil {
		return err
	}
	if err := verifyExecutable(*executable); err != nil {
		return err
	}
	if err := verifyResources(*resources, *executable, assetsLockPath); err != nil {
		return err
	}
	installerPath, err := resolveInstaller(*installer)
	if err != nil {
		return err
	}
	if err := verifyInstaller(installerPath); err != nil {
		return err
	}
	fmt.Printf("verified exe=%s installer=%s\n", *executable, installerPath)
	return nil
}

// verifyInstallerScript 校验 NSIS 源码与应用共用的单实例阻断契约。
func verifyInstallerScript(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 NSIS 脚本失败: %w", err)
	}

	source := string(data)
	requiredFragments := []struct {
		name     string
		fragment string
	}{
		{name: "共享 mutex", fragment: `!define MEETSIEVE_INSTANCE_MUTEX "` + singleinstance.WindowsMutexName + `"`},
		{name: "运行中提示", fragment: `!define MEETSIEVE_INSTANCE_RUNNING_MESSAGE "MeetSieve 正在运行，请先结束会议并退出应用后再继续。"`},
		{name: "阻断宏", fragment: "!macro EnsureMeetSieveNotRunning"},
		{name: "OpenMutex 调用", fragment: "kernel32::OpenMutexW"},
		{name: "CloseHandle 调用", fragment: "kernel32::CloseHandle"},
		{name: "阻断提示", fragment: "MessageBox MB_ICONEXCLAMATION|MB_OK"},
		{name: "安装入口", fragment: "Function .onInit\n    !insertmacro EnsureMeetSieveNotRunning"},
		{name: "卸载入口", fragment: "Function un.onInit\n    !insertmacro EnsureMeetSieveNotRunning"},
		{name: "正式声纹档案", fragment: `File "/oname=voice-matching-profile.json"`},
		{name: "简体中文", fragment: `!insertmacro MUI_LANGUAGE "SimpChinese"`},
		{name: "组件页", fragment: "!insertmacro MUI_PAGE_COMPONENTS"},
		{name: "安全目录校验", fragment: "Function ValidateInstallDirectory"},
		{name: "默认专属目录", fragment: `InstallDir "$PROGRAMFILES64\${INFO_PRODUCTNAME}"`},
		{name: "旧安装位置", fragment: `InstallDirRegKey HKLM "${UNINST_KEY}" "InstallLocation"`},
		{name: "产品标识", fragment: `File "/oname=meetsieve-install.json"`},
		{name: "安装清单", fragment: `File "/oname=meetsieve-files.json"`},
		{name: "桌面组件", fragment: `Section "桌面快捷方式"`},
		{name: "防火墙组件", fragment: `Section "局域网访客防火墙规则"`},
		{name: "Private profile", fragment: "profile=private"},
		{name: "显式删除主程序", fragment: `Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"`},
		{name: "仅删除空安装目录", fragment: `RMDir "$INSTDIR"`},
	}
	for _, required := range requiredFragments {
		if !strings.Contains(source, required.fragment) {
			return fmt.Errorf("NSIS 单实例契约缺少%s", required.name)
		}
	}
	for _, forbidden := range []string{
		"taskkill", "TerminateProcess", `RMDir /r $INSTDIR`, `RMDir /r "$INSTDIR"`,
		`RMDir /r "$AppData`, `%LocalAppData%\MeetSieve`,
	} {
		if strings.Contains(source, forbidden) {
			return fmt.Errorf("NSIS 包含禁止的安装或卸载行为: %s", forbidden)
		}
	}
	if containsVoiceModelReference(source) {
		return fmt.Errorf("NSIS 安装器不得内置声纹模型")
	}
	return nil
}

// verifyExecutable 校验主程序是 Windows amd64 GUI PE，并确认基础 CGO 依赖已链接。
func verifyExecutable(path string) error {
	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("打开 Windows PE 失败: %w", err)
	}
	defer file.Close()
	if file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return fmt.Errorf("PE 架构不是 AMD64: 0x%x", file.FileHeader.Machine)
	}
	header, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		return fmt.Errorf("PE 不是 64 位可执行文件")
	}
	if header.Subsystem != windowsGUISubsystem {
		return fmt.Errorf("PE subsystem 不是 Windows GUI: %d", header.Subsystem)
	}

	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 Windows Go build info 失败: %w", err)
	}
	requiredModules := map[string]bool{
		"github.com/gen2brain/malgo":      false,
		"github.com/mattn/go-sqlite3":     false,
		"github.com/yalue/onnxruntime_go": false,
		"gorm.io/driver/sqlite":           false,
	}
	for _, dependency := range info.Deps {
		if _, required := requiredModules[dependency.Path]; required {
			requiredModules[dependency.Path] = true
		}
	}
	for module, linked := range requiredModules {
		if !linked {
			return fmt.Errorf("Windows PE 缺少基础依赖模块: %s", module)
		}
	}
	return nil
}

// verifyResources 校验安装包依赖的运行时、许可证和正式声纹档案。
func verifyResources(directory string, executablePath string, assetsPath string) error {
	if err := packagecheck.RejectEmbeddedVoiceModel(directory); err != nil {
		return err
	}
	manifest, err := packagecheck.LoadManifest(assetsPath)
	if err != nil {
		return err
	}
	asset, err := manifest.Select("onnxruntime", "windows", "amd64")
	if err != nil {
		return err
	}
	if err := packagecheck.VerifyLockedFile(filepath.Join(directory, "onnxruntime.dll"), asset.LibrarySize, asset.LibrarySHA256); err != nil {
		return err
	}
	if err := packagecheck.VerifyLicense(filepath.Join(directory, "ONNXRUNTIME-LICENSE.txt")); err != nil {
		return err
	}
	if err := packagecheck.VerifyVoiceProfile(filepath.Join(directory, "models", "voice-matching-profile.json"), manifest); err != nil {
		return err
	}
	for _, filename := range []string{"meetsieve-install.json", "meetsieve-files.json"} {
		info, err := os.Stat(filepath.Join(directory, filename))
		if err != nil {
			return fmt.Errorf("Windows 资源缺失 %s: %w", filename, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("Windows 资源不合法: %s", filename)
		}
	}
	if err := verifyInstallationMetadata(directory, executablePath); err != nil {
		return err
	}
	return nil
}

// verifyInstallationMetadata 严格校验产品标识和编译期安装清单，不把清单当动态删除指令。
func verifyInstallationMetadata(directory string, executablePath string) error {
	markerContent, err := os.ReadFile(filepath.Join(directory, "meetsieve-install.json"))
	if err != nil {
		return err
	}
	var marker installMarker
	if err := decodeStrictJSON(markerContent, &marker); err != nil {
		return fmt.Errorf("解析 Windows 产品标识失败: %w", err)
	}
	if marker.SchemaVersion != 1 || marker.ProductID != "meet-sieve" || marker.Version == "" || marker.Arch != "amd64" || len(marker.Commit) != 40 || marker.BuildTime == "" {
		return fmt.Errorf("Windows 产品标识不合法")
	}

	manifestContent, err := os.ReadFile(filepath.Join(directory, "meetsieve-files.json"))
	if err != nil {
		return err
	}
	var manifest installedFileManifest
	if err := decodeStrictJSON(manifestContent, &manifest); err != nil {
		return fmt.Errorf("解析 Windows 安装清单失败: %w", err)
	}
	if manifest.SchemaVersion != 1 || len(manifest.Files) != 4 {
		return fmt.Errorf("Windows 安装清单版本或文件数量不合法")
	}

	expectedPaths := map[string]string{
		"MeetSieve.exe":                      executablePath,
		"onnxruntime.dll":                    filepath.Join(directory, "onnxruntime.dll"),
		"ONNXRUNTIME-LICENSE.txt":            filepath.Join(directory, "ONNXRUNTIME-LICENSE.txt"),
		"models/voice-matching-profile.json": filepath.Join(directory, "models", "voice-matching-profile.json"),
	}
	for _, entry := range manifest.Files {
		if filepath.IsAbs(entry.Path) || filepath.Clean(entry.Path) != entry.Path || strings.Contains(entry.Path, "..") {
			return fmt.Errorf("Windows 安装清单包含不安全路径: %s", entry.Path)
		}
		actualPath, exists := expectedPaths[entry.Path]
		if !exists {
			return fmt.Errorf("Windows 安装清单包含未登记文件: %s", entry.Path)
		}
		actualSHA, err := packagecheck.FileSHA256(actualPath)
		if err != nil {
			return err
		}
		if entry.SHA256 != actualSHA {
			return fmt.Errorf("Windows 安装清单 SHA-256 不一致: %s", entry.Path)
		}
		delete(expectedPaths, entry.Path)
	}
	if len(expectedPaths) != 0 {
		return fmt.Errorf("Windows 安装清单缺少必装文件")
	}
	return nil
}

// decodeStrictJSON 拒绝安装元数据中的未知字段、尾随对象和不完整 JSON。
func decodeStrictJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON 包含尾随对象")
		}
		return err
	}
	return nil
}

// containsVoiceModelReference 识别当前官方模型文件及通用 ONNX 权重引用，保留运行时 DLL。
func containsVoiceModelReference(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, ".onnx") || strings.Contains(lower, "campplus") || strings.Contains(lower, "voice-model")
}

// resolveInstaller 返回显式安装包，或从构建目录中解析唯一安装包。
func resolveInstaller(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	matches, err := filepath.Glob("build/bin/MeetSieve-*-windows-amd64-installer.exe")
	if err != nil {
		return "", fmt.Errorf("查找 NSIS installer 失败: %w", err)
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("预期一个 NSIS installer，实际 %d 个", len(matches))
	}
	return matches[0], nil
}

// verifyInstaller 校验安装包是包含应用资源的 NSIS PE，而不是裸主程序。
func verifyInstaller(path string) error {
	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("NSIS 产物不是有效 PE: %w", err)
	}
	defer file.Close()
	if file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		return fmt.Errorf("NSIS stub 架构不正确: 0x%x", file.FileHeader.Machine)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("读取 NSIS 产物失败: %w", err)
	}
	if info.Size() <= 1024*1024 {
		return fmt.Errorf("NSIS 产物过小，未包含应用资源: %d", info.Size())
	}
	return nil
}
