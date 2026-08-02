package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

const (
	// LocatorSchemaVersion 是当前应用支持的 locator JSON 格式版本。
	LocatorSchemaVersion = 1
)

var (
	// ErrLocatorSchemaNewer 表示 locator 由更新版本的应用写入，当前版本不得覆盖。
	ErrLocatorSchemaNewer = errors.New("locator schema 版本高于当前应用")
)

// Locator 是系统应用目录中保存的最小工作目录定位配置。
type Locator struct {
	SchemaVersion int    `json:"schema_version"`
	WorkspacePath string `json:"workspace_path"`
}

// ParseLocator 严格解析 locator；不接受未知、重复或弱类型 JSON 字段。
func ParseLocator(data []byte) (Locator, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := requireLocatorObject(decoder); err != nil {
		return Locator{}, err
	}

	locator, err := decodeLocatorFields(decoder)
	if err != nil {
		return Locator{}, err
	}
	if err := requireNoTrailingLocatorContent(decoder); err != nil {
		return Locator{}, err
	}
	if err := validateLocator(locator); err != nil {
		return Locator{}, err
	}
	return locator, nil
}

// requireLocatorObject 确保 JSON 根节点是对象而非数组、字符串或 null。
func requireLocatorObject(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("读取 locator 根节点失败: %w", err)
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return errors.New("locator 根节点必须是对象")
	}
	return nil
}

// decodeLocatorFields 解码两个允许字段，并在解码前拒绝重复键。
func decodeLocatorFields(decoder *json.Decoder) (Locator, error) {
	var locator Locator
	seen := make(map[string]bool, 2)
	for decoder.More() {
		key, err := decodeLocatorFieldName(decoder, seen)
		if err != nil {
			return Locator{}, err
		}
		switch key {
		case "schema_version":
			if err := decoder.Decode(&locator.SchemaVersion); err != nil {
				return Locator{}, fmt.Errorf("locator schema_version 类型不正确: %w", err)
			}
		case "workspace_path":
			if err := decoder.Decode(&locator.WorkspacePath); err != nil {
				return Locator{}, fmt.Errorf("locator workspace_path 类型不正确: %w", err)
			}
		default:
			return Locator{}, fmt.Errorf("locator 包含未知字段 %q", key)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return Locator{}, fmt.Errorf("读取 locator 结束节点失败: %w", err)
	}
	return locator, nil
}

// decodeLocatorFieldName 返回尚未出现的字段名。
func decodeLocatorFieldName(decoder *json.Decoder, seen map[string]bool) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", fmt.Errorf("读取 locator 字段失败: %w", err)
	}
	key, ok := token.(string)
	if !ok {
		return "", errors.New("locator 字段名必须是字符串")
	}
	if seen[key] {
		return "", fmt.Errorf("locator 字段 %q 重复", key)
	}
	seen[key] = true
	return key, nil
}

// requireNoTrailingLocatorContent 确保根对象之后没有第二个 JSON 值或其他字节。
func requireNoTrailingLocatorContent(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("locator 不允许尾随 JSON 内容")
		}
		return fmt.Errorf("locator 尾随内容不合法: %w", err)
	}
	return nil
}

// validateLocator 校验版本和路径；路径规范化留给工作目录路径策略统一处理。
func validateLocator(locator Locator) error {
	if locator.SchemaVersion == 0 {
		return errors.New("locator 缺少 schema_version")
	}
	if locator.SchemaVersion > LocatorSchemaVersion {
		return fmt.Errorf("%w: %d", ErrLocatorSchemaNewer, locator.SchemaVersion)
	}
	if locator.SchemaVersion != LocatorSchemaVersion {
		return fmt.Errorf("locator schema_version 不受支持: %d", locator.SchemaVersion)
	}
	if !filepath.IsAbs(locator.WorkspacePath) {
		return errors.New("locator workspace_path 必须是绝对路径")
	}
	return nil
}
