// Package agent 定义与具体智能体厂商无关的会议输出、上下文和唤醒规则。
package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"meet-sieve/internal/port"
)

const (
	// MaxAnswerRunes 是公开回答的 Unicode code point 上限。
	MaxAnswerRunes = 20_000
	// MaxSnapshotBytes 是规范化快照 JSON 的字节上限。
	MaxSnapshotBytes = 64 * 1024
	// MaxSnapshotItems 是每类快照数组的条目上限。
	MaxSnapshotItems = 50
	// MaxSnapshotItemBytes 是单条快照文字的 UTF-8 字节上限。
	MaxSnapshotItemBytes = 2_000
)

// Snapshot 是会话内滚动覆盖的结构化会议记忆。
type Snapshot struct {
	CurrentTopics      []string `json:"current_topics"`
	ConfirmedDecisions []string `json:"confirmed_decisions"`
	BusinessRules      []string `json:"business_rules"`
	Disagreements      []string `json:"disagreements"`
	OpenQuestions      []string `json:"open_questions"`
	References         []string `json:"references"`
}

type answerEnvelope struct {
	Answer   string   `json:"answer"`
	Snapshot Snapshot `json:"snapshot"`
}

type snapshotEnvelope struct {
	Snapshot Snapshot `json:"snapshot"`
}

// ReferenceAllowlist 限定本轮输出可以引用的真实输入。
type ReferenceAllowlist struct {
	Sequences map[int64]struct{}
	URLs      map[string]struct{}
	Resources map[string]struct{}
}

// ValidatedOutput 是通过本地校验、可进入事务提交的结果。
type ValidatedOutput struct {
	Answer         string
	Snapshot       Snapshot
	SnapshotJSON   []byte
	SnapshotSHA256 string
}

// ValidateOutput 按固定顺序校验 JSON、结构、边界、引用和敏感内容。
func ValidateOutput(kind port.AgentTurnKind, content []byte, allowlist ReferenceAllowlist) (ValidatedOutput, error) {
	if !json.Valid(content) {
		return ValidatedOutput{}, fmt.Errorf("智能体输出不是合法 JSON")
	}
	answer, snapshot, err := decodeOutput(kind, content)
	if err != nil {
		return ValidatedOutput{}, err
	}
	if err := validateSnapshot(snapshot, allowlist); err != nil {
		return ValidatedOutput{}, err
	}
	if kind == port.AgentTurnAnswer {
		if err := validateAnswer(answer); err != nil {
			return ValidatedOutput{}, err
		}
	}
	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return ValidatedOutput{}, fmt.Errorf("规范化智能体快照失败：%w", err)
	}
	if len(canonical) > MaxSnapshotBytes {
		return ValidatedOutput{}, fmt.Errorf("智能体快照超过 %d bytes", MaxSnapshotBytes)
	}
	digest := sha256.Sum256(canonical)
	return ValidatedOutput{
		Answer: answer, Snapshot: snapshot, SnapshotJSON: canonical,
		SnapshotSHA256: hex.EncodeToString(digest[:]),
	}, nil
}

// OutputSchema 返回当前 turn 所需的严格 JSON Schema。
func OutputSchema(kind port.AgentTurnKind) ([]byte, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("智能体 turn kind 无效")
	}
	properties := map[string]any{"snapshot": snapshotSchema()}
	required := []string{"snapshot"}
	if kind == port.AgentTurnAnswer {
		properties["answer"] = map[string]any{"type": "string", "minLength": 1, "maxLength": MaxAnswerRunes}
		required = append([]string{"answer"}, required...)
	}
	return json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false,
		"required": required, "properties": properties,
	})
}

// decodeOutput 使用严格 decoder 拒绝未知字段和尾随 JSON。
func decodeOutput(kind port.AgentTurnKind, content []byte) (string, Snapshot, error) {
	if !kind.Valid() {
		return "", Snapshot{}, fmt.Errorf("智能体 turn kind 无效")
	}
	if kind == port.AgentTurnAnswer {
		var envelope answerEnvelope
		if err := decodeStrict(content, &envelope); err != nil {
			return "", Snapshot{}, err
		}
		return envelope.Answer, envelope.Snapshot, nil
	}
	var envelope snapshotEnvelope
	if err := decodeStrict(content, &envelope); err != nil {
		return "", Snapshot{}, err
	}
	return "", envelope.Snapshot, nil
}

