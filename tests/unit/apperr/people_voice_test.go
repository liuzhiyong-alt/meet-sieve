package apperr_test

import (
	"testing"

	"meet-sieve/internal/infra/apperr"
)

// TestCodeMemberNotFound_ProvidesStableBusinessSemantics 验证成员缺失具有稳定且安全的对外错误语义。
func TestCodeMemberNotFound_ProvidesStableBusinessSemantics(t *testing.T) {
	err := apperr.Biz(apperr.CodeMemberNotFound)
	if err.ErrorCode != "MEMBER_NOT_FOUND" {
		t.Fatalf("错误码不正确：%q", err.ErrorCode)
	}
	if err.Kind != apperr.KindBusiness || err.Retryable {
		t.Fatalf("成员缺失错误分类不正确：%+v", err)
	}
	if err.Message == "" {
		t.Fatal("成员缺失必须有安全用户提示")
	}
}

// TestVoiceCodes_ProvideStableSemantics 验证 Step 2 声纹边界错误码完整且重试语义固定。
func TestVoiceCodes_ProvideStableSemantics(t *testing.T) {
	tests := []struct {
		code      apperr.Code
		want      string
		retryable bool
	}{
		{apperr.CodeVoiceRecordingBusy, "VOICE_RECORDING_BUSY", true},
		{apperr.CodeVoiceDeviceUnavailable, "VOICE_DEVICE_UNAVAILABLE", true},
		{apperr.CodeVoicePermissionDenied, "VOICE_PERMISSION_DENIED", true},
		{apperr.CodeVoiceWAVInvalid, "VOICE_WAV_INVALID", false},
		{apperr.CodeVoiceDurationExceeded, "VOICE_DURATION_EXCEEDED", false},
		{apperr.CodeVoiceQualityRejected, "VOICE_QUALITY_REJECTED", true},
		{apperr.CodeVoiceModelUnavailable, "VOICE_MODEL_UNAVAILABLE", true},
		{apperr.CodeVoiceEmbeddingFailed, "VOICE_EMBEDDING_FAILED", true},
		{apperr.CodeVoiceSampleFileInvalid, "VOICE_SAMPLE_FILE_INVALID", false},
		{apperr.CodeVoiceSampleDeleteFailed, "VOICE_SAMPLE_DELETE_FAILED", true},
		{apperr.CodeVoiceRebuildIncomplete, "VOICE_REBUILD_INCOMPLETE", true},
	}
	for _, test := range tests {
		if test.code.ErrorCode != test.want || test.code.Retryable != test.retryable || test.code.Message == "" {
			t.Errorf("声纹错误码不正确：got=%+v want=%s retryable=%t", test.code, test.want, test.retryable)
		}
	}
}
