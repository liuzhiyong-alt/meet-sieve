package codex_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/adapter/agent/codex"
	"meet-sieve/internal/infra/apperr"
)

type schemaRunner struct {
	version       string
	files         map[string][]byte
	generateErr   error
	generateCount int
	lastOutput    string
}

// Version 返回测试控制的 provider 版本。
func (runner *schemaRunner) Version(context.Context, string) (string, error) {
	return runner.version, nil
}

// Generate 把测试 schema 写入 verifier 提供的一次性目录。
func (runner *schemaRunner) Generate(_ context.Context, _ string, outputDirectory string) error {
	runner.generateCount++
	runner.lastOutput = outputDirectory
	if runner.generateErr != nil {
		return runner.generateErr
	}
	for relativePath, content := range runner.files {
		path := filepath.Join(outputDirectory, relativePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// TestSchemaVerifier_CachesByExecutableVersionAndMtime 验证相同身份只生成一次，任一身份变化都会重验。
func TestSchemaVerifier_CachesByExecutableVersionAndMtime(t *testing.T) {
	t.Parallel()

	tempRoot := t.TempDir()
	executable := filepath.Join(tempRoot, "codex")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatalf("创建测试 executable 失败：%v", err)
	}
	content := []byte(`{"type":"object"}`)
	runner := &schemaRunner{version: "codex-cli test", files: map[string][]byte{"v2/TurnStartParams.json": content}}
	verifier := codex.NewSchemaVerifier(runner, contractFor("v2/TurnStartParams.json", content), tempRoot)

	if err := verifier.Verify(context.Background(), executable); err != nil {
		t.Fatalf("首次校验失败：%v", err)
	}
	if err := verifier.Verify(context.Background(), executable); err != nil {
		t.Fatalf("缓存校验失败：%v", err)
	}
	if runner.generateCount != 1 {
		t.Fatalf("相同身份重复生成 schema：%d", runner.generateCount)
	}

	runner.version = "codex-cli changed"
	if err := verifier.Verify(context.Background(), executable); err != nil {
		t.Fatalf("版本变化后校验失败：%v", err)
	}
	changed := time.Now().Add(time.Second)
	if err := os.Chtimes(executable, changed, changed); err != nil {
		t.Fatalf("修改 executable mtime 失败：%v", err)
	}
	if err := verifier.Verify(context.Background(), executable); err != nil {
		t.Fatalf("mtime 变化后校验失败：%v", err)
	}
	if runner.generateCount != 3 {
		t.Fatalf("身份变化没有重新生成：%d", runner.generateCount)
	}
	if _, err := os.Stat(runner.lastOutput); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("临时 schema 目录未清理：%v", err)
	}
}

// TestSchemaVerifier_FailsClosedOnMissingInvalidOrDrift 验证缺失、非法 JSON 和哈希漂移均关闭能力。
func TestSchemaVerifier_FailsClosedOnMissingInvalidOrDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    map[string][]byte
		contract codex.SchemaContract
	}{
		{name: "missing", files: map[string][]byte{}, contract: contractFor("required.json", []byte(`{}`))},
		{name: "invalid json", files: map[string][]byte{"required.json": []byte(`{`)}, contract: contractFor("required.json", []byte(`{`))},
		{name: "hash drift", files: map[string][]byte{"required.json": []byte(`{"changed":true}`)}, contract: contractFor("required.json", []byte(`{}`))},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			executable := filepath.Join(root, "codex")
			if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
				t.Fatalf("创建测试 executable 失败：%v", err)
			}
			runner := &schemaRunner{version: "codex-cli test", files: test.files}
			verifier := codex.NewSchemaVerifier(runner, test.contract, root)
			err := verifier.Verify(context.Background(), executable)
			if err == nil {
				t.Fatal("不兼容 schema 不应通过")
			}
			if normalized := apperr.Normalize(err); normalized.ErrorCode != apperr.CodeAgentProtocolIncompatible.ErrorCode {
				t.Fatalf("错误没有 fail closed：%+v", normalized)
			}
			if _, statErr := os.Stat(runner.lastOutput); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("失败后临时目录未清理：%v", statErr)
			}
		})
	}
}

// TestSchemaVerifier_IgnoresNonRequiredSchemas 验证新增非必要 schema 不会误判协议不兼容。
func TestSchemaVerifier_IgnoresNonRequiredSchemas(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	executable := filepath.Join(root, "codex")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatalf("创建测试 executable 失败：%v", err)
	}
	required := []byte(`{"type":"object"}`)
	runner := &schemaRunner{version: "codex-cli test", files: map[string][]byte{
		"required.json": required,
		"future.json":   []byte(`{"new":true}`),
	}}
	verifier := codex.NewSchemaVerifier(runner, contractFor("required.json", required), root)
	if err := verifier.Verify(context.Background(), executable); err != nil {
		t.Fatalf("非必要 schema 不应导致失败：%v", err)
	}
}

// contractFor 为单个测试文件构造严格哈希契约。
func contractFor(path string, content []byte) codex.SchemaContract {
	digest := sha256.Sum256(content)
	return codex.SchemaContract{Version: "test", Files: map[string]string{path: hex.EncodeToString(digest[:])}}
}
