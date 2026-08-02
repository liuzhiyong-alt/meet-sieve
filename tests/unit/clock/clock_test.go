package clock_test

import (
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
)

// TestFixed_NowReturnsConfiguredTime 验证固定时钟可为测试提供确定性时间。
func TestFixed_NowReturnsConfiguredTime(t *testing.T) {
	t.Parallel()

	expected := time.Date(2026, 7, 30, 20, 30, 0, 0, time.UTC)
	if actual := clock.NewFixed(expected).Now(); !actual.Equal(expected) {
		t.Fatalf("固定时间不一致：got %s, want %s", actual, expected)
	}
}

// TestSystem_NowReturnsCurrentTime 验证系统时钟返回调用区间内的真实时间。
func TestSystem_NowReturnsCurrentTime(t *testing.T) {
	t.Parallel()

	before := time.Now()
	actual := clock.NewSystem().Now()
	after := time.Now()
	if actual.Before(before) || actual.After(after) {
		t.Fatalf("系统时间不在调用区间内：got %s", actual)
	}
}
