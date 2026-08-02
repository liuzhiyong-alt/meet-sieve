package transcript

import (
	"bytes"
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

// TestConvertRawRecordRows_UsesCurrentSpeakerPriority 验证成员、unknown cluster、session fallback 展示优先级。
func TestConvertRawRecordRows_UsesCurrentSpeakerPriority(t *testing.T) {
	rows := []transcriptrepository.RawRecordRow{
		{Seq: 1, Kind: "utterance.final", StartSample: 0, EndSample: 16, CurrentText: "成员", ASRSessionID: "session", ParticipantDisplayName: "张三", ClusterDisplayNo: 9},
		{Seq: 2, Kind: "utterance.final", StartSample: 16, EndSample: 32, CurrentText: "未知", ASRSessionID: "session", ClusterDisplayNo: 2},
		{Seq: 3, Kind: "utterance.final", StartSample: 32, EndSample: 48, CurrentText: "待识别", ASRSessionID: "session"},
	}
	entries, _ := convertRawRecordRows(rows, []models.ASRSession{{ID: "session"}})
	if entries[0].Speaker != "张三" || entries[1].Speaker != "未知说话人 2" || rawRecordSpeaker(entries[2]) != "未知说话人（ASR Session 1）" {
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
