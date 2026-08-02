package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"meet-sieve/internal/infra/apperr"
)

const (
	initializeRequestID = 1
	processExitTimeout  = 3 * time.Second
)

// Config 描述 Codex app-server 可执行文件和固定参数。
type Config struct {
	Command string
	Args    []string
}

// InitializeResult 是握手成功后需要记录的服务端信息。
type InitializeResult struct {
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
	UserAgent      string `json:"userAgent"`
}

// Client 管理一次短生命周期的 Codex app-server 进程。
type Client struct {
	config Config
}

// NewClient 创建 Codex app-server 客户端。
func NewClient(config Config) *Client {
	return &Client{config: config}
}

// Handshake 启动 Codex，完成 initialize/initialized 后关闭输入并等待进程退出。
func (c *Client) Handshake(ctx context.Context) (InitializeResult, error) {
	process, err := startProcess(c.config)
	if err != nil {
		return InitializeResult{}, dependencyError(err, "codex.process.start")
	}
	defer process.ensureStopped()

	if err := writeInitialize(process.codec); err != nil {
		return InitializeResult{}, dependencyError(err, "codex.initialize.write")
	}
	message, err := readInitializeResponse(ctx, process)
	if err != nil {
		return InitializeResult{}, err
	}
	result, err := parseInitializeResult(message)
	if err != nil {
		return InitializeResult{}, dependencyError(err, "codex.initialize.parse")
	}
	if err := process.codec.Write(Request{Method: "initialized"}); err != nil {
		return InitializeResult{}, dependencyError(err, "codex.initialized.write")
	}
	if err := process.closeAndWait(ctx); err != nil {
		return InitializeResult{}, dependencyError(err, "codex.process.stop")
	}
	return result, nil
}

// writeInitialize 发送关闭实验能力的最小 initialize 请求。
func writeInitialize(codec *Codec) error {
	params := map[string]any{
		"clientInfo": map[string]string{
			"name":    "meetsieve",
			"title":   "MeetSieve",
			"version": "step0",
		},
		"capabilities": map[string]bool{"experimentalApi": false},
	}
	return codec.Write(Request{ID: initializeRequestID, Method: "initialize", Params: params})
}

// readInitializeResponse 等待匹配请求 ID 的响应，并把取消和超时转换为统一错误。
func readInitializeResponse(ctx context.Context, process *process) (Message, error) {
	type readResult struct {
		message Message
		err     error
	}
	resultChannel := make(chan readResult, 1)
	go func() {
		for {
			message, err := process.codec.Read()
			if err != nil || message.ID != nil {
				resultChannel <- readResult{message: message, err: err}
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Message{}, apperr.Dependency(
				apperr.CodeDependencyTimeout,
				ctx.Err(),
				apperr.WithOp("codex.initialize.wait"),
			)
		}
		return Message{}, apperr.Biz(apperr.CodeCanceled, apperr.WithOp("codex.initialize.wait"))
	case result := <-resultChannel:
		if result.err != nil {
			return Message{}, dependencyError(result.err, "codex.initialize.read")
		}
		if result.message.ID == nil || *result.message.ID != initializeRequestID {
			return Message{}, dependencyError(fmt.Errorf("initialize 响应 ID 不匹配"), "codex.initialize.match")
		}
		return result.message, nil
	}
}

// parseInitializeResult 校验协议错误和握手结果的必要字段。
func parseInitializeResult(message Message) (InitializeResult, error) {
	if message.Error != nil {
		return InitializeResult{}, fmt.Errorf("Codex initialize 返回协议错误 code=%d", message.Error.Code)
	}
	var result InitializeResult
	if err := json.Unmarshal(message.Result, &result); err != nil {
		return InitializeResult{}, fmt.Errorf("解析 initialize result 失败: %w", err)
	}
	if result.UserAgent == "" || result.PlatformOS == "" || result.PlatformFamily == "" {
		return InitializeResult{}, fmt.Errorf("initialize result 缺少必要字段")
	}
	return result, nil
}

// dependencyError 将 Codex 进程或协议错误统一归类为外部依赖错误。
func dependencyError(cause error, op string) error {
	if errors.Is(cause, io.EOF) {
		cause = fmt.Errorf("Codex app-server 提前结束: %w", cause)
	}
	return apperr.Dependency(apperr.CodeDependency, cause, apperr.WithOp(op))
}

// startProcess 校验命令并启动隐藏窗口策略已配置的 Codex 子进程。
func startProcess(config Config) (*process, error) {
	if config.Command == "" {
		return nil, fmt.Errorf("Codex 命令不能为空")
	}
	command := exec.Command(config.Command, config.Args...)
	configureProcess(command)
	return newProcess(command)
}
