//go:build windows

package singleinstance

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsInstanceSecurityDescriptor = "D:P(A;;GA;;;AU)(A;;GA;;;BA)"
	activationCommand                 = "activate"
	activationAcknowledgement         = "ok"
	activationPipeBufferSize          = 64
)

// Acquire 使用机器级 mutex 建立 Windows 单实例，并为当前登录会话启动 activate 管道。
func Acquire() (Outcome, *Lease, error) {
	securityAttributes, err := newWindowsSecurityAttributes()
	if err != nil {
		return "", nil, err
	}
	mutex, err := createWindowsMutex(securityAttributes)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(mutex)
		return activateExistingWindowsInstance(), nil, nil
	}
	if err != nil {
		return "", nil, err
	}

	pipeServer, err := startWindowsActivationPipe(securityAttributes)
	if err != nil {
		_ = windows.CloseHandle(mutex)
		return "", nil, err
	}
	return OutcomeAcquired, newLeaseWithActivationGate(func() error {
		return closeWindowsInstance(mutex, pipeServer)
	}, pipeServer.activationGate), nil
}

// newWindowsSecurityAttributes 创建仅向本机已认证用户和内置管理员开放的 DACL。
func newWindowsSecurityAttributes() (*windows.SecurityAttributes, error) {
	securityDescriptor, err := windows.SecurityDescriptorFromString(windowsInstanceSecurityDescriptor)
	if err != nil {
		return nil, fmt.Errorf("创建 Windows 单实例安全描述符失败: %w", err)
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: securityDescriptor,
	}, nil
}

// createWindowsMutex 创建共享 mutex，并保留 ERROR_ALREADY_EXISTS 供调用方识别既有实例。
func createWindowsMutex(attributes *windows.SecurityAttributes) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(WindowsMutexName)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("编码 Windows mutex 名称失败: %w", err)
	}
	mutex, err := windows.CreateMutex(attributes, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return windows.InvalidHandle, fmt.Errorf("创建 Windows 单实例 mutex 失败: %w", err)
	}
	return mutex, err
}

// activateExistingWindowsInstance 向当前会话的首实例发送 activate；跨会话无管道时仍稳定退出。
func activateExistingWindowsInstance() Outcome {
	pipeName, err := currentWindowsActivationPipeName()
	if err == nil {
		_ = sendWindowsActivation(pipeName)
	}
	return OutcomeAlreadyRunning
}

// currentWindowsActivationPipeName 返回当前进程所属 Windows 登录会话的管道名称。
func currentWindowsActivationPipeName() (string, error) {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sessionID); err != nil {
		return "", fmt.Errorf("读取 Windows 登录会话失败: %w", err)
	}
	return ActivationPipeName(sessionID), nil
}

// closeWindowsInstance 先关闭激活管道，再释放 mutex，保证后续实例不会连入已关闭的首实例。
func closeWindowsInstance(mutex windows.Handle, pipeServer *windowsActivationPipeServer) error {
	pipeErr := pipeServer.Close()
	mutexErr := windows.CloseHandle(mutex)
	return errors.Join(pipeErr, mutexErr)
}

// windowsActivationPipeServer 在当前登录会话接收第二实例的 activate 请求。
type windowsActivationPipeServer struct {
	name           string
	security       *windows.SecurityAttributes
	activationGate *ActivationGate

	mu      sync.Mutex
	stopped bool
	pipe    windows.Handle
	done    chan struct{}
}

// startWindowsActivationPipe 在返回前创建首个管道，避免首实例与二次启动之间的注册竞态。
func startWindowsActivationPipe(attributes *windows.SecurityAttributes) (*windowsActivationPipeServer, error) {
	name, err := currentWindowsActivationPipeName()
	if err != nil {
		return nil, err
	}
	server := &windowsActivationPipeServer{
		name:           name,
		security:       attributes,
		activationGate: NewActivationGate(),
		pipe:           windows.InvalidHandle,
		done:           make(chan struct{}),
	}
	pipe, err := server.createPipe()
	if err != nil {
		return nil, err
	}
	if !server.trackPipe(pipe) {
		_ = windows.CloseHandle(pipe)
		return nil, errors.New("Windows 激活管道在启动时被关闭")
	}
	go server.serve(pipe)
	return server, nil
}

