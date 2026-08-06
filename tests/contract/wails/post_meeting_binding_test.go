package wails_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	wailstransport "meet-sieve/internal/transport/wails"
)

// TestStep8BindingsExposeFrozenMethods 验证技术方案第 18 节命令均由窄 binding 暴露。
func TestStep8BindingsExposeFrozenMethods(t *testing.T) {
	t.Parallel()
	tests := []struct {
		typeOf  reflect.Type
		methods []string
	}{
		{reflect.TypeOf((*wailstransport.FinalizationBinding)(nil)), []string{"GetFinalizationState", "RetryFinalization", "RetryAgentFinalSync"}},
		{reflect.TypeOf((*wailstransport.GapBinding)(nil)), []string{"GetGapState", "StartGapCompensation", "StopGapCompensation", "RetryGapCompensation", "GetGapConflict", "ResolveGapConflict"}},
		{reflect.TypeOf((*wailstransport.MinutesBinding)(nil)), []string{"GetMinutesState", "GetMinutesSettings", "SaveMinutesSettings", "GenerateMinutes", "StopMinutesGeneration", "SaveMinuteDraft", "ConfirmMinute", "ListMinuteVersions", "RestoreMinuteVersion"}},
	}
	for _, test := range tests {
		for _, name := range test.methods {
			if _, found := test.typeOf.MethodByName(name); !found {
				t.Fatalf("Step 8 binding 缺少方法 %s.%s", test.typeOf, name)
			}
		}
	}
}

// TestStep8DTOsDoNotExposePathsOrProviderInternals 验证 DTO 字段不提供危险输入输出通道。
func TestStep8DTOsDoNotExposePathsOrProviderInternals(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(struct {
		Gap     wailstransport.GapConflictDTO  `json:"gap"`
		Minutes wailstransport.MinutesStateDTO `json:"minutes"`
	}{})
	if err != nil {
		t.Fatalf("序列化 Step 8 DTO 失败：%v", err)
	}
	for _, forbidden := range []string{"audio_path", "relative_path", "working_directory", "provider_header", "logid", "access_token", "api_key"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Step 8 DTO 泄漏禁止字段 %q：%s", forbidden, encoded)
		}
	}
}
