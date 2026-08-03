package gap_test

import (
	"reflect"
	"testing"

	"meet-sieve/internal/domain/gap"
)

// TestPlan_SortsMergesTouchesAndAddsBoundedContext 验证 gap 排序、接触合并和上下文裁剪。
func TestPlan_SortsMergesTouchesAndAddsBoundedContext(t *testing.T) {
	t.Parallel()

	result, err := gap.Plan(gap.PlanInput{
		Gaps: []gap.Range{
			{ID: "later", StartSample: 20000, EndSample: 24000},
			{ID: "first", StartSample: 0, EndSample: 10000},
			{ID: "touch", StartSample: 10000, EndSample: 12000},
		},
		RecordingEnd: 30000, SampleRate: gap.DefaultSampleRate,
	})
	if err != nil {
		t.Fatalf("生成 gap 计划失败：%v", err)
	}
	want := []gap.Slice{
		{GapIDs: []string{"first", "touch"}, CoreStartSample: 0, CoreEndSample: 12000, AudioStartSample: 0, AudioEndSample: 20000},
		{GapIDs: []string{"later"}, CoreStartSample: 20000, CoreEndSample: 24000, AudioStartSample: 12000, AudioEndSample: 30000},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("gap 计划错误：got=%#v want=%#v", result, want)
	}
}

// TestPlan_SplitsAtDurationOrByteLimit 验证 9 分钟与 WAV 字节上限任一先到即拆分。
func TestPlan_SplitsAtDurationOrByteLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		end         int64
		maxBytes    int64
		wantSlices  int
		maxCoreSize int64
	}{
		{name: "duration", end: gap.DefaultMaxDurationSamples + 1, wantSlices: 2, maxCoreSize: gap.DefaultMaxDurationSamples},
		{name: "bytes", end: 20000, maxBytes: 44 + 10000*2, wantSlices: 2, maxCoreSize: 10000},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := gap.Plan(gap.PlanInput{
				Gaps:         []gap.Range{{ID: "gap", StartSample: 0, EndSample: test.end}},
				RecordingEnd: test.end, SampleRate: gap.DefaultSampleRate, MaxWAVBytes: test.maxBytes,
			})
			if err != nil {
				t.Fatalf("生成拆分计划失败：%v", err)
			}
			if len(result) != test.wantSlices {
				t.Fatalf("拆分数量错误：got=%d want=%d", len(result), test.wantSlices)
			}
			for _, item := range result {
				if item.CoreEndSample-item.CoreStartSample > test.maxCoreSize {
					t.Fatalf("核心范围超过上限：%#v", item)
				}
				if item.AudioEndSample-item.AudioStartSample > test.maxCoreSize {
					t.Fatalf("包含上下文的 WAV 超过上限：%#v", item)
				}
			}
			if result[0].CoreEndSample != result[1].CoreStartSample {
				t.Fatalf("拆分核心范围不连续：%#v", result)
			}
		})
	}
}

// TestPlan_RejectsEmptyAndOutOfBoundsRanges 验证空范围和越界范围不会形成 provider 请求。
func TestPlan_RejectsEmptyAndOutOfBoundsRanges(t *testing.T) {
	t.Parallel()

	for _, input := range []gap.PlanInput{
		{Gaps: []gap.Range{{ID: "empty", StartSample: 1, EndSample: 1}}, RecordingEnd: 10, SampleRate: 16000},
		{Gaps: []gap.Range{{ID: "outside", StartSample: 0, EndSample: 11}}, RecordingEnd: 10, SampleRate: 16000},
		{Gaps: []gap.Range{{ID: "missing", StartSample: 0, EndSample: 1}}, RecordingEnd: 10, SampleRate: 0},
	} {
		if _, err := gap.Plan(input); err == nil {
			t.Fatalf("非法范围必须失败：%#v", input)
		}
	}
}

// TestPlan_QuietSplitSearchesOnBothSidesOfTarget 验证目标点后的低能量窗也能成为拆分点。
func TestPlan_QuietSplitSearchesOnBothSidesOfTarget(t *testing.T) {
	t.Parallel()
	maxSamples := int64(10 * gap.DefaultSampleRate)
	pcm := make([]int16, maxSamples+gap.DefaultSampleRate)
	for index := range pcm {
		pcm[index] = 1000
	}
	// 目标点为硬上限前两秒；把最低能量窗放到目标点后一秒。
	quietCenter := int64(9 * gap.DefaultSampleRate)
	quietHalfWindow := int64(gap.DefaultSampleRate / 10)
	for index := quietCenter - quietHalfWindow; index < quietCenter+quietHalfWindow; index++ {
		pcm[index] = 0
	}
	result, err := gap.Plan(gap.PlanInput{
		Gaps:         []gap.Range{{ID: "gap", StartSample: 0, EndSample: int64(len(pcm))}},
		RecordingEnd: int64(len(pcm)), SampleRate: gap.DefaultSampleRate,
		MaxWAVBytes: 44 + maxSamples*2, PCM: pcm,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].CoreEndSample != quietCenter {
		t.Fatalf("应选择目标点后的最低能量窗：%#v", result)
	}
}
