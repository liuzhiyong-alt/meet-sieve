package singleinstance_test

import (
	"testing"

	"meet-sieve/internal/infra/singleinstance"
)

// TestWindowsContract_UsesStableMutexAndSessionPipe 验证应用和安装器共用固定单实例名称。
func TestWindowsContract_UsesStableMutexAndSessionPipe(t *testing.T) {
	t.Parallel()

	if singleinstance.WindowsMutexName != `Global\MeetSieve.App.Instance.v1` {
		t.Fatalf("Windows mutex 名称不正确：got %q", singleinstance.WindowsMutexName)
	}

	const sessionID = uint32(42)
	if got, want := singleinstance.ActivationPipeName(sessionID), `\\.\pipe\MeetSieve.App.Activate.v1.42`; got != want {
		t.Fatalf("激活管道名称不正确：got %q, want %q", got, want)
	}
}
