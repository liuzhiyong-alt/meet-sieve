package port_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var forbiddenImports = []string{
	"github.com/gin-gonic/gin",
	"github.com/wailsapp/wails",
	"gorm.io/",
	"github.com/gen2brain/malgo",
	"github.com/yalue/onnxruntime_go",
}

// TestPortPackage_DoesNotDependOnFrameworks 验证 Port 只依赖标准库和稳定业务类型。
func TestPortPackage_DoesNotDependOnFrameworks(t *testing.T) {
	t.Parallel()

	portDir := filepath.Join(projectRoot(t), "internal", "port")
	files, err := filepath.Glob(filepath.Join(portDir, "*.go"))
	if err != nil {
		t.Fatalf("查找 Port 文件失败：%v", err)
	}
	if len(files) == 0 {
		t.Fatal("未找到 Port 文件")
	}

	for _, path := range files {
		checkPortImports(t, path)
	}
}

func checkPortImports(t *testing.T, path string) {
	t.Helper()

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 Port 文件失败：%v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析 Port 文件失败：%v", err)
	}
	for _, imported := range file.Imports {
		value := strings.Trim(imported.Path.Value, `"`)
		for _, forbidden := range forbiddenImports {
			if strings.Contains(value, forbidden) {
				t.Fatalf("%s 引用了禁止依赖 %s", path, value)
			}
		}
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()

	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("读取当前目录失败：%v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("未找到项目根目录")
		}
		current = parent
	}
}
