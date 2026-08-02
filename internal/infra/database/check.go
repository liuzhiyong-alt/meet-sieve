package database

import (
	"fmt"

	"meet-sieve/internal/infra/apperr"

	"gorm.io/gorm"
)

// CheckResult 是 SQLite 快速、完整性和外键检查的安全汇总。
type CheckResult struct {
	QuickCheck               string
	IntegrityCheck           string
	ForeignKeyViolationCount int
}

// CheckSQLite 执行启动期所需的 quick_check、integrity_check 与 foreign_key_check。
func CheckSQLite(db *gorm.DB) (CheckResult, error) {
	if db == nil {
		return CheckResult{}, apperr.Biz(apperr.CodeDatabaseIntegrityFailed, apperr.WithOp("database.check.validate"))
	}
	result := CheckResult{}
	if err := db.Raw("PRAGMA quick_check").Scan(&result.QuickCheck).Error; err != nil {
		return result, databaseIntegrityError("quick_check", err)
	}
	if result.QuickCheck != "ok" {
		return result, databaseIntegrityError("quick_check", nil)
	}
	if err := db.Raw("PRAGMA integrity_check").Scan(&result.IntegrityCheck).Error; err != nil {
		return result, databaseIntegrityError("integrity_check", err)
	}
	if result.IntegrityCheck != "ok" {
		return result, databaseIntegrityError("integrity_check", nil)
	}
	var violations []struct {
		Table string
	}
	if err := db.Raw("PRAGMA foreign_key_check").Scan(&violations).Error; err != nil {
		return result, databaseIntegrityError("foreign_key_check", err)
	}
	result.ForeignKeyViolationCount = len(violations)
	if result.ForeignKeyViolationCount != 0 {
		return result, databaseIntegrityError("foreign_key_check", nil)
	}
	return result, nil
}

// databaseIntegrityError 将 SQLite 原始检查异常收敛为不泄漏 SQL 的稳定错误码。
func databaseIntegrityError(check string, cause error) error {
	options := []apperr.Option{apperr.WithOp("database.check." + check)}
	if cause != nil {
		options = append(options, apperr.WithField("cause", fmt.Sprintf("%T", cause)))
	}
	return apperr.Biz(apperr.CodeDatabaseIntegrityFailed, options...)
}
