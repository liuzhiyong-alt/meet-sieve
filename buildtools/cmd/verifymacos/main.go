// Command verifymacos 校验 macOS arm64 应用、DMG 和锁定安装资源。
package main

import (
	"debug/macho"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"meet-sieve/buildtools/internal/packagecheck"
)

const (
	defaultAssetsPath = "third_party/assets.lock.json"
	macOSExecutable   = "Contents/MacOS/MeetSieve"
	macOSRuntime      = "Contents/Resources/lib/libonnxruntime.1.26.0.dylib"
	macOSLicense      = "Contents/Resources/ONNXRUNTIME-LICENSE.txt"
	macOSProfile      = "Contents/Resources/models/voice-matching-profile.json"
)

// main 解析产物路径并通过退出码报告校验结果。
func main() {
	appPath := flag.String("app", "build/bin/MeetSieve.app", "macOS 应用路径")
	dmgPath := flag.String("dmg", "", "DMG 路径；为空时只校验应用")
	assetsPath := flag.String("assets", defaultAssetsPath, "第三方资源锁路径")
	flag.Parse()

	if err := verifyMacOSPackage(*appPath, *dmgPath, *assetsPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("verified app=%s dmg=%s\n", *appPath, *dmgPath)
}

// verifyMacOSPackage 校验应用架构、签名、锁定资源，并在提供 DMG 时验证镜像结构。
func verifyMacOSPackage(appPath string, dmgPath string, assetsPath string) error {
	manifest, err := packagecheck.LoadManifest(assetsPath)
	if err != nil {
		return err
	}
	asset, err := manifest.Select("onnxruntime", "darwin", "arm64")
	if err != nil {
		return err
	}

	if err := verifyMachOArm64(filepath.Join(appPath, macOSExecutable)); err != nil {
		return err
	}
	runtimePath := filepath.Join(appPath, macOSRuntime)
	if err := verifyMachOArm64(runtimePath); err != nil {
		return err
	}
	if err := packagecheck.VerifyLockedFile(runtimePath, asset.LibrarySize, asset.LibrarySHA256); err != nil {
		return err
	}
	if err := packagecheck.VerifyLicense(filepath.Join(appPath, macOSLicense)); err != nil {
		return err
	}
	if err := packagecheck.VerifyVoiceProfile(filepath.Join(appPath, macOSProfile), manifest); err != nil {
		return err
	}
	if err := packagecheck.RejectEmbeddedVoiceModel(filepath.Join(appPath, "Contents/Resources")); err != nil {
		return err
	}
	if err := runCommand("codesign", "--verify", "--deep", "--strict", appPath); err != nil {
		return fmt.Errorf("macOS 应用 ad-hoc codesign 校验失败: %w", err)
	}
	if dmgPath != "" {
		if err := packagecheck.VerifyLicense(dmgPath); err != nil {
			return fmt.Errorf("DMG 不合法: %w", err)
		}
		if err := runCommand("hdiutil", "verify", dmgPath); err != nil {
			return fmt.Errorf("DMG 校验失败: %w", err)
		}
	}
	return nil
}

// verifyMachOArm64 校验目标是单架构 arm64 Mach-O 文件。
func verifyMachOArm64(path string) error {
	file, err := macho.Open(path)
	if err != nil {
		return fmt.Errorf("打开 Mach-O 失败 %s: %w", path, err)
	}
	defer file.Close()
	if file.Cpu != macho.CpuArm64 {
		return fmt.Errorf("Mach-O 不是 arm64: %s cpu=%v", path, file.Cpu)
	}
	return nil
}

// runCommand 执行只读系统校验命令，并保留其诊断输出。
func runCommand(name string, arguments ...string) error {
	command := exec.Command(name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", string(output), err)
	}
	return nil
}
