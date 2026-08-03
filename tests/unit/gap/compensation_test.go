package gap_test

import (
	"reflect"
	"testing"

	"meet-sieve/internal/domain/gap"
	"meet-sieve/internal/port"
)

// TestNormalizeToCore_MapsClipsAndDropsContextOnlySegments 验证候选映射后只保留 core 交集。
func TestNormalizeToCore_MapsClipsAndDropsContextOnlySegments(t *testing.T) {
	t.Parallel()

	got := gap.NormalizeToCore([]port.FileTranscriptionSegment{
		{Text: "前文", StartSample: 0, EndSample: 1000},
		{Text: "跨入", SpeakerID: "1", StartSample: 1500, EndSample: 3000},
		{Text: "内部", SpeakerID: "2", StartSample: 4000, EndSample: 5000},
		{Text: "后文", StartSample: 7000, EndSample: 8000},
	}, 8000, 10000, 15000)
	want := []gap.CandidateSegment{
		{Text: "跨入", SpeakerID: "1", StartSample: 10000, EndSample: 11000},
		{Text: "内部", SpeakerID: "2", StartSample: 12000, EndSample: 13000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("core 裁剪错误：got=%#v want=%#v", got, want)
	}
}

// TestHasPositiveOverlap_DoesNotTreatTouchAsConflict 验证首尾相接不产生冲突。
func TestHasPositiveOverlap_DoesNotTreatTouchAsConflict(t *testing.T) {
	t.Parallel()
	if gap.HasPositiveOverlap(0, 10, 10, 20) {
		t.Fatal("首尾相接不得算作冲突")
	}
	if !gap.HasPositiveOverlap(0, 11, 10, 20) {
		t.Fatal("正长度交集必须算作冲突")
	}
}

// TestAggregateState_UsesStablePriority 验证会议 gap 聚合优先级固定。
func TestAggregateState_UsesStablePriority(t *testing.T) {
	t.Parallel()
	tests := []struct {
		states []gap.State
		want   string
	}{
		{states: nil, want: "none"},
		{states: []gap.State{gap.StateCompleted}, want: "completed"},
		{states: []gap.State{gap.StateCompleted, gap.StatePending}, want: "pending"},
		{states: []gap.State{gap.StatePending, gap.StateFailed}, want: "failed"},
		{states: []gap.State{gap.StateFailed, gap.StateProcessing}, want: "processing"},
		{states: []gap.State{gap.StateProcessing, gap.StateConflict}, want: "conflict"},
	}
	for _, test := range tests {
		if got := gap.AggregateState(test.states); got != test.want {
			t.Fatalf("聚合状态错误：states=%v got=%s want=%s", test.states, got, test.want)
		}
	}
}
