package singleinstance_test

import (
	"sync/atomic"
	"testing"

	"meet-sieve/internal/infra/singleinstance"
)

// TestActivationGate_DeliversActivationBeforeAndAfterHandler 验证窗口就绪前后的 activate 请求都不会丢失。
func TestActivationGate_DeliversActivationBeforeAndAfterHandler(t *testing.T) {
	gate := singleinstance.NewActivationGate()
	gate.Notify()

	var activations atomic.Int32
	gate.SetHandler(func() {
		activations.Add(1)
	})
	if got := activations.Load(); got != 1 {
		t.Fatalf("窗口就绪后应补投递一次 activate，实际为 %d", got)
	}

	gate.Notify()
	if got := activations.Load(); got != 2 {
		t.Fatalf("窗口就绪后应立即投递 activate，实际为 %d", got)
	}
}
