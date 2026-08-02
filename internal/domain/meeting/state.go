package meeting

// LifecycleState 表示会议在本地录音链路中的生命周期阶段。
type LifecycleState string

// LocalSaveState 表示本地录音文件的保存与校验状态。
type LocalSaveState string

const (
	// LifecyclePreparing 表示会议快照已创建，尚未取得可持久化的首帧 PCM。
	LifecyclePreparing LifecycleState = "preparing"
	// LifecycleRecording 表示会议正在持续写入本地录音分片。
	LifecycleRecording LifecycleState = "recording"
	// LifecycleFinalizing 表示采集已停止，正在关闭分片和合并录音。
	LifecycleFinalizing LifecycleState = "finalizing"
	// LifecycleEnded 表示本地录音已安全收尾并完成最终校验。
	LifecycleEnded LifecycleState = "ended"
	// LifecycleInterrupted 表示录音因失败或强退恢复而中断，不能继续原会议录音。
	LifecycleInterrupted LifecycleState = "interrupted"

	// LocalSavePending 表示尚未持久化首帧或尚未开始最终收尾。
	LocalSavePending LocalSaveState = "pending"
	// LocalSaveSaving 表示正在写入分片或合并最终录音。
	LocalSaveSaving LocalSaveState = "saving"
	// LocalSaveSaved 表示最终录音已校验并安全保存。
	LocalSaveSaved LocalSaveState = "saved"
	// LocalSaveFailed 表示本地保存未完成，恢复材料仍需保留。
	LocalSaveFailed LocalSaveState = "failed"
)

// CanTransitionTo 判断生命周期是否允许转换到目标阶段。
func (state LifecycleState) CanTransitionTo(target LifecycleState) bool {
	if target == LifecycleInterrupted {
		return state == LifecyclePreparing || state == LifecycleRecording || state == LifecycleFinalizing
	}
	if state == LifecyclePreparing {
		return target == LifecycleRecording
	}
	if state == LifecycleRecording {
		return target == LifecycleFinalizing
	}
	return state == LifecycleFinalizing && target == LifecycleEnded
}

// CanTransitionTo 判断本地保存状态是否允许转换到目标阶段。
func (state LocalSaveState) CanTransitionTo(target LocalSaveState) bool {
	if state == LocalSavePending {
		return target == LocalSaveSaving
	}
	return state == LocalSaveSaving && (target == LocalSaveSaved || target == LocalSaveFailed)
}
