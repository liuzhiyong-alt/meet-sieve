package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRaceTargetIncludesStep5ConcurrencyPackages 验证 Step 5 的并发服务不会漏过 race 门禁。
func TestRaceTargetIncludesStep5ConcurrencyPackages(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("读取 Makefile 失败：%v", err)
	}

	makefile := string(content)
	for _, packagePath := range []string{
		"./internal/domain/speaker",
		"./internal/domain/correction",
		"./internal/service/speaker",
		"./internal/service/correction",
	} {
		if !strings.Contains(makefile, packagePath) {
			t.Errorf("test-race 必须覆盖 %s", packagePath)
		}
	}
}
