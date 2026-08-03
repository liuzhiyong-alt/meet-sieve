package minutes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const maxMinuteBytes = 512 * 1024

// Conclusion 是带事实来源的会议结论。
type Conclusion struct {
	Text      string  `json:"text"`
	SourceSeq []int64 `json:"source_seq"`
}

// Topic 是带事实来源的讨论议题。
type Topic struct {
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	SourceSeq []int64 `json:"source_seq"`
}

// Task 是带事实来源的待办事项。
type Task struct {
	Task      string  `json:"task"`
	Owner     *string `json:"owner"`
	Due       *string `json:"due"`
	SourceSeq []int64 `json:"source_seq"`
}

// Reference 是已完成会议资料的安全引用。
type Reference struct {
	Label            string `json:"label"`
	ResourceEventSeq int64  `json:"resource_event_seq"`
}

// GapNotice 是生成 cutoff 时仍未完成的转写缺口。
type GapNotice struct {
	StartSample int64  `json:"start_sample"`
	EndSample   int64  `json:"end_sample"`
	State       string `json:"state"`
}

// Output 是 provider 必须返回的 v1 结构化纪要。
type Output struct {
	V           int          `json:"v"`
	Conclusions []Conclusion `json:"conclusions"`
	Topics      []Topic      `json:"topics"`
	Tasks       []Task       `json:"tasks"`
	References  []Reference  `json:"references"`
	GapNotice   []GapNotice  `json:"gap_notice"`
}

// ValidationContext 固定本次生成允许引用的事实、资料和 gap 快照。
type ValidationContext struct {
	FactText    map[int64]string
	FactAnchor  map[int64]string
	ResourceSeq map[int64]struct{}
	GapNotice   []GapNotice
}

// ParseAndValidateOutput 严格解析并验证 provider 结构化纪要。
func ParseAndValidateOutput(content []byte, context ValidationContext) (Output, error) {
	if len(content) == 0 || len(content) > maxMinuteBytes || !utf8.Valid(content) {
		return Output{}, fmt.Errorf("纪要 JSON 大小或编码无效")
	}
	if err := rejectDuplicateKeys(content); err != nil {
		return Output{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var output Output
	if err := decoder.Decode(&output); err != nil {
		return Output{}, fmt.Errorf("解析纪要 JSON 失败：%w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Output{}, err
	}
	if err := validateOutput(output, context); err != nil {
		return Output{}, err
	}
	return output, nil
}

// RenderMarkdown 按固定模板渲染已通过校验的纪要。
func RenderMarkdown(output Output, context ValidationContext) ([]byte, error) {
	if err := validateOutput(output, context); err != nil {
		return nil, err
	}
	var builder strings.Builder
	builder.WriteString("# 会议纪要\n")
	if len(output.GapNotice) > 0 {
		builder.WriteString("\n> 注意：存在未完成的转写缺口，纪要可能遗漏相关讨论。\n")
	}
	writeConclusions(&builder, output.Conclusions, context)
	writeTopics(&builder, output.Topics, context)
	writeTasks(&builder, output.Tasks, context)
	writeReferences(&builder, output.References)
	result := []byte(strings.TrimRight(builder.String(), "\n") + "\n")
	if len(result) > maxMinuteBytes {
		return nil, fmt.Errorf("渲染后纪要超过大小上限")
	}
	return result, nil
}

// rejectDuplicateKeys 递归检查所有 JSON object，避免后值覆盖前值。
func rejectDuplicateKeys(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("读取纪要 JSON 失败：%w", err)
	}
	if err := walkJSONToken(decoder, token); err != nil {
		return err
	}
	return ensureJSONEnd(decoder)
}

// walkJSONToken 从当前 token 开始递归消费一个完整 JSON value。
func walkJSONToken(decoder *json.Decoder, token json.Token) error {
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("纪要 JSON object key 无效")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("纪要 JSON 包含重复字段：%s", key)
			}
			seen[key] = struct{}{}
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := walkJSONToken(decoder, value); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := walkJSONToken(decoder, value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("纪要 JSON delimiter 无效")
	}
	_, err := decoder.Token()
	return err
}

// ensureJSONEnd 确保 JSON 后没有第二个值或垃圾内容。
func ensureJSONEnd(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("纪要 JSON 包含额外内容")
		}
		return fmt.Errorf("纪要 JSON 尾部无效：%w", err)
	}
	return nil
}

