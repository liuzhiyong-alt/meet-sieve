package speaker

// AutomationState 表示自动说话人处理的独立可用状态，不复用录音或 ASR 状态。
type AutomationState string

const (
	// AutomationReady 表示模型、档案和当前 embedding 均可用于自动处理。
	AutomationReady AutomationState = "ready"
	// AutomationModelUnavailable 表示模型或运行时不可用。
	AutomationModelUnavailable AutomationState = "model_unavailable"
	// AutomationProfileMissing 表示尚无正式校准档案。
	AutomationProfileMissing AutomationState = "profile_missing"
	// AutomationProfileMismatch 表示档案无效或与模型四元组不一致。
	AutomationProfileMismatch AutomationState = "profile_mismatch"
	// AutomationVoiceRebuildRequired 表示 accepted 样本尚未全部生成当前模型 embedding。
	AutomationVoiceRebuildRequired AutomationState = "voice_rebuild_required"
)

// ProfileAvailability 描述正式校准档案的加载结果。
type ProfileAvailability string

const (
	// ProfileAvailable 表示档案严格校验通过。
	ProfileAvailable ProfileAvailability = "available"
	// ProfileMissing 表示正式档案文件不存在。
	ProfileMissing ProfileAvailability = "missing"
	// ProfileMismatch 表示档案内容无效或模型身份不一致。
	ProfileMismatch ProfileAvailability = "mismatch"
)

// ReadinessInput 是自动处理门禁所需的最小状态集合。
type ReadinessInput struct {
	ModelUsable           bool
	Profile               ProfileAvailability
	AcceptedSampleCount   int
	CurrentEmbeddingCount int
}

// DetermineAutomationState 按模型、档案、embedding 的固定优先级返回独立 speaker 状态。
func DetermineAutomationState(input ReadinessInput) AutomationState {
	if !input.ModelUsable {
		return AutomationModelUnavailable
	}
	switch input.Profile {
	case ProfileMissing:
		return AutomationProfileMissing
	case ProfileMismatch:
		return AutomationProfileMismatch
	case ProfileAvailable:
		if input.AcceptedSampleCount > input.CurrentEmbeddingCount {
			return AutomationVoiceRebuildRequired
		}
		return AutomationReady
	default:
		return AutomationProfileMismatch
	}
}

// IsValid 判断状态是否属于已确认的自动识别 readiness 集合。
func (state AutomationState) IsValid() bool {
	switch state {
	case AutomationReady, AutomationModelUnavailable, AutomationProfileMissing,
		AutomationProfileMismatch, AutomationVoiceRebuildRequired:
		return true
	default:
		return false
	}
}
