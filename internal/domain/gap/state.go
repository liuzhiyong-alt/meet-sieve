// Package gap 定义会后缺口补转写的稳定领域状态。
package gap

// State 描述一个 ASR 缺口的处理状态。
type State string

const (
	// StatePending 表示缺口等待处理。
	StatePending State = "pending"
	// StateProcessing 表示缺口正在补转写。
	StateProcessing State = "processing"
	// StateCompleted 表示缺口已完成补偿或确认无语音。
	StateCompleted State = "completed"
	// StateFailed 表示缺口处理失败，可由用户重试。
	StateFailed State = "failed"
	// StateConflict 表示文件候选与现有事实重叠，等待人工解决。
	StateConflict State = "conflict"
)

// Valid 判断缺口状态是否属于稳定枚举。
func (state State) Valid() bool {
	switch state {
	case StatePending, StateProcessing, StateCompleted, StateFailed, StateConflict:
		return true
	default:
		return false
	}
}

// AttemptState 描述一次可审计补转写尝试的状态。
type AttemptState string

const (
	// AttemptPending 表示尝试已创建但尚未调用 provider。
	AttemptPending AttemptState = "pending"
	// AttemptRunning 表示 provider 请求正在执行。
	AttemptRunning AttemptState = "running"
	// AttemptCompleted 表示补偿事实已提交。
	AttemptCompleted AttemptState = "completed"
	// AttemptFailed 表示本次尝试失败。
	AttemptFailed AttemptState = "failed"
	// AttemptConflict 表示本次尝试保留为冲突证据。
	AttemptConflict AttemptState = "conflict"
	// AttemptCancelled 表示用户停止了本次尝试。
	AttemptCancelled AttemptState = "cancelled"
)

// Valid 判断尝试状态是否属于稳定枚举。
func (state AttemptState) Valid() bool {
	switch state {
	case AttemptPending, AttemptRunning, AttemptCompleted, AttemptFailed, AttemptConflict, AttemptCancelled:
		return true
	default:
		return false
	}
}

// Resolution 描述主持人解决补转写冲突的明确动作。
type Resolution string

const (
	// ResolutionKeepExisting 表示保留现有转写事实。
	ResolutionKeepExisting Resolution = "keep_existing"
	// ResolutionUseFileText 表示采用文件候选文字创建人工修订。
	ResolutionUseFileText Resolution = "use_file_text"
	// ResolutionSaveManualText 表示保存主持人编辑后的人工文字。
	ResolutionSaveManualText Resolution = "save_manual_text"
)

// Valid 判断解决动作是否属于稳定枚举。
func (resolution Resolution) Valid() bool {
	switch resolution {
	case ResolutionKeepExisting, ResolutionUseFileText, ResolutionSaveManualText:
		return true
	default:
		return false
	}
}
