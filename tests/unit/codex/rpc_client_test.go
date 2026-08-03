package codex_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"meet-sieve/internal/adapter/agent/codex"
)

// TestRPCClient_RoutesOutOfOrderResponsesNotificationsAndServerRequests 验证三类消息交错时仍按 ID 路由。
func TestRPCClient_RoutesOutOfOrderResponsesNotificationsAndServerRequests(t *testing.T) {
	t.Parallel()

	reader := newSynchronizedBuffer()
	defer reader.Close()
	writer := newSynchronizedBuffer()
	client := codex.NewRPCClient(codex.NewCodec(reader, writer))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.Start(ctx)

	results := make(chan string, 2)
	for _, method := range []string{"first", "second"} {
		method := method
		go func() {
			var result struct {
				Value string `json:"value"`
			}
			if err := client.Call(ctx, method, nil, &result); err != nil {
				results <- "error:" + err.Error()
				return
			}
			results <- result.Value
		}()
	}

	requests := waitForRequests(t, writer, 2)
	firstID := requests[0]["id"]
	secondID := requests[1]["id"]
	reader.WriteString(fmt.Sprintf("{\"id\":%v,\"result\":{\"value\":\"second-result\"}}\n", secondID))
	reader.WriteString("{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"x\"}}\n")
	reader.WriteString("{\"id\":\"approval-1\",\"method\":\"item/fileChange/requestApproval\",\"params\":{}}\n")
	reader.WriteString(fmt.Sprintf("{\"id\":%v,\"result\":{\"value\":\"first-result\"}}\n", firstID))

	want := map[string]bool{"first-result": true, "second-result": true}
	for range 2 {
		select {
		case result := <-results:
			if !want[result] {
				t.Fatalf("响应路由错误：%q", result)
			}
		case <-time.After(time.Second):
			t.Fatal("等待乱序响应超时")
		}
	}
	if notification := <-client.Notifications(); notification.Method != "item/agentMessage/delta" {
		t.Fatalf("通知路由错误：%+v", notification)
	}
	if request := <-client.ServerRequests(); request.ID == nil || request.Method != "item/fileChange/requestApproval" {
		t.Fatalf("ServerRequest 路由错误：%+v", request)
	}
}

// TestRPCClient_FailsAfterInvalidUnknownAndEOF 验证协议异常或 EOF 会终止后续 RPC。
func TestRPCClient_FailsAfterInvalidUnknownAndEOF(t *testing.T) {
	for _, input := range []string{"{invalid}\n", "{\"id\":99,\"result\":{}}\n", ""} {
		client := codex.NewRPCClient(codex.NewCodec(bytes.NewBufferString(input), io.Discard))
		ctx, cancel := context.WithCancel(context.Background())
		client.Start(ctx)
		select {
		case <-client.Done():
		case <-time.After(time.Second):
			t.Fatalf("协议异常未终止 client：input=%q", input)
		}
		cancel()
		if err := client.Call(context.Background(), "after/close", nil, nil); err == nil {
			t.Fatalf("终止后的 RPC 必须失败：input=%q", input)
		}
	}
}

// TestRPCClient_RespondPreservesStringRequestID 验证审批响应保留原生字符串 ID。
func TestRPCClient_RespondPreservesStringRequestID(t *testing.T) {
	reader := newSynchronizedBuffer()
	defer reader.Close()
	writer := newSynchronizedBuffer()
	client := codex.NewRPCClient(codex.NewCodec(reader, writer))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.Start(ctx)

	if err := client.Respond(codex.StringRequestID("approval-1"), map[string]string{"decision": "accept"}, nil); err != nil {
		t.Fatalf("写回 ServerRequest 失败：%v", err)
	}
	responses := waitForRequests(t, writer, 1)
	if responses[0]["id"] != "approval-1" {
		t.Fatalf("ServerRequest 响应 ID 类型丢失：%#v", responses[0])
	}
	result, ok := responses[0]["result"].(map[string]any)
	if !ok || result["decision"] != "accept" {
		t.Fatalf("ServerRequest 响应结果错误：%#v", responses[0])
	}
}

type synchronizedBuffer struct {
	mu        sync.Mutex
	data      bytes.Buffer
	wake      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

// newSynchronizedBuffer 创建可由测试双方并发读写的阻塞内存流。
func newSynchronizedBuffer() *synchronizedBuffer {
	return &synchronizedBuffer{wake: make(chan struct{}, 1), closed: make(chan struct{})}
}

// Read 等待新数据或关闭信号，模拟进程 stdout。
func (buffer *synchronizedBuffer) Read(content []byte) (int, error) {
	for {
		buffer.mu.Lock()
		if buffer.data.Len() > 0 {
			count, err := buffer.data.Read(content)
			buffer.mu.Unlock()
			return count, err
		}
		buffer.mu.Unlock()
		select {
		case <-buffer.wake:
		case <-buffer.closed:
			return 0, io.EOF
		}
	}
}

// Write 追加完整消息并唤醒 reader。
func (buffer *synchronizedBuffer) Write(content []byte) (int, error) {
	buffer.mu.Lock()
	count, err := buffer.data.Write(content)
	buffer.mu.Unlock()
	select {
	case buffer.wake <- struct{}{}:
	default:
	}
	return count, err
}

// WriteString 便于测试注入一条 JSONL 消息。
func (buffer *synchronizedBuffer) WriteString(content string) {
	_, _ = buffer.Write([]byte(content))
}

// Close 结束阻塞 reader，避免测试泄漏 goroutine。
func (buffer *synchronizedBuffer) Close() {
	buffer.closeOnce.Do(func() { close(buffer.closed) })
}

// waitForRequests 等待 client writer 生成指定数量的完整 JSONL 请求。
func waitForRequests(t *testing.T, buffer *synchronizedBuffer, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		buffer.mu.Lock()
		content := append([]byte(nil), buffer.data.Bytes()...)
		buffer.mu.Unlock()
		lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
		if len(lines) >= count {
			requests := make([]map[string]any, 0, count)
			for _, line := range lines[:count] {
				var request map[string]any
				if err := json.Unmarshal(line, &request); err != nil {
					t.Fatalf("解析 RPC 请求失败：%v", err)
				}
				requests = append(requests, request)
			}
			return requests
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待 RPC 请求超时")
	return nil
}
