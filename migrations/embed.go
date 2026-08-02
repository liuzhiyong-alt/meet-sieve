// Package migrations 提供编译进二进制的版本化 SQLite SQL。
package migrations

import "embed"

// SQLiteFiles 包含 SQLite migration 文件。
//
//go:embed sqlite/*.sql
var SQLiteFiles embed.FS
