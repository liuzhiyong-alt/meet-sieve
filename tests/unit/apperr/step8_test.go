package apperr_test

import (
	"errors"
	"strings"
	"testing"

	"meet-sieve/internal/infra/apperr"
)

// TestStep8Codes_ProvideStableSafeSemantics 验证 Step 8 错误码完整、唯一且不泄漏内部内容。
func TestStep8Codes_ProvideStableSafeSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code      apperr.Code
		want      string
		kind      apperr.Kind
		retryable bool
	}{
		{apperr.CodeFinalizationFailed, "FINALIZATION_FAILED", apperr.KindSystem, true},
		{apperr.CodeGapAudioUnavailable, "GAP_AUDIO_UNAVAILABLE", apperr.KindSystem, true},
		{apperr.CodeGapAudioInvalid, "GAP_AUDIO_INVALID", apperr.KindSystem, true},
		{apperr.CodeGapRequestTooLarge, "GAP_REQUEST_TOO_LARGE", apperr.KindBusiness, false},
		{apperr.CodeGapTranscriptionTimeout, "GAP_TRANSCRIPTION_TIMEOUT", apperr.KindDependency, true},
		{apperr.CodeGapTranscriptionRejected, "GAP_TRANSCRIPTION_REJECTED", apperr.KindDependency, true},
		{apperr.CodeGapTranscriptionNoSpeech, "GAP_TRANSCRIPTION_NO_SPEECH", apperr.KindBusiness, false},
		{apperr.CodeGapTranscriptionConflict, "GAP_TRANSCRIPTION_CONFLICT", apperr.KindBusiness, true},
		{apperr.CodeGapTranscriptionCancelled, "GAP_TRANSCRIPTION_CANCELLED", apperr.KindBusiness, true},
		{apperr.CodeGapAttemptInterrupted, "GAP_ATTEMPT_INTERRUPTED", apperr.KindSystem, true},
		{apperr.CodeAgentFinalSyncFailed, "AGENT_FINAL_SYNC_FAILED", apperr.KindDependency, true},
		{apperr.CodeMinutesGapProcessing, "MINUTES_GAP_PROCESSING", apperr.KindBusiness, true},
		{apperr.CodeMinutesBusy, "MINUTES_BUSY", apperr.KindBusiness, true},
		{apperr.CodeMinutesOutputInvalid, "MINUTES_OUTPUT_INVALID", apperr.KindDependency, true},
		{apperr.CodeMinutesVersionConflict, "MINUTES_VERSION_CONFLICT", apperr.KindBusiness, true},
		{apperr.CodeMinutesProjectionFailed, "MINUTES_PROJECTION_FAILED", apperr.KindSystem, true},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		if test.code.ErrorCode != test.want || test.code.Kind != test.kind ||
			test.code.Retryable != test.retryable || test.code.Message == "" {
			t.Errorf("Step 8 错误码不正确：got=%+v want=%s kind=%s retryable=%t", test.code, test.want, test.kind, test.retryable)
		}
		if _, exists := seen[test.code.ErrorCode]; exists {
			t.Errorf("Step 8 错误码重复：%q", test.code.ErrorCode)
		}
		seen[test.code.ErrorCode] = struct{}{}
	}

	cause := errors.New("/private/meeting response=secret transcript=secret")
	result := apperr.Dependency(apperr.CodeMinutesOutputInvalid, cause)
	if strings.Contains(result.Message, "secret") || strings.Contains(result.Message, "/private/") {
		t.Fatalf("用户文案泄漏内部内容：%q", result.Message)
	}
	if !errors.Is(result, cause) {
		t.Fatal("内部 cause 必须保留用于脱敏排障")
	}
}
