package voice_test

import (
	"testing"

	"meet-sieve/internal/domain/voice"
)

// TestReadiness_RecognizesRegisteredStates 验证成员声纹资料只返回技术方案登记的就绪状态。
func TestReadiness_RecognizesRegisteredStates(t *testing.T) {
	for _, readiness := range []voice.Readiness{
		voice.ReadinessNotEnrolled,
		voice.ReadinessProcessing,
		voice.ReadinessReady,
		voice.ReadinessRebuildRequired,
		voice.ReadinessUnavailable,
	} {
		if !readiness.IsValid() {
			t.Fatalf("登记的 readiness 必须合法：%q", readiness)
		}
	}
}
