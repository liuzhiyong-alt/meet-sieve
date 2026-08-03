package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"meet-sieve/internal/infra/apperr"
)

// SchemaContract 固定当前实现真正依赖的 schema 文件及其哈希。
type SchemaContract struct {
	Version string
	Files   map[string]string
}

// SchemaCommandRunner 隔离版本查询与 schema 生成进程，便于验证缓存和失败清理。
type SchemaCommandRunner interface {
	Version(ctx context.Context, executablePath string) (string, error)
	Generate(ctx context.Context, executablePath string, outputDirectory string) error
}

// CommandSchemaRunner 使用用户指定的 Codex executable 执行官方 schema 生成命令。
type CommandSchemaRunner struct{}

// Version 返回 Codex CLI 的版本身份。
func (CommandSchemaRunner) Version(ctx context.Context, executablePath string) (string, error) {
	output, err := exec.CommandContext(ctx, executablePath, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("读取 provider 版本失败: %w", err)
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "", fmt.Errorf("provider 版本为空")
	}
	return version, nil
}

// Generate 调用 app-server 官方命令，把 schema 写入一次性目录。
func (CommandSchemaRunner) Generate(ctx context.Context, executablePath string, outputDirectory string) error {
	command := exec.CommandContext(
		ctx,
		executablePath,
		"app-server",
		"generate-json-schema",
		"--out",
		outputDirectory,
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("生成 provider schema 失败: %w (%d bytes diagnostic)", err, len(output))
	}
	return nil
}

// SchemaVerifier 对运行时生成的必要 schema 执行严格 JSON 与哈希校验。
type SchemaVerifier struct {
	runner        SchemaCommandRunner
	contract      SchemaContract
	temporaryRoot string
	mu            sync.Mutex
	verifiedKey   string
}

// NewSchemaVerifier 创建严格 schema 校验器；temporaryRoot 为空时使用系统临时目录。
func NewSchemaVerifier(runner SchemaCommandRunner, contract SchemaContract, temporaryRoot string) *SchemaVerifier {
	files := make(map[string]string, len(contract.Files))
	for path, digest := range contract.Files {
		files[path] = digest
	}
	if temporaryRoot == "" {
		temporaryRoot = os.TempDir()
	}
	return &SchemaVerifier{
		runner:        runner,
		contract:      SchemaContract{Version: contract.Version, Files: files},
		temporaryRoot: temporaryRoot,
	}
}

// Verify 按 executable 路径、版本和 mtime 缓存兼容结论，并保证临时目录始终清理。
func (verifier *SchemaVerifier) Verify(ctx context.Context, executablePath string) error {
	verifier.mu.Lock()
	defer verifier.mu.Unlock()

	identity, err := verifier.loadIdentity(ctx, executablePath)
	if err != nil {
		return err
	}
	if verifier.verifiedKey == identity {
		return nil
	}
	if err := verifier.generateAndValidate(ctx, executablePath); err != nil {
		return err
	}
	verifier.verifiedKey = identity
	return nil
}

// loadIdentity 读取缓存所需身份，不把可执行文件路径写入用户消息。
func (verifier *SchemaVerifier) loadIdentity(ctx context.Context, executablePath string) (string, error) {
	info, err := os.Stat(executablePath)
	if err != nil || info.IsDir() {
		return "", apperr.Biz(
			apperr.CodeAgentExecutableInvalid,
			apperr.WithOp("agent.schema.executable"),
		)
	}
	version, err := verifier.runner.Version(ctx, executablePath)
	if err != nil {
		return "", protocolIncompatible(err, "agent.schema.version")
	}
	return fmt.Sprintf("%s\x00%s\x00%d", executablePath, version, info.ModTime().UnixNano()), nil
}

// generateAndValidate 原子执行生成与必要文件校验，任何失败都不更新缓存。
func (verifier *SchemaVerifier) generateAndValidate(ctx context.Context, executablePath string) error {
	directory, err := os.MkdirTemp(verifier.temporaryRoot, "meetsieve-codex-schema-")
	if err != nil {
		return protocolIncompatible(err, "agent.schema.temp")
	}
	defer func() { _ = os.RemoveAll(directory) }()

	if err := verifier.runner.Generate(ctx, executablePath, directory); err != nil {
		return protocolIncompatible(err, "agent.schema.generate")
	}
	if err := validateRequiredSchemas(directory, verifier.contract.Files); err != nil {
		return protocolIncompatible(err, "agent.schema.validate")
	}
	return nil
}

// validateRequiredSchemas 只校验必要文件，允许 provider 增加与当前实现无关的 schema。
func validateRequiredSchemas(root string, required map[string]string) error {
	if len(required) == 0 {
		return fmt.Errorf("必要 schema 清单为空")
	}
	for relativePath, expectedDigest := range required {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			return fmt.Errorf("必要 schema 缺失: %s", relativePath)
		}
		if !json.Valid(content) {
			return fmt.Errorf("必要 schema 不是合法 JSON: %s", relativePath)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != expectedDigest {
			return fmt.Errorf("必要 schema 哈希漂移: %s", relativePath)
		}
	}
	return nil
}

// protocolIncompatible 统一返回安全用户文案，同时保留内部 cause 和操作名。
func protocolIncompatible(cause error, operation string) error {
	return apperr.Dependency(
		apperr.CodeAgentProtocolIncompatible,
		cause,
		apperr.WithOp(operation),
	)
}
