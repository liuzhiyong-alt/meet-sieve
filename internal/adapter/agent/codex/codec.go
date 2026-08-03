// Package codex 封装本机 Codex app-server 的 stdio JSONL 协议。
package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
)

// RequestID 保留 JSON-RPC request ID 的整数或字符串原始类型。
type RequestID struct {
	integer *int64
	text    *string
}

// IntRequestID 创建整数 request ID。
func IntRequestID(value int64) RequestID {
	return RequestID{integer: &value}
}

// StringRequestID 创建字符串 request ID。
func StringRequestID(value string) RequestID {
	return RequestID{text: &value}
}

// Key 返回可安全作为 pending map key 的带类型身份。
func (id RequestID) Key() string {
	if id.integer != nil {
		return "i:" + strconv.FormatInt(*id.integer, 10)
	}
	if id.text != nil {
		return "s:" + *id.text
	}
	return ""
}

// Equal 判断两个 request ID 的类型和值是否相同。
func (id RequestID) Equal(other RequestID) bool {
	return id.Key() != "" && id.Key() == other.Key()
}

// MarshalJSON 保持 request ID 的原生 JSON 类型。
func (id RequestID) MarshalJSON() ([]byte, error) {
	if id.integer != nil {
		return json.Marshal(*id.integer)
	}
	if id.text != nil {
		return json.Marshal(*id.text)
	}
	return nil, fmt.Errorf("Codex request ID 为空")
}

// UnmarshalJSON 只接受 schema 允许的 int64 或 string。
func (id *RequestID) UnmarshalJSON(content []byte) error {
	content = bytes.TrimSpace(content)
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return fmt.Errorf("Codex request ID 为空")
	}
	if content[0] == '"' {
		var value string
		if err := json.Unmarshal(content, &value); err != nil {
			return fmt.Errorf("解析字符串 request ID 失败: %w", err)
		}
		*id = StringRequestID(value)
		return nil
	}
	var value int64
	if err := json.Unmarshal(content, &value); err != nil {
		return fmt.Errorf("解析整数 request ID 失败: %w", err)
	}
	*id = IntRequestID(value)
	return nil
}

// Request 是发送给 Codex app-server 的 JSONL 请求或通知。
type Request struct {
	ID     *RequestID `json:"id,omitempty"`
	Method string     `json:"method"`
	Params any        `json:"params,omitempty"`
}

// Response 是发送给 app-server 的 ServerRequest 响应。
type Response struct {
	ID     RequestID      `json:"id"`
	Result any            `json:"result,omitempty"`
	Error  *ProtocolError `json:"error,omitempty"`
}

// ProtocolError 是 Codex 返回的 JSON-RPC 错误。
type ProtocolError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Message 是从 Codex app-server 读取的响应或通知。
type Message struct {
	ID     *RequestID      `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ProtocolError  `json:"error,omitempty"`
}

// Codec 对 stdio 流执行逐行 JSON 编解码。
type Codec struct {
	scanner *bufio.Scanner
	writer  io.Writer
	writeMu sync.Mutex
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
	return c.writeJSON(request)
}

// WriteResponse 把一次 ServerRequest 的结果或协议错误写回 app-server。
func (c *Codec) WriteResponse(response Response) error {
	return c.writeJSON(response)
}

// writeJSON 在同一 writer mutex 内编码并写入完整 JSONL envelope。
func (c *Codec) writeJSON(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.writer == nil {
		return fmt.Errorf("Codex JSONL 写端不可用")
	}
	data, err := json.Marshal(value)
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
