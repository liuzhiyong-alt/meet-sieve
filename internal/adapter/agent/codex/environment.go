package codex

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mergeEnvironmentPath 替换环境中的 PATH，并保留其他变量和首个出现的目录顺序。
func mergeEnvironmentPath(environment []string, caseInsensitive bool, pathValues ...string) []string {
	directories := make([]string, 0, 16)
	seen := make(map[string]struct{})
	for _, value := range pathValues {
		for _, directory := range filepath.SplitList(value) {
			directory = strings.TrimSpace(directory)
			if directory == "" {
				continue
			}
			key := directory
			if caseInsensitive {
				key = strings.ToLower(key)
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			directories = append(directories, directory)
		}
	}
	return replaceEnvironmentValue(environment, "PATH", strings.Join(directories, string(os.PathListSeparator)), caseInsensitive)
}

// replaceEnvironmentValue 返回替换单个变量后的环境副本。
func replaceEnvironmentValue(environment []string, name string, value string, caseInsensitive bool) []string {
	result := make([]string, 0, len(environment)+1)
	replaced := false
	for _, item := range environment {
		key, _, found := strings.Cut(item, "=")
		matches := found && ((!caseInsensitive && key == name) || (caseInsensitive && strings.EqualFold(key, name)))
		if matches {
			if !replaced {
				result = append(result, name+"="+value)
				replaced = true
			}
			continue
		}
		result = append(result, item)
	}
	if !replaced {
		result = append(result, name+"="+value)
	}
	return result
}

// currentEnvironment 返回不与 os.Environ 共享底层数组的环境副本。
func currentEnvironment() []string {
	return append([]string(nil), os.Environ()...)
}

// applyLocalProxyEnvironment 移除继承代理，并按用户配置为 Codex 注入本机 HTTP(S) 代理。
func applyLocalProxyEnvironment(environment []string, proxyPort int) []string {
	result := removeEnvironmentValues(environment,
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
	)
	if proxyPort <= 0 {
		return result
	}
	proxyURL := "http://127.0.0.1:" + strconv.Itoa(proxyPort)
	noProxy := "localhost,127.0.0.1,::1"
	return append(result,
		"HTTP_PROXY="+proxyURL,
		"HTTPS_PROXY="+proxyURL,
		"http_proxy="+proxyURL,
		"https_proxy="+proxyURL,
		"NO_PROXY="+noProxy,
		"no_proxy="+noProxy,
	)
}

// removeEnvironmentValues 删除指定变量，避免 Finder、Explorer 或终端环境影响显式配置。
func removeEnvironmentValues(environment []string, names ...string) []string {
	removals := make(map[string]struct{}, len(names))
	for _, name := range names {
		removals[strings.ToLower(name)] = struct{}{}
	}
	result := make([]string, 0, len(environment))
	for _, item := range environment {
		key, _, found := strings.Cut(item, "=")
		if found {
			if _, remove := removals[strings.ToLower(key)]; remove {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}
