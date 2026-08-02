package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"meet-sieve/internal/infra/filesystem"
)

const locatorFilePermission = 0o600

// LocatorStore 在系统应用目录读写最小 locator 文件。
type LocatorStore struct {
	path        string
	atomicWrite func(string, []byte, os.FileMode) error
}

// NewLocatorStore 创建指定 config.json 路径的 locator 存储。
func NewLocatorStore(path string) *LocatorStore {
	return &LocatorStore{path: path, atomicWrite: filesystem.WriteAtomic}
}

// NewSystemLocatorStore 创建当前平台系统应用目录中的 locator 存储。
func NewSystemLocatorStore() (*LocatorStore, error) {
	directory, err := filesystem.CurrentAppConfigDir()
	if err != nil {
		return nil, err
	}
	return NewLocatorStore(filepath.Join(directory, "config.json")), nil
}

// Load 读取 locator；文件不存在返回 configured=false，且不创建目录或工作目录。
func (store *LocatorStore) Load() (Locator, bool, error) {
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return Locator{}, false, nil
	}
	if err != nil {
		return Locator{}, false, fmt.Errorf("读取 locator 失败: %w", err)
	}
	locator, err := ParseLocator(content)
	if err != nil {
		return Locator{}, false, err
	}
	return locator, true, nil
}

// Save 严格校验并以同目录临时文件原子替换 locator，任一写入步骤失败时保留旧文件。
func (store *LocatorStore) Save(locator Locator) error {
	if err := validateLocator(locator); err != nil {
		return err
	}
	content, err := json.Marshal(locator)
	if err != nil {
		return fmt.Errorf("编码 locator 失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("创建 locator 目录失败: %w", err)
	}
	if err := store.atomicWrite(store.path, content, locatorFilePermission); err != nil {
		return fmt.Errorf("保存 locator 失败: %w", err)
	}
	return nil
}
