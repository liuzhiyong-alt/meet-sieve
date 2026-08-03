package apperr_test

import (
	"testing"

	"meet-sieve/internal/infra/apperr"
)

// TestLANGuestCodes_ProvideStableSemantics 验证 Step 6 错误码、分类和重试语义固定。
func TestLANGuestCodes_ProvideStableSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code      apperr.Code
		want      string
		kind      apperr.Kind
		retryable bool
	}{
		{apperr.CodeLANInterfaceUnavailable, "LAN_INTERFACE_UNAVAILABLE", apperr.KindBusiness, true},
		{apperr.CodeLANStartFailed, "LAN_START_FAILED", apperr.KindSystem, true},
		{apperr.CodeLANGenerationChanged, "LAN_GENERATION_CHANGED", apperr.KindBusiness, true},
		{apperr.CodeLANSessionInvalid, "LAN_SESSION_INVALID", apperr.KindBusiness, false},
		{apperr.CodeLANSessionExpired, "LAN_SESSION_EXPIRED", apperr.KindBusiness, true},
		{apperr.CodeLANMeetingEnded, "LAN_MEETING_ENDED", apperr.KindBusiness, false},
		{apperr.CodeLANRateLimited, "LAN_RATE_LIMITED", apperr.KindBusiness, true},
		{apperr.CodeMessageInvalid, "MESSAGE_INVALID", apperr.KindValidation, false},
		{apperr.CodeLinkInvalid, "LINK_INVALID", apperr.KindValidation, false},
		{apperr.CodeAttachmentTooLarge, "ATTACHMENT_TOO_LARGE", apperr.KindValidation, false},
		{apperr.CodeAttachmentTypeBlocked, "ATTACHMENT_TYPE_BLOCKED", apperr.KindValidation, false},
		{apperr.CodeAttachmentDiskLow, "ATTACHMENT_DISK_LOW", apperr.KindBusiness, true},
		{apperr.CodeAttachmentUploadCancelled, "ATTACHMENT_UPLOAD_CANCELLED", apperr.KindBusiness, true},
		{apperr.CodeAttachmentUploadFailed, "ATTACHMENT_UPLOAD_FAILED", apperr.KindSystem, true},
		{apperr.CodeAttachmentNotFound, "ATTACHMENT_NOT_FOUND", apperr.KindBusiness, false},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		if test.code.ErrorCode != test.want || test.code.Kind != test.kind ||
			test.code.Retryable != test.retryable || test.code.Message == "" {
			t.Errorf("Step 6 错误码不正确：got=%+v want=%s kind=%s retryable=%t", test.code, test.want, test.kind, test.retryable)
		}
		if _, exists := seen[test.code.ErrorCode]; exists {
			t.Errorf("Step 6 错误码重复：%s", test.code.ErrorCode)
		}
		seen[test.code.ErrorCode] = struct{}{}
	}
}
