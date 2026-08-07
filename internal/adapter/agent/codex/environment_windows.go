//go:build windows

package codex

import (
	"context"

	"golang.org/x/sys/windows/registry"
)

// resolveLaunchEnvironment 合并 Explorer 进程环境与注册表中的最新用户、系统 PATH。
func resolveLaunchEnvironment(context.Context) []string {
	environment := currentEnvironment()
	currentPath := environmentValue(environment, "PATH", true)
	userPath := readRegistryPath(registry.CURRENT_USER, `Environment`)
	systemPath := readRegistryPath(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)
	return mergeEnvironmentPath(environment, true, userPath, currentPath, systemPath)
}

// readRegistryPath 读取并展开 Windows 环境注册表中的 PATH；失败时安全回退。
func readRegistryPath(root registry.Key, path string) string {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer func() { _ = key.Close() }()
	value, _, err := key.GetStringValue("Path")
	if err != nil || value == "" {
		return ""
	}
	expanded, err := registry.ExpandString(value)
	if err != nil {
		return value
	}
	return expanded
}
