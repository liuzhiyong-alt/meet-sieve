package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	notificationQueueSize  = 256
	serverRequestQueueSize = 8
)

type rpcResult struct {
	message Message
	err     error
}

// RPCClient 路由长生命周期 JSON-RPC 的响应、通知和 ServerRequest。
type RPCClient struct {
	codec          *Codec
	nextID         atomic.Int64
	started        atomic.Bool
	startOnce      sync.Once
	shutdownOnce   sync.Once
	pendingMu      sync.Mutex
	pending        map[string]chan rpcResult
	notifications  chan Message
	serverRequests chan Message
	done           chan struct{}
	terminalMu     sync.Mutex
	terminalErr    error
}

// NewRPCClient 创建未启动的路由器；调用 Start 后才会读取消息。
func NewRPCClient(codec *Codec) *RPCClient {
	return &RPCClient{
		codec:          codec,
		pending:        make(map[string]chan rpcResult),
		notifications:  make(chan Message, notificationQueueSize),
		serverRequests: make(chan Message, serverRequestQueueSize),
		done:           make(chan struct{}),
	}
}

// Start 启动唯一 reader loop，并将其生命周期绑定到 context。
func (client *RPCClient) Start(ctx context.Context) {
	client.startOnce.Do(func() {
		client.started.Store(true)
		go client.readLoop()
		go func() {
			<-ctx.Done()
			client.shutdown(ctx.Err())
		}()
	})
}

// Call 发送一次 RPC 并等待与 request ID 匹配的响应。
func (client *RPCClient) Call(ctx context.Context, method string, params any, output any) error {
	if !client.started.Load() {
		return fmt.Errorf("Codex RPCClient 尚未启动")
	}
	id := IntRequestID(client.nextID.Add(1))
	resultChannel := make(chan rpcResult, 1)
	if err := client.addPending(id, resultChannel); err != nil {
		return err
	}
	if err := client.codec.Write(Request{ID: &id, Method: method, Params: params}); err != nil {
		client.removePending(id)
		return err
	}

	select {
	case result := <-resultChannel:
		if result.err != nil {
			return result.err
		}
		if result.message.Error != nil {
			return fmt.Errorf("Codex RPC %s 返回错误 code=%d", method, result.message.Error.Code)
		}
		if output == nil {
			return nil
		}
		if err := json.Unmarshal(result.message.Result, output); err != nil {
			return fmt.Errorf("解析 Codex RPC %s 结果失败: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		client.removePending(id)
		return ctx.Err()
	case <-client.done:
		client.removePending(id)
		return client.err()
	}
}

// Notify 发送不带 request ID 的客户端通知。
func (client *RPCClient) Notify(method string, params any) error {
	if !client.started.Load() || method == "" {
		return fmt.Errorf("Codex notification 参数无效")
	}
	select {
	case <-client.done:
		return client.err()
	default:
	}
	return client.codec.Write(Request{Method: method, Params: params})
}

// Notifications 返回有界通知通道，消费者不得关闭。
func (client *RPCClient) Notifications() <-chan Message {
	return client.notifications
}

// ServerRequests 返回不可丢弃的原生请求通道，消费者不得关闭。
func (client *RPCClient) ServerRequests() <-chan Message {
	return client.serverRequests
}

// Respond 向 app-server 回写一次 ServerRequest 结果。
func (client *RPCClient) Respond(id RequestID, result any, protocolError *ProtocolError) error {
	if !client.started.Load() || id.Key() == "" {
		return fmt.Errorf("Codex ServerRequest 响应参数无效")
	}
	select {
	case <-client.done:
		return client.err()
	default:
	}
	return client.codec.WriteResponse(Response{ID: id, Result: result, Error: protocolError})
}

// Done 在 reader、context 或显式关闭导致路由器终止时关闭。
func (client *RPCClient) Done() <-chan struct{} {
	return client.done
}

// Close 终止路由并一次性失败所有 pending RPC。
func (client *RPCClient) Close(cause error) {
	client.shutdown(cause)
}

// readLoop 是唯一 stdout reader，只做协议分类和内存路由。
func (client *RPCClient) readLoop() {
	for {
		message, err := client.codec.Read()
		if err != nil {
			client.shutdown(err)
			return
		}
		if err := client.dispatch(message); err != nil {
			client.shutdown(err)
			return
		}
	}
}

// dispatch 区分 response、notification 和 ServerRequest。
func (client *RPCClient) dispatch(message Message) error {
	if message.ID != nil && message.Method != "" {
		select {
		case client.serverRequests <- message:
			return nil
		default:
			return fmt.Errorf("Codex ServerRequest 队列已满")
		}
	}
	if message.ID != nil {
		client.pendingMu.Lock()
		channel, exists := client.pending[message.ID.Key()]
		if exists {
			delete(client.pending, message.ID.Key())
		}
		client.pendingMu.Unlock()
		if !exists {
			return fmt.Errorf("Codex 返回未知 response ID")
		}
		channel <- rpcResult{message: message}
		return nil
	}
	if message.Method == "" {
		return fmt.Errorf("Codex 消息缺少 ID 和 method")
	}
	select {
	case client.notifications <- message:
		return nil
	default:
		return fmt.Errorf("Codex notification 队列已满")
	}
}

// addPending 在写请求前登记一次性响应通道。
func (client *RPCClient) addPending(id RequestID, channel chan rpcResult) error {
	client.pendingMu.Lock()
	defer client.pendingMu.Unlock()
	select {
	case <-client.done:
		return client.err()
	default:
	}
	client.pending[id.Key()] = channel
	return nil
}

// removePending 清理取消或写失败的请求。
func (client *RPCClient) removePending(id RequestID) {
	client.pendingMu.Lock()
	delete(client.pending, id.Key())
	client.pendingMu.Unlock()
}

// shutdown 只执行一次，并保证所有等待者收到同一个终止原因。
func (client *RPCClient) shutdown(cause error) {
	client.shutdownOnce.Do(func() {
		if cause == nil {
			cause = fmt.Errorf("Codex RPCClient 已关闭")
		}
		client.terminalMu.Lock()
		client.terminalErr = cause
		client.terminalMu.Unlock()
		close(client.done)

		client.pendingMu.Lock()
		for key, channel := range client.pending {
			channel <- rpcResult{err: cause}
			delete(client.pending, key)
		}
		client.pendingMu.Unlock()
	})
}

// err 返回稳定的终止原因。
func (client *RPCClient) err() error {
	client.terminalMu.Lock()
	defer client.terminalMu.Unlock()
	if client.terminalErr == nil {
		return fmt.Errorf("Codex RPCClient 已关闭")
	}
	return client.terminalErr
}
