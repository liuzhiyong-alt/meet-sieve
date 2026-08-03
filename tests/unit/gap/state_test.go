package gap_test

import (
	"testing"

	"meet-sieve/internal/domain/gap"
)

// TestEnumsValid_OnlyAcceptDeclaredValues 验证缺口、尝试和解决动作使用封闭枚举。
func TestEnumsValid_OnlyAcceptDeclaredValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{name: "gap pending", got: gap.StatePending.Valid(), want: true},
		{name: "gap processing", got: gap.StateProcessing.Valid(), want: true},
		{name: "gap completed", got: gap.StateCompleted.Valid(), want: true},
		{name: "gap failed", got: gap.StateFailed.Valid(), want: true},
		{name: "gap conflict", got: gap.StateConflict.Valid(), want: true},
		{name: "gap invalid", got: gap.State("cancelled").Valid(), want: false},
		{name: "attempt pending", got: gap.AttemptPending.Valid(), want: true},
		{name: "attempt running", got: gap.AttemptRunning.Valid(), want: true},
		{name: "attempt completed", got: gap.AttemptCompleted.Valid(), want: true},
		{name: "attempt failed", got: gap.AttemptFailed.Valid(), want: true},
		{name: "attempt conflict", got: gap.AttemptConflict.Valid(), want: true},
		{name: "attempt cancelled", got: gap.AttemptCancelled.Valid(), want: true},
		{name: "attempt invalid", got: gap.AttemptState("processing").Valid(), want: false},
		{name: "keep existing", got: gap.ResolutionKeepExisting.Valid(), want: true},
		{name: "use file text", got: gap.ResolutionUseFileText.Valid(), want: true},
		{name: "save manual text", got: gap.ResolutionSaveManualText.Valid(), want: true},
		{name: "resolution invalid", got: gap.Resolution("merge").Valid(), want: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.got != test.want {
				t.Fatalf("枚举校验错误：got %t, want %t", test.got, test.want)
			}
		})
	}
}
