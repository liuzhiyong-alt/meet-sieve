package codex_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"meet-sieve/internal/adapter/agent/codex"
	"meet-sieve/internal/infra/apperr"
)

// TestHandshake_CompletesInitializeAndInitialized 验证真实子进程完成两阶段握手并退出。
func TestHandshake_CompletesInitializeAndInitialized(t *testing.T) {
	client := codex.NewClient(codex.Config{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestCodexHelperProcess", "--", "success"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Handshake(ctx)
	if err != nil {
		t.Fatalf("握手失败：%v", err)
	}
	if result.UserAgent != "codex-test" {
		t.Fatalf("握手结果不正确：%+v", result)
	}
}

// TestHandshake_MapsTimeoutToDependencyTimeout 验证超时被映射为稳定的 504 依赖错误。
func TestHandshake_MapsTimeoutToDependencyTimeout(t *testing.T) {
	client := codex.NewClient(codex.Config{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestCodexHelperProcess", "--", "timeout"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Handshake(ctx)
	appErr := apperr.Normalize(err)
	if appErr.Code != apperr.CodeDependencyTimeout.Value {
		t.Fatalf("超时错误码不正确：got %d, err=%v", appErr.Code, err)
	}
}

// TestCodexHelperProcess 模拟 stdio JSONL app-server，供集成测试验证真实进程边界。
func TestCodexHelperProcess(t *testing.T) {
	if len(os.Args) < 2 {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode != "success" && mode != "timeout" {
		return
	}

	codec := json.NewDecoder(os.Stdin)
	var initialize map[string]any
	if err := codec.Decode(&initialize); err != nil {
		os.Exit(2)
	}
	if mode == "timeout" {
		time.Sleep(10 * time.Second)
		os.Exit(3)
	}
	if initialize["method"] != "initialize" {
		os.Exit(4)
	}
	fmt.Fprintln(os.Stdout, `{"id":1,"result":{"codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"macos","userAgent":"codex-test"}}`)

	var initialized map[string]any
	if err := codec.Decode(&initialized); err != nil {
		os.Exit(5)
	}
	if initialized["method"] != "initialized" {
		os.Exit(6)
	}
	os.Exit(0)
}
