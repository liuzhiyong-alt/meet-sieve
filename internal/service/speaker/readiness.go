package speaker

import (
	domain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
)

// ReadinessStatus 是 UI 可直接消费的独立 speaker 门禁快照。
type ReadinessStatus struct {
	State     domain.AutomationState `json:"state"`
	ErrorCode string                 `json:"error_code,omitempty"`
	ProfileID string                 `json:"profile_id,omitempty"`
	Model     domain.ModelIdentity   `json:"model"`
}

// BuildReadinessStatus 将领域状态映射为稳定错误码；非 ready 状态不携带 profile ID。
func BuildReadinessStatus(state domain.AutomationState, model domain.ModelIdentity, profileID string) ReadinessStatus {
	status := ReadinessStatus{State: state, Model: model}
	switch state {
	case domain.AutomationReady:
		status.ProfileID = profileID
	case domain.AutomationModelUnavailable:
		status.ErrorCode = apperr.CodeSpeakerModelUnavailable.ErrorCode
	case domain.AutomationProfileMissing:
		status.ErrorCode = apperr.CodeSpeakerProfileMissing.ErrorCode
	case domain.AutomationProfileMismatch:
		status.ErrorCode = apperr.CodeSpeakerProfileMismatch.ErrorCode
	case domain.AutomationVoiceRebuildRequired:
		status.ErrorCode = apperr.CodeVoiceRebuildIncomplete.ErrorCode
	default:
		status.State = domain.AutomationProfileMismatch
		status.ErrorCode = apperr.CodeSpeakerProfileMismatch.ErrorCode
	}
	return status
}
