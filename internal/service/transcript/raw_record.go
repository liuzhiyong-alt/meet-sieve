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
	StartSample  int64
	EndSample    int64
	Text         string
	Speaker      string
	SessionOrder int
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
		if entry.Seq <= lastSeq || entry.StartSample < 0 || entry.EndSample <= entry.StartSample || (entry.Kind != "utterance.final" && entry.Kind != "asr.gap") {
			return nil, fmt.Errorf("原始记录事件序列无效")
		}
		if entry.Kind == "utterance.final" && (strings.TrimSpace(entry.Text) == "" || entry.SessionOrder <= 0) {
			return nil, fmt.Errorf("原始记录 final 事件无效")
		}
		lastSeq = entry.Seq
	}
	return result, nil
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
