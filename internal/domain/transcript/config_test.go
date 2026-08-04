package transcript

import "testing"

// TestAuthModeAndRuntimeConfig 验证已冻结的 transport 与运行时容量不允许隐式降级。
func TestAuthModeAndRuntimeConfig(t *testing.T) {
	config := RuntimeConfig{Endpoint: "wss://example.invalid", ResourceID: "resource", PCMQueueSamples: 32000, FinalQueueCapacity: 128, FinalPersistTimeoutMS: 5000, TailTimeoutMS: 15000, ReconnectBackoffMS: []int64{1000, 2000, 4000, 8000, 15000}}
	if err := config.Validate(); err != nil {
		t.Fatalf("冻结配置应通过：%v", err)
	}
	if _, err := AuthMode("unknown").Transport(); err == nil {
		t.Fatal("未知鉴权方式不得猜测 transport")
	}
	transport, err := AuthModeAPIKey.Transport()
	if err != nil || transport != TransportSeedV1 {
		t.Fatalf("APP Key 应使用已验证的 Seed transport：transport=%s err=%v", transport, err)
	}
	if _, err = AuthModeLegacy.Transport(); err == nil {
		t.Fatal("旧版鉴权不得继续进入实时转写运行时")
	}
}

// TestProviderMillisecondsToSample 验证 provider 时间只按固定采样率映射且越界被拒绝。
func TestProviderMillisecondsToSample(t *testing.T) {
	sample, err := ProviderMillisecondsToSample(32000, 1500, 64000)
	if err != nil || sample != 56000 {
		t.Fatalf("毫秒映射错误：sample=%d err=%v", sample, err)
	}
	if _, err = ProviderMillisecondsToSample(32000, 3000, 64000); err == nil {
		t.Fatal("越过 last_sent_sample 的时间不得裁剪")
	}
}
