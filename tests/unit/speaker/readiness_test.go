package speaker_test

import (
	"testing"

	speaker "meet-sieve/internal/domain/speaker"
)

// TestDetermineAutomationState_KeepsSpeakerReadinessIndependent 验证自动识别门禁只返回 speaker 状态。
func TestDetermineAutomationState_KeepsSpeakerReadinessIndependent(t *testing.T) {
	tests := []struct {
		name  string
		input speaker.ReadinessInput
		want  speaker.AutomationState
	}{
		{name: "model", input: speaker.ReadinessInput{Profile: speaker.ProfileAvailable}, want: speaker.AutomationModelUnavailable},
		{name: "missing", input: speaker.ReadinessInput{ModelUsable: true, Profile: speaker.ProfileMissing}, want: speaker.AutomationProfileMissing},
		{name: "mismatch", input: speaker.ReadinessInput{ModelUsable: true, Profile: speaker.ProfileMismatch}, want: speaker.AutomationProfileMismatch},
		{name: "rebuild", input: speaker.ReadinessInput{ModelUsable: true, Profile: speaker.ProfileAvailable, AcceptedSampleCount: 2, CurrentEmbeddingCount: 1}, want: speaker.AutomationVoiceRebuildRequired},
		{name: "unknown_only", input: speaker.ReadinessInput{ModelUsable: true, Profile: speaker.ProfileAvailable}, want: speaker.AutomationReady},
		{name: "ready", input: speaker.ReadinessInput{ModelUsable: true, Profile: speaker.ProfileAvailable, AcceptedSampleCount: 2, CurrentEmbeddingCount: 2}, want: speaker.AutomationReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := speaker.DetermineAutomationState(test.input)
			if state != test.want || !state.IsValid() {
				t.Fatalf("speaker readiness 错误：got=%q want=%q", state, test.want)
			}
		})
	}
}
