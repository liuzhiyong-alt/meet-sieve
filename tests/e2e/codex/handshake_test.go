package codex_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"meet-sieve/internal/adapter/agent/codex"
)

// TestCodexHandshake_CompletesAgainstInstalledCodex 验证本机 Codex app-server 的真实协议握手。
func TestCodexHandshake_CompletesAgainstInstalledCodex(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Fatalf("本机未安装 codex：%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := codex.NewClient(codex.Config{
		Command: "codex",
		Args:    []string{"app-server", "--stdio"},
	})
	result, err := client.Handshake(ctx)
	if err != nil {
		t.Fatalf("真实 Codex 握手失败：%v", err)
	}
	if result.UserAgent == "" || result.PlatformOS == "" {
		t.Fatalf("Codex initialize 响应缺少必要字段：%+v", result)
	}
	t.Logf(
		"Codex 真实握手成功：user_agent=%s platform=%s/%s",
		result.UserAgent,
		result.PlatformFamily,
		result.PlatformOS,
	)
}
