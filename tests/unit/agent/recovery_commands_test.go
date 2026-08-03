package agent_test

import (
	"strings"
	"testing"

	agentservice "meet-sieve/internal/service/agent"
)

func TestBuildRecoveryCommandsUsesPlatformSafeQuoting(t *testing.T) {
	const threadID = "0198c3a1-2ee2-7a11-bf62-123456789abc"
	posix, err := agentservice.BuildRecoveryCommands("darwin", threadID, "/tmp/会议 '甲'")
	if err != nil {
		t.Fatalf("构造 POSIX 恢复命令失败: %v", err)
	}
	if !strings.Contains(posix.DirectoryCommand, `'/tmp/会议 '\''甲'\'''`) {
		t.Fatalf("POSIX 路径未安全引用: %s", posix.DirectoryCommand)
	}
	windows, err := agentservice.BuildRecoveryCommands("windows", threadID, `C:\Meet Sieve\会议'甲`)
	if err != nil {
		t.Fatalf("构造 PowerShell 恢复命令失败: %v", err)
	}
	if !strings.Contains(windows.DirectoryCommand, `'C:\Meet Sieve\会议''甲'`) {
		t.Fatalf("PowerShell 路径未安全引用: %s", windows.DirectoryCommand)
	}
}

func TestBuildRecoveryCommandsRejectsUnsafeThreadID(t *testing.T) {
	if _, err := agentservice.BuildRecoveryCommands("darwin", "id; touch /tmp/pwned", "/tmp/meeting"); err == nil {
		t.Fatal("恢复命令不应接受可注入的 thread ID")
	}
}
