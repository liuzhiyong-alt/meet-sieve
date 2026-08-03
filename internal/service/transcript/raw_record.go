package transcript

import (
	"fmt"
	"strings"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
)

// RawRecordEntry 是渲染原始记录所需的已脱敏事件投影。
type RawRecordEntry struct {
	Seq          int64
	Kind         string
	OccurredAt   int64
	StartSample  int64
	EndSample    int64
	Text         string
	Speaker      string
	SessionOrder int
	DisplayName  string
	URL          string
	OriginalName string
	MediaType    string
	SizeBytes    int64
	SHA256       string
	Description  string
}

// RawRecordInput 是 SQLite 快照转换后的固定模板输入，不携带 UUID、路径或凭据。
type RawRecordInput struct {
	Subject        string
	MeetingNo      string
	StartedAt      time.Time
	RealtimeState  string
	GapCount       int
	HasCorrections bool
	Entries        []RawRecordEntry
}

// RenderRawRecord 使用固定换行和排序规则生成可重建的会议原始记录 Markdown。
func RenderRawRecord(input RawRecordInput) ([]byte, error) {
	if strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.MeetingNo) == "" || input.StartedAt.IsZero() || input.GapCount < 0 {
		return nil, fmt.Errorf("原始记录渲染输入无效")
	}
	entries, err := sortedRawRecordEntries(input.Entries)
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(escapeMarkdownInline(input.Subject))
	builder.WriteString("\n\n- 会议号：")
	builder.WriteString(escapeMarkdownInline(input.MeetingNo))
	builder.WriteString("\n- 开始时间：")
	builder.WriteString(input.StartedAt.Format("2006-01-02 15:04:05 MST"))
	builder.WriteString("\n- 实时转写：")
	builder.WriteString(rawRecordASRStatus(input.RealtimeState, input.GapCount))
	builder.WriteString("\n\n## 原始记录\n")
	for _, entry := range entries {
		builder.WriteString("\n")
		switch entry.Kind {
		case "utterance.final":
			builder.WriteString("### ")
			builder.WriteString(formatSampleTime(entry.StartSample))
			builder.WriteString(" · ")
			builder.WriteString(escapeMarkdownInline(rawRecordSpeaker(entry)))
			builder.WriteString("\n\n")
			builder.WriteString(escapeMarkdownText(entry.Text))
			builder.WriteString("\n")
		case "asr.gap":
			builder.WriteString("### ")
			builder.WriteString(formatSampleTime(entry.StartSample))
			builder.WriteString("–")
			builder.WriteString(formatSampleTime(entry.EndSample))
			builder.WriteString(" · 转写缺口\n\n实时转写在此范围中断。录音仍保存在本地，尚未执行补转写。\n")
		case "message.created":
			writeGuestHeading(&builder, input.StartedAt, entry, "会议消息")
			builder.WriteString("\n\n")
			builder.WriteString(escapeMarkdownText(entry.Text))
			builder.WriteString("\n")
		case "resource.link":
			writeGuestHeading(&builder, input.StartedAt, entry, "链接")
			builder.WriteString("\n\n链接：")
			builder.WriteString(escapeMarkdownInline(entry.URL))
			builder.WriteString("\n")
		case "resource.attachment":
			writeGuestHeading(&builder, input.StartedAt, entry, "附件")
			builder.WriteString("\n\n- 文件名：")
			builder.WriteString(escapeMarkdownInline(entry.OriginalName))
			builder.WriteString("\n- 大小：")
			builder.WriteString(fmt.Sprintf("%d bytes", entry.SizeBytes))
			builder.WriteString("\n- SHA-256：")
			builder.WriteString(escapeMarkdownInline(entry.SHA256))
			if entry.MediaType != "" {
				builder.WriteString("\n- 媒体类型：")
				builder.WriteString(escapeMarkdownInline(entry.MediaType))
			}
			if entry.Description != "" {
				builder.WriteString("\n- 说明：")
				builder.WriteString(escapeMarkdownText(entry.Description))
			}
			builder.WriteString("\n")
		case "ai.question":
			writeAIHeading(&builder, input.StartedAt, entry, "主持人提问")
			builder.WriteString("\n\n")
			builder.WriteString(escapeMarkdownText(entry.Text))
			builder.WriteString("\n")
		case "ai.answer":
			writeAIHeading(&builder, input.StartedAt, entry, "AI 回答（未经人工确认）")
			builder.WriteString("\n\n")
			builder.WriteString(escapeMarkdownText(entry.Text))
			builder.WriteString("\n")
		case "ai.cancelled":
			writeAIHeading(&builder, input.StartedAt, entry, "AI 回答已停止")
			builder.WriteString("\n\n本次任务未产生公开回答。\n")
		case "ai.failed":
			writeAIHeading(&builder, input.StartedAt, entry, "AI 任务失败，未产生公开回答")
			builder.WriteString("\n")
		}
	}
	if input.HasCorrections {
		builder.WriteString("\n> 本记录包含人工校对。原始 ASR 与修改历史保存在 MeetSieve 本地数据库中。\n")
	}
	return []byte(strings.TrimRight(builder.String(), "\n") + "\n"), nil
}

