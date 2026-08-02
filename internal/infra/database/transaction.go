package database

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// TransactionManager 统一管理 GORM 事务边界。
type TransactionManager struct {
	db         *gorm.DB
	dispatcher *WriteDispatcher
}

// NewTransactionManager 创建事务管理器。
func NewTransactionManager(db *gorm.DB) *TransactionManager {
	return &TransactionManager{db: db}
}

// NewDispatchedTransactionManager 创建只经单 writer 提交业务事务的事务管理器。
func NewDispatchedTransactionManager(dispatcher *WriteDispatcher) *TransactionManager {
	return &TransactionManager{dispatcher: dispatcher}
}

// WithinTransaction 在事务内执行回调；回调失败或 context 取消时自动回滚。
func (m *TransactionManager) WithinTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if m == nil {
		return fmt.Errorf("事务管理器不能为空")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("事务开始前 context 已取消: %w", err)
	}
	if fn == nil {
		return fmt.Errorf("事务回调不能为空")
	}
	if m.dispatcher != nil {
		if err := m.dispatcher.Submit(ctx, fn); err != nil {
			return fmt.Errorf("单 writer 事务失败：%w", err)
		}
		return nil
	}
	if m.db == nil {
		return fmt.Errorf("事务数据库不能为空")
	}

	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if callbackErr := fn(tx); callbackErr != nil {
			return fmt.Errorf("事务回调失败: %w", callbackErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("执行事务失败: %w", err)
	}
	return nil
}
