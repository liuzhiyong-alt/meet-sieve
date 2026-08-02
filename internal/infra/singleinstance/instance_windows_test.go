//go:build windows

package singleinstance

import (
	"testing"
	"time"
)

// TestAcquire_WindowsActivatesExistingInstanceAndReleasesMutex 验证真实 mutex、命名管道和关闭后的重新取得。
func TestAcquire_WindowsActivatesExistingInstanceAndReleasesMutex(t *testing.T) {
	firstOutcome, firstLease, err := Acquire()
	if err != nil {
		t.Fatalf("首次取得 Windows 单实例所有权失败：%v", err)
	}
	if firstOutcome != OutcomeAcquired || firstLease == nil {
		t.Fatalf("首次取得结果不正确：outcome=%q lease=%v", firstOutcome, firstLease)
	}
	t.Cleanup(func() {
		if firstLease != nil {
			_ = firstLease.Close()
		}
	})

	activated := make(chan struct{}, 1)
	firstLease.SetActivationHandler(func() {
		activated <- struct{}{}
	})

	secondOutcome, secondLease, err := Acquire()
	if err != nil {
		t.Fatalf("二次取得 Windows 单实例所有权失败：%v", err)
	}
	if secondOutcome != OutcomeAlreadyRunning || secondLease != nil {
		t.Fatalf("二次取得不应成为所有者：outcome=%q lease=%v", secondOutcome, secondLease)
	}
	select {
	case <-activated:
	case <-time.After(time.Second):
		t.Fatal("二次启动未向首实例发送 activate")
	}

	if err := firstLease.Close(); err != nil {
		t.Fatalf("释放 Windows 单实例所有权失败：%v", err)
	}
	firstLease = nil

	thirdOutcome, thirdLease, err := Acquire()
	if err != nil {
		t.Fatalf("释放后重新取得 Windows 单实例所有权失败：%v", err)
	}
	if thirdOutcome != OutcomeAcquired || thirdLease == nil {
		t.Fatalf("释放后取得结果不正确：outcome=%q lease=%v", thirdOutcome, thirdLease)
	}
	if err := thirdLease.Close(); err != nil {
		t.Fatalf("释放重新取得的 Windows 所有权失败：%v", err)
	}
}