// validateOutput 验证版本、来源、资料、保守 owner/due 和完整 gap 快照。
func validateOutput(output Output, context ValidationContext) error {
	if output.V != 1 || !equalGapNotices(output.GapNotice, context.GapNotice) {
		return fmt.Errorf("纪要版本或 gap 快照不一致")
	}
	for _, conclusion := range output.Conclusions {
		if strings.TrimSpace(conclusion.Text) == "" || !validSources(conclusion.SourceSeq, context) {
			return fmt.Errorf("纪要结论来源无效")
		}
	}
	for _, topic := range output.Topics {
		if strings.TrimSpace(topic.Title) == "" || strings.TrimSpace(topic.Content) == "" || !validSources(topic.SourceSeq, context) {
			return fmt.Errorf("纪要议题来源无效")
		}
	}
	for _, task := range output.Tasks {
		if strings.TrimSpace(task.Task) == "" || !validSources(task.SourceSeq, context) || !taskFieldsSupported(task, context) {
			return fmt.Errorf("纪要待办来源无效")
		}
	}
	for _, reference := range output.References {
		if strings.TrimSpace(reference.Label) == "" {
			return fmt.Errorf("纪要资料标签无效")
		}
		if _, exists := context.ResourceSeq[reference.ResourceEventSeq]; !exists {
			return fmt.Errorf("纪要资料来源无效")
		}
	}
	return nil
}

// equalGapNotices 按内容比较固定快照，空切片与 nil 均表示没有 gap。
func equalGapNotices(left []GapNotice, right []GapNotice) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// validSources 要求非空、无重复且全部属于本次事实白名单。
func validSources(sources []int64, context ValidationContext) bool {
	if len(sources) == 0 {
		return false
	}
	seen := make(map[int64]struct{}, len(sources))
	for _, sequence := range sources {
		if _, exists := context.FactText[sequence]; !exists {
			return false
		}
		if _, duplicate := seen[sequence]; duplicate {
			return false
		}
		seen[sequence] = struct{}{}
	}
	return true
}

// taskFieldsSupported 保守要求 owner/due 的当前文字出现在至少一条来源事实中。
func taskFieldsSupported(task Task, context ValidationContext) bool {
	for _, value := range []*string{task.Owner, task.Due} {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed == "" || !sourcesContain(task.SourceSeq, context.FactText, trimmed) {
			return false
		}
	}
	return true
}

// sourcesContain 判断任一白名单来源包含待验证文字。
func sourcesContain(sources []int64, facts map[int64]string, value string) bool {
	for _, sequence := range sources {
		if strings.Contains(facts[sequence], value) {
			return true
		}
	}
	return false
}

// writeConclusions 渲染会议结论段。
func writeConclusions(builder *strings.Builder, values []Conclusion, context ValidationContext) {
	builder.WriteString("\n## 会议结论\n")
	for _, value := range values {
		builder.WriteString("\n- ")
		builder.WriteString(escapeMarkdown(value.Text))
		builder.WriteString(renderSources(value.SourceSeq, context))
	}
}

// writeTopics 渲染讨论议题段。
func writeTopics(builder *strings.Builder, values []Topic, context ValidationContext) {
	builder.WriteString("\n## 讨论议题\n")
	for _, value := range values {
		builder.WriteString("\n### ")
		builder.WriteString(escapeMarkdown(value.Title))
		builder.WriteString("\n\n")
		builder.WriteString(escapeMarkdown(value.Content))
		builder.WriteString(renderSources(value.SourceSeq, context))
		builder.WriteString("\n")
	}
}

// writeTasks 渲染待办事项段。
func writeTasks(builder *strings.Builder, values []Task, context ValidationContext) {
	builder.WriteString("\n## 待办事项\n")
	for _, value := range values {
		builder.WriteString("\n- [ ] ")
		builder.WriteString(escapeMarkdown(value.Task))
		if value.Owner != nil {
			builder.WriteString("；负责人：" + escapeMarkdown(*value.Owner))
		}
		if value.Due != nil {
			builder.WriteString("；截止：" + escapeMarkdown(*value.Due))
		}
		builder.WriteString(renderSources(value.SourceSeq, context))
	}
}

// writeReferences 渲染相关资料段。
func writeReferences(builder *strings.Builder, values []Reference) {
	builder.WriteString("\n## 相关资料\n")
	for _, value := range values {
		builder.WriteString(fmt.Sprintf("\n- %s（原始记录事件 #%d）", escapeMarkdown(value.Label), value.ResourceEventSeq))
	}
}

// renderSources 渲染原始记录时间锚点。
func renderSources(sources []int64, context ValidationContext) string {
	anchors := make([]string, 0, len(sources))
	for _, sequence := range sources {
		anchors = append(anchors, "原始记录 "+context.FactAnchor[sequence])
	}
	return "（" + strings.Join(anchors, "、") + "）"
}

// escapeMarkdown 转义 provider 可控正文中的基础 Markdown 控制字符。
func escapeMarkdown(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "`", "\\`")
	return replacer.Replace(strings.TrimSpace(value))
}
