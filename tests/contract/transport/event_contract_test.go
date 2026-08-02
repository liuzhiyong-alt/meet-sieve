package transport_test

import (
	"encoding/json"
	"testing"
	"time"

	wailstransport "meet-sieve/internal/transport/wails"
)

// TestNewEvent_UsesVersionOneEnvelope 验证事件 envelope 使用稳定版本和毫秒时间戳。
func TestNewEvent_UsesVersionOneEnvelope(t *testing.T) {
	t.Parallel()

	occurredAt := time.UnixMilli(1_754_000_000_123)
	event := wailstransport.NewEvent("system.health.changed", occurredAt, uint64(8), map[string]string{"status": "ready"})

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("反序列化失败：%v", err)
	}
	if payload["version"] != float64(1) {
		t.Fatalf("事件版本不正确：got %#v", payload["version"])
	}
	if payload["occurredAt"] != float64(occurredAt.UnixMilli()) {
		t.Fatalf("事件时间不正确：got %#v", payload["occurredAt"])
	}
	if payload["sequence"] != float64(8) {
		t.Fatalf("事件序号不正确：got %#v", payload["sequence"])
	}
}
