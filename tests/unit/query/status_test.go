package query_test

import (
	"testing"

	querydomain "meet-sieve/internal/domain/query"
)

// TestHighestPriorityStatus_UsesConfirmedOrder 验证 Step 9 状态优先级不会被页面各自推导。
func TestHighestPriorityStatus_UsesConfirmedOrder(t *testing.T) {
	facts := querydomain.MeetingStatusFacts{
		Deleting: true, LocalSaveFailed: true, GapConflict: true, GapProcessing: true,
		MinuteCandidate: true, AgentUnsynced: true, MinuteConfirmed: true, LocalSaved: true,
	}
	if got := querydomain.HighestPriorityStatus(facts); got != querydomain.StatusDeleting {
		t.Fatalf("删除状态必须最高优先：got %q", got)
	}
	facts.Deleting = false
	if got := querydomain.HighestPriorityStatus(facts); got != querydomain.StatusRecoveryRequired {
		t.Fatalf("本地恢复状态优先级错误：got %q", got)
	}
	facts.LocalSaveFailed = false
	if got := querydomain.HighestPriorityStatus(facts); got != querydomain.StatusGapConflict {
		t.Fatalf("缺口冲突优先级错误：got %q", got)
	}
}
