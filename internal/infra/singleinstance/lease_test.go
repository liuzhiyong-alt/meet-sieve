package singleinstance

import (
	"errors"
	"sync/atomic"
	"testing"
)

// TestLease_CloseRunsReleaseOnlyOnce 验证所有权释放在重复调用时只执行一次并保留首次错误。
func TestLease_CloseRunsReleaseOnlyOnce(t *testing.T) {
	wantErr := errors.New("释放失败")
	var calls atomic.Int32
	lease := newLease(func() error {
		calls.Add(1)
		return wantErr
	})

	if err := lease.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("首次 Close 错误不正确：%v", err)
	}
	if err := lease.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("重复 Close 应返回首次错误：%v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("释放函数调用次数不正确：%d", got)
	}
}
