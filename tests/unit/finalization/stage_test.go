package finalization_test

import (
	"testing"

	"meet-sieve/internal/domain/finalization"
)

// TestStageValid_OnlyAcceptsDeclaredStages 验证核心收尾阶段使用封闭枚举。
func TestStageValid_OnlyAcceptsDeclaredStages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stage finalization.Stage
		want  bool
	}{
		{name: "停止 LAN", stage: finalization.StageStopLAN, want: true},
		{name: "停止采集", stage: finalization.StageStopCapture, want: true},
		{name: "等待尾部 final", stage: finalization.StageWaitTailFinal, want: true},
		{name: "持久化转写", stage: finalization.StagePersistTranscript, want: true},
		{name: "合并录音", stage: finalization.StageMergeRecording, want: true},
		{name: "刷新原始记录", stage: finalization.StageFlushRawRecord, want: true},
		{name: "提交本地保存", stage: finalization.StageCommitLocalSaved, want: true},
		{name: "拒绝未知阶段", stage: finalization.Stage("done"), want: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.stage.Valid(); got != test.want {
				t.Fatalf("阶段校验错误：got %t, want %t", got, test.want)
			}
		})
	}
}