// decodeStrict 保证只存在一个对象且所有字段均已登记。
func decodeStrict(content []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("智能体输出结构无效：%w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("智能体输出包含尾随内容")
	}
	return nil
}

// validateAnswer 校验公开回答的字符、长度和安全边界。
func validateAnswer(answer string) error {
	if !utf8.ValidString(answer) || strings.TrimSpace(answer) == "" {
		return fmt.Errorf("智能体回答为空或 UTF-8 无效")
	}
	if utf8.RuneCountInString(answer) > MaxAnswerRunes {
		return fmt.Errorf("智能体回答超过 %d 字符", MaxAnswerRunes)
	}
	return validateSafeText(answer)
}

// validateSnapshot 校验六类数组、单项、引用和敏感内容。
func validateSnapshot(snapshot Snapshot, allowlist ReferenceAllowlist) error {
	groups := [][]string{
		snapshot.CurrentTopics, snapshot.ConfirmedDecisions, snapshot.BusinessRules,
		snapshot.Disagreements, snapshot.OpenQuestions, snapshot.References,
	}
	for _, group := range groups {
		if len(group) > MaxSnapshotItems {
			return fmt.Errorf("智能体快照单类超过 %d 项", MaxSnapshotItems)
		}
		for _, item := range group {
			if !utf8.ValidString(item) || strings.TrimSpace(item) == "" || len([]byte(item)) > MaxSnapshotItemBytes {
				return fmt.Errorf("智能体快照条目为空、无效或过长")
			}
			if err := validateSafeText(item); err != nil {
				return err
			}
		}
	}
	for _, reference := range snapshot.References {
		if !allowlist.allows(reference) {
			return fmt.Errorf("智能体引用不在本轮输入白名单内")
		}
	}
	return nil
}

// allows 判断引用是本轮 seq、URL 或安全资源路径之一。
func (allowlist ReferenceAllowlist) allows(reference string) bool {
	if strings.HasPrefix(reference, "seq:") {
		sequence, err := strconv.ParseInt(strings.TrimPrefix(reference, "seq:"), 10, 64)
		_, exists := allowlist.Sequences[sequence]
		return err == nil && exists
	}
	if parsed, err := url.Parse(reference); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		_, exists := allowlist.URLs[reference]
		return exists
	}
	if !isSafeRelativePath(reference) {
		return false
	}
	_, exists := allowlist.Resources[reference]
	return exists
}

// validateSafeText 拒绝绝对路径、凭据形态和内部协议诊断。
func validateSafeText(value string) error {
	lower := strings.ToLower(value)
	credentialMarkers := []string{"access_token", "access token", "api_key", "api key", "sk-secret", "authorization: bearer"}
	diagnosticMarkers := []string{"json-rpc", "request id", "codex home", "provider turn id"}
	if filepath.IsAbs(value) || looksLikeWindowsAbsolutePath(value) {
		return fmt.Errorf("智能体输出包含绝对路径")
	}
	for _, marker := range append(credentialMarkers, diagnosticMarkers...) {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("智能体输出包含敏感或内部诊断字段")
		}
	}
	return nil
}

// isSafeRelativePath 判断资源引用没有绝对路径和父目录逃逸。
func isSafeRelativePath(value string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(value))
	return value != "" && cleaned == value && cleaned != "." && !strings.HasPrefix(cleaned, "../") && !strings.HasPrefix(cleaned, "/") && !looksLikeWindowsAbsolutePath(value)
}

// looksLikeWindowsAbsolutePath 在非 Windows 构建中同样识别盘符绝对路径。
func looksLikeWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

// snapshotSchema 构造六类数组共用边界的展开 schema。
func snapshotSchema() map[string]any {
	itemList := func() map[string]any {
		return map[string]any{"type": "array", "maxItems": MaxSnapshotItems, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": MaxSnapshotItemBytes}}
	}
	properties := map[string]any{}
	for _, name := range []string{"current_topics", "confirmed_decisions", "business_rules", "disagreements", "open_questions", "references"} {
		properties[name] = itemList()
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required":   []string{"current_topics", "confirmed_decisions", "business_rules", "disagreements", "open_questions", "references"},
		"properties": properties,
	}
}
