// Package finalization 定义会议核心收尾的稳定领域状态。
package finalization

// Stage 描述核心收尾当前正在执行的可展示阶段。
type Stage string

const (
	// StageStopLAN 表示正在停止 LAN 访问。
	StageStopLAN Stage = "stop_lan"
	// StageStopCapture 表示正在停止音频采集。
	StageStopCapture Stage = "stop_capture"
	// StageWaitTailFinal 表示正在等待实时转写尾部 final。
	StageWaitTailFinal Stage = "wait_tail_final"
	// StagePersistTranscript 表示正在持久化转写事实。
	StagePersistTranscript Stage = "persist_transcript"
	// StageMergeRecording 表示正在合并并校验完整录音。
	StageMergeRecording Stage = "merge_recording"
	// StageFlushRawRecord 表示正在强制刷新原始记录。
	StageFlushRawRecord Stage = "flush_raw_record"
	// StageCommitLocalSaved 表示正在提交本地保存完成状态。
	StageCommitLocalSaved Stage = "commit_local_saved"
)

// Valid 判断阶段是否属于稳定枚举。
func (stage Stage) Valid() bool {
	switch stage {
	case StageStopLAN, StageStopCapture, StageWaitTailFinal, StagePersistTranscript,
		StageMergeRecording, StageFlushRawRecord, StageCommitLocalSaved:
		return true
	default:
		return false
	}
}
