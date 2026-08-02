package transcript

import "testing"

// TestNewSampleRange 验证 gap 与 final 共用的样本区间拒绝墙上时钟式的非法范围。
func TestNewSampleRange(t *testing.T) {
	tests := []struct {
		name    string
		start   int64
		end     int64
		wantErr bool
	}{
		{name: "合法半开区间", start: 0, end: 16000},
		{name: "起点为负", start: -1, end: 16000, wantErr: true},
		{name: "空区间", start: 16000, end: 16000, wantErr: true},
		{name: "倒序区间", start: 16001, end: 16000, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sampleRange, err := NewSampleRange(test.start, test.end)
			if test.wantErr {
				if err == nil {
					t.Fatal("非法样本区间必须失败")
				}
				return
			}
			if err != nil {
				t.Fatalf("创建合法样本区间失败：%v", err)
			}
			if got := sampleRange.Duration(); got != test.end-test.start {
				t.Fatalf("样本区间长度不正确：got %d want %d", got, test.end-test.start)
			}
		})
	}
}

// TestBuildGapOriginKey 验证相同缺口重复恢复保持幂等，不同原因不能被合并。
func TestBuildGapOriginKey(t *testing.T) {
	sampleRange, err := NewSampleRange(32000, 48000)
	if err != nil {
		t.Fatalf("创建样本区间失败：%v", err)
	}
	first, err := BuildGapOriginKey("meeting-1", GapDisconnected, sampleRange)
	if err != nil {
		t.Fatalf("生成缺口幂等键失败：%v", err)
	}
	second, err := BuildGapOriginKey("meeting-1", GapDisconnected, sampleRange)
	if err != nil {
		t.Fatalf("重复生成缺口幂等键失败：%v", err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("相同 gap 必须生成稳定 64 位幂等键：%q / %q", first, second)
	}
	different, err := BuildGapOriginKey("meeting-1", GapBackpressure, sampleRange)
	if err != nil {
		t.Fatalf("生成不同原因的幂等键失败：%v", err)
	}
	if first == different {
		t.Fatal("不同原因的相同区间必须保留为不同 gap")
	}
}
