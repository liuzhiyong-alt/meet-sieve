package minutes_test

import (
	"testing"

	"meet-sieve/internal/domain/minutes"
)

// TestVersionEnumsValid_OnlyAcceptDeclaredValues 验证纪要版本来源和状态使用封闭枚举。
func TestVersionEnumsValid_OnlyAcceptDeclaredValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{name: "AI 来源", got: minutes.SourceAI.Valid(), want: true},
		{name: "人工来源", got: minutes.SourceHuman.Valid(), want: true},
		{name: "恢复来源", got: minutes.SourceRestored.Valid(), want: true},
		{name: "非法来源", got: minutes.Source("imported").Valid(), want: false},
		{name: "草稿", got: minutes.StateDraft.Valid(), want: true},
		{name: "已确认", got: minutes.StateConfirmed.Valid(), want: true},
		{name: "非法状态", got: minutes.State("published").Valid(), want: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.got != test.want {
				t.Fatalf("纪要枚举校验错误：got %t, want %t", test.got, test.want)
			}
		})
	}
}
