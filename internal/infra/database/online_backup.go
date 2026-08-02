package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"

	"github.com/mattn/go-sqlite3"
)

// OnlineBackup 使用 SQLite backup API 从源库创建一致快照；目标路径不得预先存在。
func OnlineBackup(sourcePath string, destinationPath string) error {
	if _, err := os.Lstat(destinationPath); err == nil {
		return fmt.Errorf("SQLite 备份目标已存在")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查 SQLite 备份目标失败: %w", err)
	}
	source, err := openBackupSource(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := sql.Open("sqlite3", destinationPath)
	if err != nil {
		return fmt.Errorf("打开 SQLite 备份目标失败: %w", err)
	}
	defer destination.Close()
	return copySQLiteBackup(source, destination)
}

// openBackupSource 强制以只读模式读取源库，避免备份操作本身创建 WAL 或修改源文件。
func openBackupSource(path string) (*sql.DB, error) {
	query := url.Values{}
	query.Set("mode", "ro")
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 备份源失败: %w", err)
	}
	return db, nil
}

// copySQLiteBackup 保持源、目标底层连接在整个 backup 生命周期中不被连接池归还。
func copySQLiteBackup(source *sql.DB, destination *sql.DB) error {
	context := context.Background()
	sourceConn, err := source.Conn(context)
	if err != nil {
		return fmt.Errorf("获取 SQLite 备份源连接失败: %w", err)
	}
	defer sourceConn.Close()
	destinationConn, err := destination.Conn(context)
	if err != nil {
		return fmt.Errorf("获取 SQLite 备份目标连接失败: %w", err)
	}
	defer destinationConn.Close()

	sourceDriverConn, err := unwrapSQLiteConnection(sourceConn)
	if err != nil {
		return err
	}
	destinationDriverConn, err := unwrapSQLiteConnection(destinationConn)
	if err != nil {
		return err
	}
	backup, err := destinationDriverConn.Backup("main", sourceDriverConn, "main")
	if err != nil {
		return fmt.Errorf("初始化 SQLite Online Backup 失败: %w", err)
	}
	defer backup.Close()
	for {
		done, err := backup.Step(128)
		if err != nil {
			return fmt.Errorf("执行 SQLite Online Backup 失败: %w", err)
		}
		if done {
			return nil
		}
	}
}

// unwrapSQLiteConnection 取得 mattn/go-sqlite3 的原生连接以调用 backup API。
func unwrapSQLiteConnection(connection *sql.Conn) (*sqlite3.SQLiteConn, error) {
	var result *sqlite3.SQLiteConn
	err := connection.Raw(func(driverConnection any) error {
		var ok bool
		result, ok = driverConnection.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("SQLite driver 不支持 Online Backup")
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("获取 SQLite 原生连接失败: %w", err)
	}
	return result, nil
}
