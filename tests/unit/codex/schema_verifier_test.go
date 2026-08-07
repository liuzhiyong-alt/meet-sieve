package codex_test

import (
	"context"
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
	versionErr    error
	files         map[string][]byte
	generateErr   error
	generateCount int
	lastOutput    string
}

// Version 返回测试控制的 provider 版本。
func (runner *schemaRunner) Version(context.Context, codex.LaunchSpec) (string, error) {
	return runner.version, runner.versionErr
}

// TestSchemaVerifier_PreservesCancellationAndTimeout 验证进程取消和超时不会伪装成协议不兼容。
func TestSchemaVerifier_PreservesCancellationAndTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cause     error
		errorCode string
	}{
		{name: "canceled", cause: context.Canceled, errorCode: apperr.CodeCanceled.ErrorCode},
		{name: "timeout", cause: context.DeadlineExceeded, errorCode: apperr.CodeDependencyTimeout.ErrorCode},
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
			verifier := codex.NewSchemaVerifier(&schemaRunner{versionErr: test.cause}, contractFor("required.json", []byte(`{}`)), root)
			err := verifier.Verify(context.Background(), testLaunchSpec(executable))
			if normalized := apperr.Normalize(err); normalized.ErrorCode != test.errorCode {
				t.Fatalf("Schema 命令错误分类错误：got=%s want=%s err=%v", normalized.ErrorCode, test.errorCode, err)
			}
		})
	}
}

// TestSchemaVerifier_PreservesClassifiedLaunchError 验证启动环境错误不会被覆盖成协议不兼容。
func TestSchemaVerifier_PreservesClassifiedLaunchError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	executable := filepath.Join(root, "codex")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatalf("创建测试 executable 失败：%v", err)
	}
	runtimeErr := apperr.Dependency(
		apperr.CodeAgentRuntimeMissing,
		errors.New("node missing"),
		apperr.WithOp("agent.launch.runtime"),
	)
	verifier := codex.NewSchemaVerifier(&schemaRunner{versionErr: runtimeErr}, contractFor("required.json", []byte(`{}`)), root)
	err := verifier.Verify(context.Background(), testLaunchSpec(executable))
	if normalized := apperr.Normalize(err); normalized.ErrorCode != apperr.CodeAgentRuntimeMissing.ErrorCode {
		t.Fatalf("已分类启动错误被错误覆盖：got=%s err=%v", normalized.ErrorCode, err)
	}
}

// TestSchemaVerifier_MapsCommandFailureToLaunchError 验证命令异常退出与 schema 语义漂移使用不同错误码。
func TestSchemaVerifier_MapsCommandFailureToLaunchError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	executable := filepath.Join(root, "codex")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatalf("创建测试 executable 失败：%v", err)
	}
	verifier := codex.NewSchemaVerifier(
		&schemaRunner{version: "codex-cli test", generateErr: errors.New("process exited")},
		contractFor("required.json", []byte(`{}`)),
		root,
	)
	err := verifier.Verify(context.Background(), testLaunchSpec(executable))
	if normalized := apperr.Normalize(err); normalized.ErrorCode != apperr.CodeAgentLaunchFailed.ErrorCode {
		t.Fatalf("命令失败错误码不正确：got=%s err=%v", normalized.ErrorCode, err)
	}
}

// Generate 把测试 schema 写入 verifier 提供的一次性目录。
func (runner *schemaRunner) Generate(_ context.Context, _ codex.LaunchSpec, outputDirectory string) error {
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

	if err := verifier.Verify(context.Background(), testLaunchSpec(executable)); err != nil {
		t.Fatalf("首次校验失败：%v", err)
	}
	if err := verifier.Verify(context.Background(), testLaunchSpec(executable)); err != nil {
		t.Fatalf("缓存校验失败：%v", err)
	}
	if runner.generateCount != 1 {
		t.Fatalf("相同身份重复生成 schema：%d", runner.generateCount)
	}

	runner.version = "codex-cli changed"
	if err := verifier.Verify(context.Background(), testLaunchSpec(executable)); err != nil {
		t.Fatalf("版本变化后校验失败：%v", err)
	}
	changed := time.Now().Add(time.Second)
	if err := os.Chtimes(executable, changed, changed); err != nil {
		t.Fatalf("修改 executable mtime 失败：%v", err)
	}
	if err := verifier.Verify(context.Background(), testLaunchSpec(executable)); err != nil {
		t.Fatalf("mtime 变化后校验失败：%v", err)
	}
	if runner.generateCount != 3 {
		t.Fatalf("身份变化没有重新生成：%d", runner.generateCount)
	}
	if _, err := os.Stat(runner.lastOutput); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("临时 schema 目录未清理：%v", err)
	}
}

// TestSchemaVerifier_FailsClosedOnMissingInvalidOrSemanticDrift 验证缺失、非法 JSON 和语义漂移均关闭能力。
func TestSchemaVerifier_FailsClosedOnMissingInvalidOrSemanticDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    map[string][]byte
		contract codex.SchemaContract
	}{
		{name: "missing", files: map[string][]byte{}, contract: contractFor("required.json", []byte(`{}`))},
		{name: "invalid json", files: map[string][]byte{"required.json": []byte(`{`)}, contract: contractFor("required.json", []byte(`{}`))},
		{name: "semantic drift", files: map[string][]byte{"required.json": []byte(`{"changed":true}`)}, contract: contractFor("required.json", []byte(`{}`))},
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
			err := verifier.Verify(context.Background(), testLaunchSpec(executable))
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

// TestSchemaVerifier_AcceptsJSONRepresentationOnlyChange 验证字段顺序和空白变化不阻断 Codex 升级。
func TestSchemaVerifier_AcceptsJSONRepresentationOnlyChange(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	executable := filepath.Join(root, "codex")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatalf("创建测试 executable 失败：%v", err)
	}
	baseline := []byte(`{"type":"object","properties":{"threadId":{"type":"string"},"cwd":{"type":"string"}}}`)
	reordered := []byte("{\n  \"properties\": {\"cwd\": {\"type\": \"string\"}, \"threadId\": {\"type\": \"string\"}},\n  \"type\": \"object\"\n}")
	verifier := codex.NewSchemaVerifier(&schemaRunner{version: "codex-cli upgraded", files: map[string][]byte{"required.json": reordered}}, contractFor("required.json", baseline), root)
	if err := verifier.Verify(context.Background(), testLaunchSpec(executable)); err != nil {
		t.Fatalf("仅 JSON 表示变化不应阻断：%v", err)
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
	if err := verifier.Verify(context.Background(), testLaunchSpec(executable)); err != nil {
		t.Fatalf("非必要 schema 不应导致失败：%v", err)
	}
}

// contractFor 为单个测试文件构造规范化哈希契约。
func contractFor(path string, content []byte) codex.SchemaContract {
	digest, err := codex.CanonicalSchemaDigest(content)
	if err != nil {
		panic(err)
	}
	return codex.SchemaContract{Version: "test", Files: map[string]string{path: digest}}
}

// testLaunchSpec 为 schema verifier 测试构造最小启动身份。
func testLaunchSpec(executable string) codex.LaunchSpec {
	return codex.LaunchSpec{Command: executable, SourcePath: executable, Env: os.Environ()}
}
