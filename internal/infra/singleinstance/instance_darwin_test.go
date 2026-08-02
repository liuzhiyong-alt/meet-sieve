//go:build darwin

package singleinstance

import "testing"

// TestAcquire_MacOSAcquiresWhenMeetSieveIsNotRunning 验证没有既有应用实例时可取得启动权。
func TestAcquire_MacOSAcquiresWhenMeetSieveIsNotRunning(t *testing.T) {
	outcome, lease, err := Acquire()
	if err != nil {
		t.Fatalf("取得 macOS 单实例所有权失败：%v", err)
	}
	if outcome != OutcomeAcquired || lease == nil {
		t.Fatalf("首次取得结果不正确：outcome=%q lease=%v", outcome, lease)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("释放 macOS 单实例资源失败：%v", err)
	}
}