// createPipe 使用和 mutex 相同的 DACL 创建只接受本机客户端的消息管道。
func (server *windowsActivationPipeServer) createPipe() (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(server.name)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("编码 Windows 激活管道名称失败: %w", err)
	}
	pipe, err := windows.CreateNamedPipe(
		name,
		windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		1,
		activationPipeBufferSize,
		activationPipeBufferSize,
		0,
		server.security,
	)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("创建 Windows 激活管道失败: %w", err)
	}
	return pipe, nil
}

// serve 逐个处理连接；关闭当前管道可中断阻塞的 ConnectNamedPipe。
func (server *windowsActivationPipeServer) serve(pipe windows.Handle) {
	defer close(server.done)
	for {
		server.handleConnection(pipe)
		if server.isStopped() {
			return
		}
		nextPipe, err := server.createPipe()
		if err != nil {
			return
		}
		if !server.trackPipe(nextPipe) {
			_ = windows.CloseHandle(nextPipe)
			return
		}
		pipe = nextPipe
	}
}

// handleConnection 读取单个 activate 命令，处理完成后才向第二实例确认。
func (server *windowsActivationPipeServer) handleConnection(pipe windows.Handle) {
	defer server.closeTrackedPipe(pipe)
	if err := windows.ConnectNamedPipe(pipe, nil); err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return
	}
	buffer := make([]byte, len(activationCommand))
	read, err := windows.Read(pipe, buffer)
	if err != nil || string(buffer[:read]) != activationCommand {
		return
	}
	server.activationGate.Notify()
	_, _ = windows.Write(pipe, []byte(activationAcknowledgement))
}

// trackPipe 将可阻塞的当前管道登记给 Close，用于取消 ConnectNamedPipe 或 Read。
func (server *windowsActivationPipeServer) trackPipe(pipe windows.Handle) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.stopped {
		return false
	}
	server.pipe = pipe
	return true
}

// closeTrackedPipe 只关闭当前仍归本协程所有的管道，避免和 Close 重复关闭句柄。
func (server *windowsActivationPipeServer) closeTrackedPipe(pipe windows.Handle) {
	server.mu.Lock()
	if server.pipe != pipe {
		server.mu.Unlock()
		return
	}
	server.pipe = windows.InvalidHandle
	server.mu.Unlock()
	_ = windows.DisconnectNamedPipe(pipe)
	_ = windows.CloseHandle(pipe)
}

// isStopped 返回服务是否进入关闭状态。
func (server *windowsActivationPipeServer) isStopped() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.stopped
}

// Close 取消阻塞的管道 I/O 并等待服务 goroutine 退出。
func (server *windowsActivationPipeServer) Close() error {
	server.mu.Lock()
	if server.stopped {
		server.mu.Unlock()
		return nil
	}
	server.stopped = true
	pipe := server.pipe
	server.pipe = windows.InvalidHandle
	server.mu.Unlock()
	if pipe != windows.InvalidHandle {
		_ = windows.DisconnectNamedPipe(pipe)
		_ = windows.CloseHandle(pipe)
	}
	<-server.done
	return nil
}

// sendWindowsActivation 将 activate 写入当前会话管道，并等待首实例处理完成。
func sendWindowsActivation(pipeName string) error {
	name, err := windows.UTF16PtrFromString(pipeName)
	if err != nil {
		return fmt.Errorf("编码 Windows 激活管道名称失败: %w", err)
	}
	pipe, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return fmt.Errorf("连接 Windows 激活管道失败: %w", err)
	}
	defer func() {
		_ = windows.CloseHandle(pipe)
	}()
	if _, err := windows.Write(pipe, []byte(activationCommand)); err != nil {
		return fmt.Errorf("发送 Windows activate 失败: %w", err)
	}
	acknowledgement := make([]byte, len(activationAcknowledgement))
	read, err := windows.Read(pipe, acknowledgement)
	if err != nil || string(acknowledgement[:read]) != activationAcknowledgement {
		return fmt.Errorf("确认 Windows activate 失败: %w", err)
	}
	return nil
}
