package identity_test

import (
	"testing"

	"meet-sieve/internal/infra/identity"

	"github.com/google/uuid"
)

// TestUUIDGenerator_NewReturnsUniqueUUID 验证生产 ID 生成器返回合法且不重复的 UUID。
func TestUUIDGenerator_NewReturnsUniqueUUID(t *testing.T) {
	t.Parallel()

	generator := identity.NewUUIDGenerator()
	first := generator.New()
	second := generator.New()
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("首个 ID 不是 UUID：%v", err)
	}
	if first == second {
		t.Fatal("连续生成的 UUID 不应相同")
	}
}

// TestFixedGenerator_NewReturnsConfiguredSequence 验证固定生成器按顺序提供确定性 ID。
func TestFixedGenerator_NewReturnsConfiguredSequence(t *testing.T) {
	t.Parallel()

	generator := identity.NewFixedGenerator("meeting-1", "meeting-2")
	if actual := generator.New(); actual != "meeting-1" {
		t.Fatalf("首个固定 ID 不一致：got %q", actual)
	}
	if actual := generator.New(); actual != "meeting-2" {
		t.Fatalf("第二个固定 ID 不一致：got %q", actual)
	}
	if actual := generator.New(); actual != "" {
		t.Fatalf("序列耗尽后应返回空字符串：got %q", actual)
	}
}
