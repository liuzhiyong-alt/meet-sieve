package codex_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"meet-sieve/internal/adapter/agent/codex"
)

type schemaMetadata struct {
	CodexVersion string            `json:"codex_version"`
	Methods      []string          `json:"methods"`
	Files        map[string]string `json:"files"`
}

// TestGeneratedSchema_HashesMatchMetadata 验证握手所需 schema 与已核验 Codex 版本保持一致。
func TestGeneratedSchema_HashesMatchMetadata(t *testing.T) {
	t.Parallel()

	contractDir := filepath.Join(projectRoot(t), "tests", "contract", "codex")
	data, err := os.ReadFile(filepath.Join(contractDir, "metadata.json"))
	if err != nil {
		t.Fatalf("读取 Codex metadata 失败：%v", err)
	}
	var metadata schemaMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("解析 Codex metadata 失败：%v", err)
	}
	if metadata.CodexVersion == "" || len(metadata.Methods) < 8 {
		t.Fatalf("Codex metadata 缺少版本或 method：%+v", metadata)
	}
	contract := codex.RequiredSchemaContract()
	if metadata.CodexVersion != contract.Version || len(metadata.Files) != len(contract.Files) {
		t.Fatalf("metadata 与运行时必要契约不一致：metadata=%d runtime=%d", len(metadata.Files), len(contract.Files))
	}

	for relativePath, expected := range metadata.Files {
		content, err := os.ReadFile(filepath.Join(contractDir, relativePath))
		if err != nil {
			t.Fatalf("读取 schema %s 失败：%v", relativePath, err)
		}
		digest := sha256.Sum256(content)
		actual := hex.EncodeToString(digest[:])
		if actual != expected {
			t.Fatalf("schema %s 哈希漂移：got %s, want %s", relativePath, actual, expected)
		}
		runtimeExpected, exists := contract.Files[filepath.ToSlash(strings.TrimPrefix(relativePath, "schema/"))]
		if !exists || runtimeExpected != expected {
			t.Fatalf("schema %s 未进入运行时必要契约或哈希不一致", relativePath)
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
