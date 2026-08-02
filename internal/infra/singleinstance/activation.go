package singleinstance

import "sync"

// ActivationGate 在 Wails 主窗口尚未就绪时暂存 activate 请求，并在就绪后投递。
type ActivationGate struct {
	mu      sync.Mutex
	handler func()
	pending bool
}

// NewActivationGate 创建用于单实例激活信号的并发安全闸门。
func NewActivationGate() *ActivationGate {
	return &ActivationGate{}
}

// Notify 记录一次 activate 请求；窗口已就绪时立即调用处理函数。
func (gate *ActivationGate) Notify() {
	handler := gate.loadHandlerOrMarkPending()
	if handler != nil {
		handler()
	}
}

// SetHandler 在 Wails 主窗口就绪后设置 activate 处理函数，并补投递早到的请求。
func (gate *ActivationGate) SetHandler(handler func()) {
	shouldNotify := gate.installHandler(handler)
	if shouldNotify {
		handler()
	}
}

// loadHandlerOrMarkPending 返回就绪处理函数，或将早到请求标记为待投递。
func (gate *ActivationGate) loadHandlerOrMarkPending() func() {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.handler == nil {
		gate.pending = true
		return nil
	}
	return gate.handler
}

// installHandler 安装处理函数并返回是否需要补投递先前的请求。
func (gate *ActivationGate) installHandler(handler func()) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()

	gate.handler = handler
	shouldNotify := gate.pending && handler != nil
	gate.pending = false
	return shouldNotify
}
