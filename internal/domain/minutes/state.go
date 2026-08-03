// Package minutes 定义会议纪要版本的稳定领域状态。
package minutes

// Source 描述不可变纪要版本的来源。
type Source string

const (
	// SourceAI 表示版本由已校验的 AI 结构化输出生成。
	SourceAI Source = "ai"
	// SourceHuman 表示版本由主持人人工保存。
	SourceHuman Source = "human"
	// SourceRestored 表示版本由历史版本恢复产生。
	SourceRestored Source = "restored"
)

// Valid 判断版本来源是否属于稳定枚举。
func (source Source) Valid() bool {
	switch source {
	case SourceAI, SourceHuman, SourceRestored:
		return true
	default:
		return false
	}
}

// State 描述不可变纪要版本的确认状态。
type State string

const (
	// StateDraft 表示版本仍是草稿。
	StateDraft State = "draft"
	// StateConfirmed 表示主持人已确认版本。
	StateConfirmed State = "confirmed"
)

// Valid 判断版本状态是否属于稳定枚举。
func (state State) Valid() bool {
	return state == StateDraft || state == StateConfirmed
}
