package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"meet-sieve/internal/infra/apperr"
)

// SchemaContract 固定当前实现真正依赖的 schema 文件及其规范化哈希。
type SchemaContract struct {
	Version string
	Files   map[string]string
}

// SchemaCommandRunner 隔离版本查询与 schema 生成进程，便于验证缓存和失败清理。
type SchemaCommandRunner interface {
	Version(ctx context.Context, spec LaunchSpec) (string, error)
	Generate(ctx context.Context, spec LaunchSpec, outputDirectory string) error
}

// CommandSchemaRunner 使用用户指定的 Codex executable 执行官方 schema 生成命令。
type CommandSchemaRunner struct{}

// Version 返回 Codex CLI 的版本身份。
func (CommandSchemaRunner) Version(ctx context.Context, spec LaunchSpec) (string, error) {
	output, err := spec.CommandContext(ctx, "--version").Output()
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
func (CommandSchemaRunner) Generate(ctx context.Context, spec LaunchSpec, outputDirectory string) error {
	command := spec.CommandContext(
		ctx,
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

// SchemaVerifier 对运行时生成的必要 schema 执行 JSON 语义哈希校验。
type SchemaVerifier struct {
	runner        SchemaCommandRunner
	contract      SchemaContract
	temporaryRoot string
	mu            sync.Mutex
	verifiedKey   string
}

// NewSchemaVerifier 创建 schema 语义校验器；temporaryRoot 为空时使用系统临时目录。
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
func (verifier *SchemaVerifier) Verify(ctx context.Context, spec LaunchSpec) error {
	verifier.mu.Lock()
	defer verifier.mu.Unlock()

	identity, err := verifier.loadIdentity(ctx, spec)
	if err != nil {
		return err
	}
	if verifier.verifiedKey == identity {
		return nil
	}
	if err := verifier.generateAndValidate(ctx, spec); err != nil {
		return err
	}
	verifier.verifiedKey = identity
	return nil
}

// loadIdentity 读取缓存所需身份，不把可执行文件路径写入用户消息。
func (verifier *SchemaVerifier) loadIdentity(ctx context.Context, spec LaunchSpec) (string, error) {
	info, err := os.Stat(spec.SourcePath)
	if err != nil || info.IsDir() {
		return "", apperr.Biz(
			apperr.CodeAgentExecutableInvalid,
			apperr.WithOp("agent.schema.executable"),
		)
	}
	version, err := verifier.runner.Version(ctx, spec)
	if err != nil {
		return "", mapSchemaCommandError(err, "agent.schema.version")
	}
	pathDigest := sha256.Sum256([]byte(environmentValue(spec.Env, "PATH", runtime.GOOS == "windows")))
	return fmt.Sprintf("%s\x00%s\x00%d\x00%x", spec.SourcePath, version, info.ModTime().UnixNano(), pathDigest), nil
}

// generateAndValidate 原子执行生成与必要文件校验，任何失败都不更新缓存。
func (verifier *SchemaVerifier) generateAndValidate(ctx context.Context, spec LaunchSpec) error {
	directory, err := os.MkdirTemp(verifier.temporaryRoot, "meetsieve-codex-schema-")
	if err != nil {
		return apperr.Dependency(apperr.CodeAgentLaunchFailed, err, apperr.WithOp("agent.schema.temp"))
	}
	defer func() { _ = os.RemoveAll(directory) }()

	if err := verifier.runner.Generate(ctx, spec, directory); err != nil {
		return mapSchemaCommandError(err, "agent.schema.generate")
	}
	if err := validateRequiredSchemas(directory, verifier.contract.Files); err != nil {
		return protocolIncompatible(err, "agent.schema.validate")
	}
	return nil
}

// mapSchemaCommandError 区分调用方取消、外部命令超时、已分类错误和普通启动失败。
func mapSchemaCommandError(cause error, operation string) error {
	if errors.Is(cause, context.Canceled) {
		return apperr.Biz(apperr.CodeCanceled, apperr.WithOp(operation))
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return apperr.Dependency(apperr.CodeDependencyTimeout, cause, apperr.WithOp(operation))
	}
	var appErr *apperr.AppError
	if errors.As(cause, &appErr) {
		return cause
	}
	return apperr.Dependency(apperr.CodeAgentLaunchFailed, cause, apperr.WithOp(operation))
}

// validateRequiredSchemas 只校验必要文件，允许 provider 增加与当前实现无关的 schema。
// 对象键顺序、缩进与换行不属于协议语义，不能导致升级后的 Codex 不可用。
func validateRequiredSchemas(root string, required map[string]string) error {
	if len(required) == 0 {
		return fmt.Errorf("必要 schema 清单为空")
	}
	for relativePath, expectedDigest := range required {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			return fmt.Errorf("必要 schema 缺失: %s", relativePath)
		}
		digest, err := CanonicalSchemaDigest(content)
		if err != nil {
			return fmt.Errorf("必要 schema 不是合法 JSON: %s: %w", relativePath, err)
		}
		if digest != expectedDigest {
			return fmt.Errorf("必要 schema 哈希漂移: %s", relativePath)
		}
	}
	return nil
}

// CanonicalSchemaDigest 计算 JSON schema 的规范化 SHA-256。
// 它保留数组顺序和 number 原文，排序对象键，从而只忽略 JSON 表示层差异。
func CanonicalSchemaDigest(content []byte) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", fmt.Errorf("JSON schema 包含多个顶层值")
		}
		return "", err
	}
	canonical, err := marshalCanonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// marshalCanonicalJSON 按 JSON 类型递归输出稳定字节序列，不改变数组和标量语义。
