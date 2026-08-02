package app_test

import (
	"testing"

	application "meet-sieve/internal/app"
)

// TestFoundationDependencies_Linked 验证高风险基础适配器进入同一个应用装配。
func TestFoundationDependencies_Linked(t *testing.T) {
	t.Parallel()

	if !application.NewFoundationDependencies().Linked() {
		t.Fatal("基础依赖装配不完整")
	}
}
