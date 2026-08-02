package speaker_test

import (
	"testing"

	speakerdomain "meet-sieve/internal/domain/speaker"
	"meet-sieve/internal/infra/apperr"
	speakerservice "meet-sieve/internal/service/speaker"
)

// TestBuildReadinessStatus_ExposesIndependentStableState 验证 DTO 不借用录音或 ASR 状态表达 speaker 门禁。
func TestBuildReadinessStatus_ExposesIndependentStableState(t *testing.T) {
	tests := []struct {
		state     speakerdomain.AutomationState
		errorCode string
	}{
		{state: speakerdomain.AutomationReady},
		{state: speakerdomain.AutomationModelUnavailable, errorCode: apperr.CodeSpeakerModelUnavailable.ErrorCode},
		{state: speakerdomain.AutomationProfileMissing, errorCode: apperr.CodeSpeakerProfileMissing.ErrorCode},
		{state: speakerdomain.AutomationProfileMismatch, errorCode: apperr.CodeSpeakerProfileMismatch.ErrorCode},
		{state: speakerdomain.AutomationVoiceRebuildRequired, errorCode: apperr.CodeVoiceRebuildIncomplete.ErrorCode},
	}
	for _, test := range tests {
		status := speakerservice.BuildReadinessStatus(test.state, expectedModel, "test-profile")
		if status.State != test.state || status.ErrorCode != test.errorCode || status.Model != expectedModel {
			t.Fatalf("readiness DTO 错误：got=%+v", status)
		}
		if test.state == speakerdomain.AutomationReady && status.ProfileID != "test-profile" {
			t.Fatalf("ready 状态必须携带已校验 profile ID：%+v", status)
		}
		if test.state != speakerdomain.AutomationReady && status.ProfileID != "" {
			t.Fatalf("不可用状态不得伪装已加载 profile：%+v", status)
		}
	}
}
