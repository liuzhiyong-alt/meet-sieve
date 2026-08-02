package filesystem

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RuntimeEnvironment 描述解析平台运行目录所需的显式环境。
type RuntimeEnvironment struct {
	// GOOS 是待解析的目标操作系统。
	GOOS string
	// HomeDir 是 macOS 用户主目录。
	HomeDir string
	// LocalAppData 是 Windows 当前用户本地应用数据目录。
	LocalAppData string
	// AppData 是 Windows 当前用户 roaming 应用数据目录。
	AppData string
}

// CurrentAppDataDir 返回当前用户保存模型等可重建应用数据的目录。
func CurrentAppDataDir() (string, error) {
	homeDir, homeErr := os.UserHomeDir()
	if homeErr != nil && runtime.GOOS == "darwin" {
		return "", fmt.Errorf("读取用户目录失败: %w", homeErr)
	}
	return ResolveAppDataDir(RuntimeEnvironment{
		GOOS:         runtime.GOOS,
		HomeDir:      homeDir,
		LocalAppData: os.Getenv("LOCALAPPDATA"),
	})
}

// ResolveAppDataDir 根据显式平台环境解析每用户应用数据目录。
func ResolveAppDataDir(env RuntimeEnvironment) (string, error) {
	switch env.GOOS {
	case "darwin":
		if strings.TrimSpace(env.HomeDir) == "" {
			return "", fmt.Errorf("macOS 用户目录不能为空")
		}
		return filepath.Join(env.HomeDir, "Library", "Application Support", "MeetSieve"), nil
	case "windows":
		if strings.TrimSpace(env.LocalAppData) == "" {
			return "", fmt.Errorf("Windows LOCALAPPDATA 不能为空")
		}
		return filepath.Join(env.LocalAppData, "MeetSieve"), nil
	default:
		return "", fmt.Errorf("不支持的运行平台: %s", env.GOOS)
	}
}

// ResolveBundledONNXLibrary 返回安装程序放置的固定 ONNX Runtime 动态库路径。
func ResolveBundledONNXLibrary(installRoot string, targetOS string) string {
	switch targetOS {
	case "darwin":
		return filepath.Join(installRoot, "Contents", "Resources", "lib", "libonnxruntime.1.26.0.dylib")
	case "windows":
		return filepath.Join(installRoot, "onnxruntime.dll")
	default:
		return ""
	}
}

// CurrentAppConfigDir 返回当前平台保存 locator 的 MeetSieve 系统应用目录。
func CurrentAppConfigDir() (string, error) {
	homeDir, homeErr := os.UserHomeDir()
	if homeErr != nil && runtime.GOOS == "darwin" {
		return "", fmt.Errorf("读取用户目录失败: %w", homeErr)
	}
	return ResolveAppConfigDir(RuntimeEnvironment{
		GOOS:    runtime.GOOS,
		HomeDir: homeDir,
		AppData: os.Getenv("APPDATA"),
	})
}

// ResolveAppConfigDir 根据显式平台环境解析 locator 目录，便于跨平台单元测试。
func ResolveAppConfigDir(env RuntimeEnvironment) (string, error) {
	switch env.GOOS {
	case "darwin":
		if strings.TrimSpace(env.HomeDir) == "" {
			return "", fmt.Errorf("macOS 用户目录不能为空")
		}
		return filepath.Join(env.HomeDir, "Library", "Application Support", "MeetSieve"), nil
	case "windows":
		if strings.TrimSpace(env.AppData) == "" {
			return "", fmt.Errorf("Windows APPDATA 不能为空")
		}
		return filepath.Join(env.AppData, "MeetSieve"), nil
	default:
		return "", fmt.Errorf("不支持的运行平台: %s", env.GOOS)
	}
}

// CurrentLogDir 返回当前平台的 MeetSieve 日志目录。
func CurrentLogDir() (string, error) {
	homeDir, homeErr := os.UserHomeDir()
	if homeErr != nil && runtime.GOOS == "darwin" {
		return "", fmt.Errorf("读取用户目录失败: %w", homeErr)
	}
	return ResolveLogDir(RuntimeEnvironment{
		GOOS:         runtime.GOOS,
		HomeDir:      homeDir,
		LocalAppData: os.Getenv("LOCALAPPDATA"),
	})
}

// ResolveLogDir 根据显式平台环境解析日志目录，便于跨平台单元测试。
func ResolveLogDir(env RuntimeEnvironment) (string, error) {
	switch env.GOOS {
	case "darwin":
		if strings.TrimSpace(env.HomeDir) == "" {
			return "", fmt.Errorf("macOS 用户目录不能为空")
		}
		return filepath.Join(env.HomeDir, "Library", "Logs", "MeetSieve"), nil
	case "windows":
		if strings.TrimSpace(env.LocalAppData) == "" {
			return "", fmt.Errorf("Windows LOCALAPPDATA 不能为空")
		}
		return filepath.Join(env.LocalAppData, "MeetSieve", "logs"), nil
	default:
		return "", fmt.Errorf("不支持的运行平台: %s", env.GOOS)
	}
}
