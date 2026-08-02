package database

import (
	"context"
	"errors"
	"strings"

	"meet-sieve/internal/infra/apperr"
)

// normalizeWriteError 将 SQLite 可恢复的锁竞争收敛为稳定的 DATABASE_BUSY。
func normalizeWriteError(err error) error {
	if err == nil {
		return nil
	}
	if isSQLiteBusy(err) {
		return apperr.Dependency(apperr.CodeDatabaseBusy, err, apperr.WithOp("database.writer.transaction"))
	}
	return err
}

// isSQLiteBusy 按 SQLite driver 的稳定错误文本识别锁竞争与忙等待超时。
func isSQLiteBusy(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") || strings.Contains(message, "database is busy")
}
