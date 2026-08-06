package transcript

import (
	"bytes"
	"strings"
	"testing"
	"time"

	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"
)

// TestRenderRawRecord_IsDeterministic 验证原始记录仅由同一事实快照决定，并使用固定换行与模板。
func TestRenderRawRecord_IsDeterministic(t *testing.T) {
	input := RawRecordInput{Subject: "产品 * 评审", MeetingNo: "MS-20260802-0001", StartedAt: time.Date(2026, 8, 2, 9, 0, 0, 0, time.FixedZone("CST", 8*3600)), RealtimeState: "已停止", GapCount: 1, Entries: []RawRecordEntry{{Seq: 1, Kind: "utterance.final", StartSample: 16000, EndSample: 32000, Text: "# 原始文本", SessionOrder: 1}, {Seq: 2, Kind: "asr.gap", StartSample: 32000, EndSample: 48000}}}
	first, err := RenderRawRecord(input)
	if err != nil {
		t.Fatalf("首次渲染失败：%v", err)
	}
	second, err := RenderRawRecord(input)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("相同快照必须产生相同字节：err=%v\n%s\n%s", err, first, second)
	}
	if first[len(first)-1] != '\n' || bytes.Count(first, []byte("\r\n")) != 0 {
		t.Fatalf("Markdown 必须使用 LF 且只以单个换行结束：%q", first)
	}
	if !bytes.Contains(first, []byte("未知说话人（ASR Session 1）")) || !bytes.Contains(first, []byte("转写缺口")) || !bytes.Contains(first, []byte("\\\\# 原始文本")) {
		t.Fatalf("模板或安全转义错误：%s", first)
	}
}

// TestConvertRawRecordRows_UsesCurrentSpeakerPriority 验证成员、unknown cluster、匿名 track 展示优先级。
func TestConvertRawRecordRows_UsesCurrentSpeakerPriority(t *testing.T) {
	rows := []transcriptrepository.RawRecordRow{
		{Seq: 1, Kind: "utterance.final", StartSample: 0, EndSample: 16, CurrentText: "成员", ASRSessionID: "session", ParticipantDisplayName: "张三", ClusterDisplayNo: 9},
		{Seq: 2, Kind: "utterance.final", StartSample: 16, EndSample: 32, CurrentText: "未知", ASRSessionID: "session", ClusterDisplayNo: 2},
		{Seq: 3, Kind: "utterance.final", StartSample: 32, EndSample: 48, CurrentText: "待识别", ASRSessionID: "session", TrackDisplayNo: 3},
	}
	entries, _ := convertRawRecordRows(rows, []models.ASRSession{{ID: "session"}})
	if entries[0].Speaker != "张三" || entries[1].Speaker != "未知说话人 2" || entries[2].Speaker != "说话人 3" {
		t.Fatalf("说话人展示优先级错误：%+v", entries)
	}
}

// TestRenderRawRecord_AddsCorrectionNoteOnlyWhenNeeded 验证校对说明不会在每条正文重复。
func TestRenderRawRecord_AddsCorrectionNoteOnlyWhenNeeded(t *testing.T) {
	input := RawRecordInput{
		Subject: "校对会议", MeetingNo: "MS-20260802-0002", StartedAt: time.Unix(1, 0),
		RealtimeState: "已停止", HasCorrections: true,
		Entries: []RawRecordEntry{{Seq: 1, Kind: "utterance.final", StartSample: 0, EndSample: 16, Text: "当前文本", Speaker: "张三", SessionOrder: 1}},
	}
	content, err := RenderRawRecord(input)
	if err != nil {
		t.Fatalf("渲染校对说明失败：%v", err)
	}
	if bytes.Count(content, []byte("本记录包含人工校对")) != 1 || !bytes.Contains(content, []byte("00:00:00 · 张三")) {
		t.Fatalf("校对说明或当前 speaker 错误：%s", content)
	}
}

// TestRenderRawRecord_MixesGuestContentBySeqWithoutFakeSamples 验证消息、链接和附件使用 occurred_at 且与转写按 seq 混排。
func TestRenderRawRecord_MixesGuestContentBySeqWithoutFakeSamples(t *testing.T) {
	startedAt := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	content, err := RenderRawRecord(RawRecordInput{
		Subject: "Guest", MeetingNo: "MS-20260802-0003", StartedAt: startedAt, RealtimeState: "进行中",
		Entries: []RawRecordEntry{
			{Seq: 1, Kind: "utterance.final", StartSample: 0, EndSample: 16000, Text: "开始", SessionOrder: 1},
			{Seq: 2, Kind: "message.created", OccurredAt: startedAt.Add(2 * time.Second).UnixMilli(), DisplayName: "王_*", Text: "> 消息"},
			{Seq: 3, Kind: "resource.link", OccurredAt: startedAt.Add(3 * time.Second).UnixMilli(), DisplayName: "访客", URL: "https://example.com/a_(b)"},
			{Seq: 4, Kind: "resource.attachment", OccurredAt: startedAt.Add(4 * time.Second).UnixMilli(), DisplayName: "访客", OriginalName: "设计_[1].pdf", MediaType: "application/pdf", SizeBytes: 12, SHA256: strings.Repeat("a", 64), Description: "# 说明"},
		},
	})
	if err != nil {
		t.Fatalf("渲染 Guest 原始记录失败：%v", err)
	}
	text := string(content)
	ordered := []string{"00:00:00", "00:00:02 · 会议消息", "00:00:03 · 链接", "00:00:04 · 附件"}
	last := -1
	for _, marker := range ordered {
		index := strings.Index(text, marker)
		if index <= last {
			t.Fatalf("原始记录未按 seq 混排：marker=%q\n%s", marker, text)
		}
		last = index
	}
	if strings.Contains(text, "meeting/") || !strings.Contains(text, `王\_\*`) || !strings.Contains(text, `\> 消息`) {
		t.Fatalf("Guest 字段转义或敏感路径契约不正确：%s", text)
	}
}

// TestRenderRawRecord_ProjectsAIWithoutPartialOrInternalPayload 验证 AI 事实标记来源且失败不含正文。
func TestRenderRawRecord_ProjectsAIWithoutPartialOrInternalPayload(t *testing.T) {
	startedAt := time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	content, err := RenderRawRecord(RawRecordInput{
		Subject: "AI 记录", MeetingNo: "MS-20260802-0004", StartedAt: startedAt, RealtimeState: "进行中",
		Entries: []RawRecordEntry{
			{Seq: 1, Kind: "ai.question", OccurredAt: startedAt.UnixMilli(), Text: "比较方案"},
			{Seq: 2, Kind: "ai.answer", OccurredAt: startedAt.Add(time.Second).UnixMilli(), Text: "这是 AI 建议"},
			{Seq: 3, Kind: "ai.failed", OccurredAt: startedAt.Add(2 * time.Second).UnixMilli(), Text: "不得投影的 partial"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "主持人提问") || !strings.Contains(text, "AI 回答（未经人工确认）") || !strings.Contains(text, "AI 任务失败，未产生公开回答") || strings.Contains(text, "不得投影的 partial") {
		t.Fatalf("AI 原始记录投影错误：%s", text)
	}
}
