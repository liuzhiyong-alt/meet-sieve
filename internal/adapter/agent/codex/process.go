package codex

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const maxStderrBytes = 64 * 1024

type process struct {
	command   *exec.Cmd
	stdin     io.WriteCloser
	codec     *Codec
	stderr    *limitedBuffer
	waitOnce  sync.Once
	waitError error
}

// newProcess 建立 Codex 标准输入输出管道，并启动 app-server 子进程。
func newProcess(command *exec.Cmd) (*process, error) {
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 Codex stdin 失败: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("创建 Codex stdout 失败: %w", err)
	}
	stderr := &limitedBuffer{limit: maxStderrBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("启动 Codex app-server 失败: %w", err)
	}
	return &process{
		command: command,
		stdin:   stdin,
		codec:   NewCodec(stdout, stdin),
		stderr:  stderr,
	}, nil
}

// closeAndWait 先关闭标准输入请求正常退出，超时或取消时再强制终止。
func (p *process) closeAndWait(ctx context.Context) error {
	if err := p.stdin.Close(); err != nil {
		return fmt.Errorf("关闭 Codex stdin 失败: %w", err)
	}
	waitChannel := make(chan error, 1)
	go func() {
		waitChannel <- p.wait()
	}()

	timer := time.NewTimer(processExitTimeout)
	defer timer.Stop()
	select {
	case err := <-waitChannel:
		if err != nil {
			return fmt.Errorf("Codex 退出失败: %w", err)
		}
		return nil
	case <-ctx.Done():
		_ = p.command.Process.Kill()
		<-waitChannel
		return ctx.Err()
	case <-timer.C:
		_ = p.command.Process.Kill()
		<-waitChannel
		return fmt.Errorf("Codex 进程退出超时")
	}
}

// ensureStopped 兜底终止尚未退出的子进程，避免握手失败后残留。
func (p *process) ensureStopped() {
	if p.command.ProcessState != nil && p.command.ProcessState.Exited() {
		return
	}
	_ = p.stdin.Close()
	_ = p.command.Process.Kill()
	_ = p.wait()
}

// wait 保证 exec.Cmd.Wait 只执行一次，并复用首次等待结果。
func (p *process) wait() error {
	p.waitOnce.Do(func() {
		p.waitError = p.command.Wait()
	})
	return p.waitError
}

type limitedBuffer struct {
	mu    sync.Mutex
	data  bytes.Buffer
	limit int
}

// Write 保留固定上限的 stderr，防止外部进程无限占用内存。
func (b *limitedBuffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	originalLength := len(content)
	remaining := b.limit - b.data.Len()
	if remaining > 0 {
		if len(content) > remaining {
			content = content[:remaining]
		}
		_, _ = b.data.Write(content)
	}
	return originalLength, nil
}
