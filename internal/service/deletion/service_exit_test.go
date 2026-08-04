package deletion

import (
	"context"
	"testing"
	"time"
)

// TestPrepareExitWaitsForCheckpointAndBlocksNewCommands 验证退出门不强杀当前原子项。
func TestPrepareExitWaitsForCheckpointAndBlocksNewCommands(t *testing.T) {
	service := NewService(Dependencies{})
	if !service.enterOperation() {
		t.Fatal("应允许首个删除命令进入")
	}
	done := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done <- service.PrepareExit(ctx)
	}()
	time.Sleep(10 * time.Millisecond)
	if service.enterOperation() {
		t.Fatal("退出门开启后不得领取新删除命令")
	}
	service.leaveOperation()
	if safe := <-done; !safe {
		t.Fatal("当前原子项完成持久化后应允许退出")
	}
}

// TestPrepareExitTimeoutReopensCommands 验证阻止退出后服务仍可继续使用。
func TestPrepareExitTimeoutReopensCommands(t *testing.T) {
	service := NewService(Dependencies{})
	if !service.enterOperation() {
		t.Fatal("应允许首个删除命令进入")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if service.PrepareExit(ctx) {
		t.Fatal("未持久化完成时必须阻止退出")
	}
	service.leaveOperation()
	if !service.enterOperation() {
		t.Fatal("阻止退出后应恢复接收命令")
	}
	service.leaveOperation()
}
