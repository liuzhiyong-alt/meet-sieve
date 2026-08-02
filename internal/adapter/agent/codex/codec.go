// Package codex 封装本机 Codex app-server 的 stdio JSONL 协议。
package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Request 是发送给 Codex app-server 的 JSONL 请求或通知。
type Request struct {
	ID     int    `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

// ProtocolError 是 Codex 返回的 JSON-RPC 错误。
type ProtocolError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Message 是从 Codex app-server 读取的响应或通知。
type Message struct {
	ID     *int            `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ProtocolError  `json:"error,omitempty"`
}

// Codec 对 stdio 流执行逐行 JSON 编解码。
type Codec struct {
	scanner *bufio.Scanner
	writer  io.Writer
}

// NewCodec 创建 JSONL 编解码器；不需要的读端或写端可以传 nil。
func NewCodec(reader io.Reader, writer io.Writer) *Codec {
	var scanner *bufio.Scanner
	if reader != nil {
		scanner = bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	}
	return &Codec{scanner: scanner, writer: writer}
}

// Write 将单个请求编码成一行 JSON 并立即写入目标流。
func (c *Codec) Write(request Request) error {
	if c.writer == nil {
		return fmt.Errorf("Codex JSONL 写端不可用")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("编码 Codex 请求失败: %w", err)
	}
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		return fmt.Errorf("写入 Codex 请求失败: %w", err)
	}
	return nil
}

// Read 读取并解析下一条完整 JSONL 消息。
func (c *Codec) Read() (Message, error) {
	if c.scanner == nil {
		return Message{}, fmt.Errorf("Codex JSONL 读端不可用")
	}
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return Message{}, fmt.Errorf("读取 Codex 响应失败: %w", err)
		}
		return Message{}, io.EOF
	}
	var message Message
	if err := json.Unmarshal(c.scanner.Bytes(), &message); err != nil {
		return Message{}, fmt.Errorf("解析 Codex 响应失败: %w", err)
	}
	return message, nil
}
