// Command verifybuildmeta 校验跨平台构建使用的 semantic version 与 Windows 数字版本。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
)

var (
	semanticVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*)?$`)
	windowsVersionPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$`)
)

type wailsConfig struct {
	Info struct {
		ProductVersion string `json:"productVersion"`
	} `json:"info"`
}

// main 解析构建参数并通过退出码报告版本配置是否一致。
func main() {
	version := flag.String("version", "", "应用 semantic version")
	windowsFileVersion := flag.String("windows-file-version", "", "Windows 四段数字版本")
	wailsConfigPath := flag.String("wails-config", "cmd/meetsieve/wails.json", "Wails 配置路径")
	flag.Parse()

	if err := verifyBuildMetadata(*version, *windowsFileVersion, *wailsConfigPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// verifyBuildMetadata 校验版本格式，并确认 Wails PE 资源使用相同的 Windows 数字版本。
func verifyBuildMetadata(version string, windowsFileVersion string, configPath string) error {
	if !semanticVersionPattern.MatchString(version) {
		return fmt.Errorf("BUILD_VERSION 不是受支持的 semantic version: %s", version)
	}
	if !windowsVersionPattern.MatchString(windowsFileVersion) {
		return fmt.Errorf("WINDOWS_FILE_VERSION 必须是四段数字版本: %s", windowsFileVersion)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取 Wails 配置失败: %w", err)
	}
	var config wailsConfig
	if err := json.Unmarshal(content, &config); err != nil {
		return fmt.Errorf("解析 Wails 配置失败: %w", err)
	}
	if config.Info.ProductVersion != windowsFileVersion {
		return fmt.Errorf("Wails productVersion=%s 与 WINDOWS_FILE_VERSION=%s 不一致", config.Info.ProductVersion, windowsFileVersion)
	}
	return nil
}