// rawRecordSpeaker 优先使用当前投影，未识别时才回退到脱敏的 session 编号。
func rawRecordSpeaker(entry RawRecordEntry) string {
	if strings.TrimSpace(entry.Speaker) != "" {
		return entry.Speaker
	}
	return fmt.Sprintf("未知说话人（ASR Session %d）", entry.SessionOrder)
}

// sortedRawRecordEntries 验证有序 seq，避免调用方误用墙上时间排序改变记录事实。
func sortedRawRecordEntries(entries []RawRecordEntry) ([]RawRecordEntry, error) {
	result := append([]RawRecordEntry(nil), entries...)
	lastSeq := int64(0)
	for _, entry := range result {
		if entry.Seq <= lastSeq || !validRawRecordEntry(entry) {
			return nil, fmt.Errorf("原始记录事件序列无效")
		}
		if entry.Kind == "utterance.final" && (strings.TrimSpace(entry.Text) == "" || entry.SessionOrder <= 0) {
			return nil, fmt.Errorf("原始记录 final 事件无效")
		}
		lastSeq = entry.Seq
	}
	return result, nil
}

// validRawRecordEntry 按 kind 区分音频 sample 事件和墙上时间内容事件。
func validRawRecordEntry(entry RawRecordEntry) bool {
	switch entry.Kind {
	case "utterance.final":
		return entry.StartSample >= 0 && entry.EndSample > entry.StartSample && strings.TrimSpace(entry.Text) != "" && entry.SessionOrder > 0
	case "asr.gap":
		return entry.StartSample >= 0 && entry.EndSample > entry.StartSample
	case "message.created":
		return entry.OccurredAt >= 0 && strings.TrimSpace(entry.DisplayName) != "" && strings.TrimSpace(entry.Text) != ""
	case "resource.link":
		return entry.OccurredAt >= 0 && strings.TrimSpace(entry.DisplayName) != "" && strings.TrimSpace(entry.URL) != ""
	case "resource.attachment":
		return entry.OccurredAt >= 0 && strings.TrimSpace(entry.DisplayName) != "" && strings.TrimSpace(entry.OriginalName) != "" && entry.SizeBytes > 0 && len(entry.SHA256) == 64
	case "ai.question", "ai.answer":
		return entry.OccurredAt >= 0 && strings.TrimSpace(entry.Text) != ""
	case "ai.cancelled", "ai.failed":
		return entry.OccurredAt >= 0
	default:
		return false
	}
}

// writeAIHeading 用统一事件时间渲染明确的 AI 内容身份。
func writeAIHeading(builder *strings.Builder, startedAt time.Time, entry RawRecordEntry, label string) {
	builder.WriteString("### ")
	builder.WriteString(formatOccurredTime(startedAt, entry.OccurredAt))
	builder.WriteString(" · ")
	builder.WriteString(label)
}

// writeGuestHeading 以 event occurred_at 相对会议开始时间渲染无 sample 内容。
func writeGuestHeading(builder *strings.Builder, startedAt time.Time, entry RawRecordEntry, label string) {
	builder.WriteString("### ")
	builder.WriteString(formatOccurredTime(startedAt, entry.OccurredAt))
	builder.WriteString(" · ")
	builder.WriteString(label)
	builder.WriteString(" · ")
	builder.WriteString(escapeMarkdownInline(entry.DisplayName))
}

// formatOccurredTime 将持久事件时间格式化为会议内 HH:MM:SS，时钟回退时归零。
func formatOccurredTime(startedAt time.Time, occurredAt int64) string {
	duration := time.UnixMilli(occurredAt).Sub(startedAt)
	if duration < 0 {
		duration = 0
	}
	seconds := int64(duration / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds%3600/60, seconds%60)
}

// rawRecordASRStatus 将实时状态与 gap 摘要显示为可验证的独立事实。
func rawRecordASRStatus(state string, gapCount int) string {
	if gapCount > 0 {
		return fmt.Sprintf("%s，存在 %d 个缺口", state, gapCount)
	}
	return state
}

// formatSampleTime 以全局样本时间线固定格式化为 HH:MM:SS。
func formatSampleTime(sample int64) string {
	seconds := sample / transcriptdomain.SampleRate
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds%3600/60, seconds%60)
}

// escapeMarkdownInline 保护模板字段中的 Markdown 控制字符，不改变其字符内容。
func escapeMarkdownInline(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "`", "\\`")
	return replacer.Replace(value)
}

// escapeMarkdownText 防止 ASR 文本意外形成标题、列表或引用，但保留原始换行和字符顺序。
func escapeMarkdownText(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
			prefixLength := len(line) - len(trimmed)
			lines[index] = line[:prefixLength] + "\\" + trimmed
		}
		lines[index] = escapeMarkdownInline(lines[index])
	}
	return strings.Join(lines, "\n")
}
