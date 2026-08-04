package apperr_test

import (
	"testing"

	"meet-sieve/internal/infra/apperr"
)

// TestStep9Codes_ProvideStableSafeSemantics 验证 Step 9 错误码完整且唯一。
func TestStep9Codes_ProvideStableSafeSemantics(t *testing.T) {
	tests := []apperr.Code{
		apperr.CodeQueryCursorInvalid, apperr.CodeQueryCursorFilterChanged, apperr.CodeMeetingNotFound,
		apperr.CodeMeetingMaintenanceLocked, apperr.CodeRecoveryNotAllowed, apperr.CodeDeleteTaskStopTimeout,
		apperr.CodeDeletePreviewStale, apperr.CodeDeleteManifestInvalid, apperr.CodeDeletePathOutsideMeeting,
		apperr.CodeDeleteSpecialFileBlocked, apperr.CodeDeleteItemBusy, apperr.CodeDeleteInterrupted,
		apperr.CodeDeletePersistTimeout, apperr.CodeStorageScanRunning, apperr.CodeStorageScanFailed,
		apperr.CodeDiagnosticTargetInvalid, apperr.CodeDiagnosticExportFailed, apperr.CodeResourceMissing,
		apperr.CodeResourceChanged, apperr.CodeResourceOutsideWorkspace, apperr.CodeResourceOpenFailed,
		apperr.CodePeopleMemberReferenced, apperr.CodePeopleRevisionConflict,
	}
	seen := make(map[string]struct{}, len(tests))
	for _, code := range tests {
		if code.ErrorCode == "" || code.Message == "" || code.Value < 400 {
			t.Fatalf("Step 9 错误码不完整：%+v", code)
		}
		if _, exists := seen[code.ErrorCode]; exists {
			t.Fatalf("Step 9 错误码重复：%s", code.ErrorCode)
		}
		seen[code.ErrorCode] = struct{}{}
	}
}
