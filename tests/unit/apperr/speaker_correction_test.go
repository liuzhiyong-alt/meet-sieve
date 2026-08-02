package apperr_test

import (
	"testing"

	"meet-sieve/internal/infra/apperr"
)

// TestSpeakerCorrectionCodes_ProvideStableSemantics 验证 Step 5 错误码、分类和重试语义固定。
func TestSpeakerCorrectionCodes_ProvideStableSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code      apperr.Code
		want      string
		kind      apperr.Kind
		retryable bool
	}{
		{apperr.CodeSpeakerModelUnavailable, "SPEAKER_MODEL_UNAVAILABLE", apperr.KindDependency, true},
		{apperr.CodeSpeakerProfileMissing, "SPEAKER_PROFILE_MISSING", apperr.KindBusiness, false},
		{apperr.CodeSpeakerProfileMismatch, "SPEAKER_PROFILE_MISMATCH", apperr.KindBusiness, false},
		{apperr.CodeSpeakerEvidencePending, "SPEAKER_EVIDENCE_PENDING", apperr.KindBusiness, true},
		{apperr.CodeSpeakerEvidenceInsufficient, "SPEAKER_EVIDENCE_INSUFFICIENT", apperr.KindBusiness, false},
		{apperr.CodeSpeakerEmbeddingFailed, "SPEAKER_EMBEDDING_FAILED", apperr.KindDependency, true},
		{apperr.CodeSpeakerProcessingFailed, "SPEAKER_PROCESSING_FAILED", apperr.KindSystem, true},
		{apperr.CodeCorrectionTargetNotFound, "CORRECTION_TARGET_NOT_FOUND", apperr.KindBusiness, false},
		{apperr.CodeCorrectionMeetingStateInvalid, "CORRECTION_MEETING_STATE_INVALID", apperr.KindBusiness, false},
		{apperr.CodeCorrectionRevisionConflict, "CORRECTION_REVISION_CONFLICT", apperr.KindBusiness, true},
		{apperr.CodeCorrectionIdempotencyConflict, "CORRECTION_IDEMPOTENCY_CONFLICT", apperr.KindBusiness, false},
		{apperr.CodeCorrectionTextInvalid, "CORRECTION_TEXT_INVALID", apperr.KindValidation, false},
		{apperr.CodeAudioClipUnavailable, "AUDIO_CLIP_UNAVAILABLE", apperr.KindBusiness, false},
		{apperr.CodeAudioClipExpired, "AUDIO_CLIP_EXPIRED", apperr.KindBusiness, true},
		{apperr.CodeVoiceMeetingClipRejected, "VOICE_MEETING_CLIP_REJECTED", apperr.KindBusiness, true},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		if test.code.ErrorCode != test.want || test.code.Kind != test.kind ||
			test.code.Retryable != test.retryable || test.code.Message == "" {
			t.Errorf("Step 5 错误码不正确：got=%+v want=%s kind=%s retryable=%t", test.code, test.want, test.kind, test.retryable)
		}
		if _, exists := seen[test.code.ErrorCode]; exists {
			t.Errorf("Step 5 错误码重复：%s", test.code.ErrorCode)
		}
		seen[test.code.ErrorCode] = struct{}{}
	}
}
