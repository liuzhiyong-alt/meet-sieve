package apperr_test

import (
	"errors"
	"strings"
	"testing"

	"meet-sieve/internal/infra/apperr"
)

// TestAgentCodes_ProvideStableSafeSemantics 验证 Step 7 错误码覆盖完整且不泄漏内部内容。
func TestAgentCodes_ProvideStableSafeSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code      apperr.Code
		want      string
		kind      apperr.Kind
		retryable bool
	}{
		{apperr.CodeAgentExecutableInvalid, "AGENT_EXECUTABLE_INVALID", apperr.KindBusiness, true},
		{apperr.CodeAgentRuntimeMissing, "AGENT_RUNTIME_MISSING", apperr.KindDependency, true},
		{apperr.CodeAgentLaunchFailed, "AGENT_LAUNCH_FAILED", apperr.KindDependency, true},
		{apperr.CodeAgentNotLoggedIn, "AGENT_NOT_LOGGED_IN", apperr.KindDependency, true},
		{apperr.CodeAgentProtocolIncompatible, "AGENT_PROTOCOL_INCOMPATIBLE", apperr.KindDependency, false},
		{apperr.CodeAgentApprovalUnsupported, "AGENT_APPROVAL_UNSUPPORTED", apperr.KindDependency, false},
		{apperr.CodeAgentApprovalExpired, "AGENT_APPROVAL_EXPIRED", apperr.KindBusiness, false},
		{apperr.CodeAgentInitializeFailed, "AGENT_INITIALIZE_FAILED", apperr.KindDependency, true},
		{apperr.CodeAgentBusy, "AGENT_BUSY", apperr.KindBusiness, true},
		{apperr.CodeAgentQuestionInvalid, "AGENT_QUESTION_INVALID", apperr.KindValidation, false},
		{apperr.CodeAgentWakeWordInvalid, "AGENT_WAKE_WORD_INVALID", apperr.KindValidation, false},
		{apperr.CodeAgentProxyPortInvalid, "AGENT_PROXY_PORT_INVALID", apperr.KindValidation, false},
		{apperr.CodeAgentTurnTimeout, "AGENT_TURN_TIMEOUT", apperr.KindDependency, true},
		{apperr.CodeAgentTurnCancelled, "AGENT_TURN_CANCELLED", apperr.KindCanceled, true},
		{apperr.CodeAgentOutputInvalid, "AGENT_OUTPUT_INVALID", apperr.KindDependency, true},
		{apperr.CodeAgentContextFlushFailed, "AGENT_CONTEXT_FLUSH_FAILED", apperr.KindSystem, true},
		{apperr.CodeAgentThreadNotFound, "AGENT_THREAD_NOT_FOUND", apperr.KindDependency, true},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, test := range tests {
		if test.code.ErrorCode != test.want || test.code.Kind != test.kind ||
			test.code.Retryable != test.retryable || test.code.Message == "" {
			t.Errorf("Step 7 错误码不正确：got=%+v want=%s kind=%s retryable=%t", test.code, test.want, test.kind, test.retryable)
		}
		if _, exists := seen[test.code.ErrorCode]; exists {
			t.Errorf("Step 7 错误码重复：%s", test.code.ErrorCode)
		}
		seen[test.code.ErrorCode] = struct{}{}
	}

	cause := errors.New("question=secret answer=secret /private/meeting tool_args=secret")
	result := apperr.Dependency(apperr.CodeAgentOutputInvalid, cause)
	if strings.Contains(result.Message, "secret") || strings.Contains(result.Message, "/private/") {
		t.Fatalf("用户文案泄漏内部内容：%q", result.Message)
	}
	if !errors.Is(result, cause) {
		t.Fatal("内部 cause 必须保留用于排障")
	}
}
