package database

import (
	"fmt"
	"net/url"
	"path/filepath"

	"meet-sieve/internal/infra/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const defaultBusyTimeoutMS = 5000

// Runtime 持有业务运行期专用的单 writer 与小型 reader 连接池。
type Runtime struct {
	writer *gorm.DB
	reader *gorm.DB
}

// Open 保留启动期和既有调用的单连接 SQLite 打开方式。
func Open(path string) (*gorm.DB, error) {
	return openDatabase(path, defaultDatabaseConfig(), 1, 1)
}

// OpenRuntime 使用已校验的技术配置创建独立的 writer/read 数据库句柄。
func OpenRuntime(path string, cfg config.DatabaseConfig) (*Runtime, error) {
	if !isValidRuntimeConfig(cfg) {
		return nil, fmt.Errorf("SQLite runtime 配置不合法")
	}
	writer, err := openDatabase(path, cfg, 1, 1)
	if err != nil {
		return nil, err
	}
	reader, err := openDatabase(path, cfg, cfg.ReadMaxOpenConns, cfg.ReadMaxIdleConns)
	if err != nil {
		_ = Close(writer)
		return nil, err
	}
	return &Runtime{writer: writer, reader: reader}, nil
}

// Writer 返回只供 WriteDispatcher 使用的单连接 GORM 句柄。
func (runtime *Runtime) Writer() *gorm.DB {
	if runtime == nil {
		return nil
	}
	return runtime.writer
}

// Reader 返回只供查询使用的小型连接池句柄。
func (runtime *Runtime) Reader() *gorm.DB {
	if runtime == nil {
		return nil
	}
	return runtime.reader
}

// Close 关闭 writer/read 连接池；重复关闭由 database/sql 保持幂等。
func (runtime *Runtime) Close() error {
	if runtime == nil {
		return nil
	}
	writerErr := Close(runtime.writer)
	readerErr := Close(runtime.reader)
	if writerErr != nil {
		return writerErr
	}
	return readerErr
}

// defaultDatabaseConfig 为兼容 Open 提供 Step 1 固定配置，而运行期应使用 OpenRuntime。
func defaultDatabaseConfig() config.DatabaseConfig {
	return config.DatabaseConfig{
		BusyTimeoutMS:      defaultBusyTimeoutMS,
		ReadMaxOpenConns:   1,
		ReadMaxIdleConns:   1,
		WriteQueueCapacity: config.Step1WriteQueueCapacity,
	}
}

// isValidRuntimeConfig 拒绝非法配置，避免数据库层回退到与技术配置不一致的硬编码值。
func isValidRuntimeConfig(cfg config.DatabaseConfig) bool {
	return cfg.BusyTimeoutMS > 0 &&
		cfg.WriteQueueCapacity == config.Step1WriteQueueCapacity &&
		cfg.ReadMaxOpenConns >= 1 && cfg.ReadMaxOpenConns <= config.MaxReadOpenConns &&
		cfg.ReadMaxIdleConns >= 1 && cfg.ReadMaxIdleConns <= cfg.ReadMaxOpenConns
}

// openDatabase 创建一个 SQLite 句柄；DSN 与 PRAGMA 同时设置以覆盖连接池中新建的连接。
func openDatabase(path string, cfg config.DatabaseConfig, maxOpenConns int, maxIdleConns int) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(buildSQLiteDSN(path, cfg)), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败：%w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取 SQLite 连接池失败：%w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	if err := applyPragmas(db, cfg.BusyTimeoutMS); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// buildSQLiteDSN 把逐连接 PRAGMA 写入 mattn/go-sqlite3 DSN，避免连接池新连接漏配外键或超时。
func buildSQLiteDSN(path string, cfg config.DatabaseConfig) string {
	query := url.Values{}
	query.Set("_foreign_keys", "on")
	query.Set("_busy_timeout", fmt.Sprintf("%d", cfg.BusyTimeoutMS))
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "NORMAL")
	return (&url.URL{Scheme: "file", Path: filepath.Clean(path), RawQuery: query.Encode()}).String()
}

// applyPragmas 立即验证首个连接的外键、WAL、同步级别和锁等待时间。
func applyPragmas(db *gorm.DB, busyTimeoutMS int) error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMS),
	} {
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("应用 SQLite PRAGMA 失败：%w", err)
		}
	}
	return nil
}

// Close 关闭单个 GORM SQLite 连接池。
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取 SQLite 连接池失败：%w", err)
	}
	return sqlDB.Close()
}
