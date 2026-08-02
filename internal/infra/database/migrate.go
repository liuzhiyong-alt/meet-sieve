package database

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"meet-sieve/migrations"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/mattn/go-sqlite3"
)

// Migrate 将内嵌 SQLite migration 向上执行到最新版本。
func Migrate(path string) error {
	return MigrateFS(path, migrations.SQLiteFiles, "sqlite")
}

// MigrateFS 使用指定文件系统执行 migration，供内嵌生产资源和失败路径测试复用。
func MigrateFS(path string, migrationFiles fs.FS, directory string) error {
	source, err := iofs.New(migrationFiles, directory)
	if err != nil {
		return fmt.Errorf("加载 migration 文件失败: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return fmt.Errorf("打开 migration SQLite 连接失败: %w", err)
	}
	defer db.Close()

	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{})
	if err != nil {
		return fmt.Errorf("创建 migration SQLite driver 失败: %w", err)
	}

	runner, err := migrate.NewWithInstance("iofs", source, "sqlite3", driver)
	if err != nil {
		return fmt.Errorf("创建 migration runner 失败: %w", err)
	}
	defer runner.Close()

	if err := runner.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("执行 migration 失败: %w", err)
	}
	return nil
}
