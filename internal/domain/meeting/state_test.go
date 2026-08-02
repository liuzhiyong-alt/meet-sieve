package meeting

import "testing"

// TestLifecycleStateCanTransitionTo 验证准备中的会议只能进入录音阶段，不能跳过录音直接结束。
func TestLifecycleStateCanTransitionTo(t *testing.T) {
	t.Parallel()

	if !LifecyclePreparing.CanTransitionTo(LifecycleRecording) {
		t.Fatal("preparing 应允许转换到 recording")
	}
	if LifecyclePreparing.CanTransitionTo(LifecycleEnded) {
		t.Fatal("preparing 不应允许跳过录音直接转换到 ended")
	}
}

// TestLifecycleStateAllowsOrderlyCompletion 验证录音完成必须经过收尾阶段。
func TestLifecycleStateAllowsOrderlyCompletion(t *testing.T) {
	t.Parallel()

	if !LifecycleRecording.CanTransitionTo(LifecycleFinalizing) {
		t.Fatal("recording 应允许转换到 finalizing")
	}
	if LifecycleRecording.CanTransitionTo(LifecycleEnded) {
		t.Fatal("recording 不应跳过 finalizing 直接转换到 ended")
	}
}

// TestLifecycleStateEndsOnlyAfterFinalizing 验证最终文件校验完成后才能结束会议。
func TestLifecycleStateEndsOnlyAfterFinalizing(t *testing.T) {
	t.Parallel()

	if !LifecycleFinalizing.CanTransitionTo(LifecycleEnded) {
		t.Fatal("finalizing 应允许转换到 ended")
	}
}

// TestLifecycleStateInterruptsActiveMeeting 验证活动会议遇到不可恢复错误可以进入中断态，终态不能回退。
func TestLifecycleStateInterruptsActiveMeeting(t *testing.T) {
	t.Parallel()

	for _, state := range []LifecycleState{LifecyclePreparing, LifecycleRecording, LifecycleFinalizing} {
		if !state.CanTransitionTo(LifecycleInterrupted) {
			t.Fatalf("%s 应允许转换到 interrupted", state)
		}
	}
	if LifecycleInterrupted.CanTransitionTo(LifecycleRecording) {
		t.Fatal("interrupted 不应允许恢复为 recording")
	}
}

// TestLocalSaveStateCanTransitionTo 验证本地保存只能从待保存推进到保存中和终态。
func TestLocalSaveStateCanTransitionTo(t *testing.T) {
	t.Parallel()

	if !LocalSavePending.CanTransitionTo(LocalSaveSaving) {
		t.Fatal("pending 应允许转换到 saving")
	}
	if LocalSavePending.CanTransitionTo(LocalSaveSaved) {
		t.Fatal("pending 不应直接转换到 saved")
	}
}

// TestLocalSaveStateFinishesFromSaving 验证保存中可以进入成功或失败终态，终态不可回退。
func TestLocalSaveStateFinishesFromSaving(t *testing.T) {
	t.Parallel()

	if !LocalSaveSaving.CanTransitionTo(LocalSaveSaved) {
		t.Fatal("saving 应允许转换到 saved")
	}
	if !LocalSaveSaving.CanTransitionTo(LocalSaveFailed) {
		t.Fatal("saving 应允许转换到 failed")
	}
	if LocalSaveSaved.CanTransitionTo(LocalSaveSaving) || LocalSaveFailed.CanTransitionTo(LocalSaveSaving) {
		t.Fatal("本地保存终态不应回退到 saving")
	}
}