func marshalCanonicalJSON(value any) ([]byte, error) {
	var builder strings.Builder
	if err := writeCanonicalJSON(&builder, value); err != nil {
		return nil, err
	}
	return []byte(builder.String()), nil
}

// writeCanonicalJSON 排序 object key，拒绝 decoder 不应产生的未知 Go 类型。
func writeCanonicalJSON(builder *strings.Builder, value any) error {
	switch typed := value.(type) {
	case nil:
		builder.WriteString("null")
	case bool:
		builder.WriteString(strconv.FormatBool(typed))
	case string:
		encoded, err := canonicalJSONString(typed)
		if err != nil {
			return err
		}
		builder.Write(encoded)
	case json.Number:
		number, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return fmt.Errorf("JSON number 无效: %w", err)
		}
		// JSON 的 1、1.0 与 1e0 表达相同数值，统一为稳定浮点字面量。
		builder.WriteString(strconv.FormatFloat(number, 'g', -1, 64))
	case []any:
		builder.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeCanonicalJSON(builder, item); err != nil {
				return err
			}
		}
		builder.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				builder.WriteByte(',')
			}
			encodedKey, err := canonicalJSONString(key)
			if err != nil {
				return err
			}
			builder.Write(encodedKey)
			builder.WriteByte(':')
			if err := writeCanonicalJSON(builder, typed[key]); err != nil {
				return err
			}
		}
		builder.WriteByte('}')
	default:
		return fmt.Errorf("JSON schema 包含未知类型 %T", value)
	}
	return nil
}

// canonicalJSONString 使用 JSON 编码字符串，但不把 HTML 字符重新写成转义序列。
func canonicalJSONString(value string) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	encoded = []byte(strings.NewReplacer(
		`\u003c`, "<",
		`\u003e`, ">",
		`\u0026`, "&",
	).Replace(string(encoded)))
	return encoded, nil
}

// protocolIncompatible 统一返回安全用户文案，同时保留内部 cause 和操作名。
func protocolIncompatible(cause error, operation string) error {
	return apperr.Dependency(
		apperr.CodeAgentProtocolIncompatible,
		cause,
		apperr.WithOp(operation),
	)
}
