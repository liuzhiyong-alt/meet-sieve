package singleinstance

import "sync"

// Outcome 表示一次平台单实例检查的稳定结果。
type Outcome string

const (
	// OutcomeAcquired 表示当前进程取得应用实例所有权。
	OutcomeAcquired Outcome = "acquired"
	// OutcomeAlreadyRunning 表示已有进程持有应用实例所有权。
	OutcomeAlreadyRunning Outcome = "already_running"
)

// Lease 表示当前进程持有的单实例资源；Close 可重复安全调用。
type Lease struct {
	closeFn        func() error
	activationGate *ActivationGate
	closeOnce      sync.Once
	closeErr       error
}

// newLease 创建由当前平台实现释放的单实例资源。
func newLease(closeFn func() error) *Lease {
	return &Lease{closeFn: closeFn}
}

// newLeaseWithActivationGate 创建能接收第二实例 activate 请求的单实例资源。
func newLeaseWithActivationGate(closeFn func() error, activationGate *ActivationGate) *Lease {
	return &Lease{closeFn: closeFn, activationGate: activationGate}
}

// SetActivationHandler 设置当前首实例的窗口激活处理函数。
func (lease *Lease) SetActivationHandler(handler func()) {
	if lease == nil || lease.activationGate == nil {
		return
	}
	lease.activationGate.SetHandler(handler)
}

// Close 释放单实例资源；后续调用返回首次释放结果。
func (lease *Lease) Close() error {
	if lease == nil {
		return nil
	}
	lease.closeOnce.Do(func() {
		if lease.closeFn != nil {
			lease.closeErr = lease.closeFn()
		}
	})
	return lease.closeErr
}
