package agent

import (
	"encoding/binary"
	"testing"

	domainagent "meet-sieve/internal/domain/agent"
	"meet-sieve/internal/port"
	agentrepository "meet-sieve/internal/repository/agent"
)

// TestWakeCommandCollector_SubmitsCrossFinalCommandAfterThreeSeconds 验证短停顿跨 final，长停顿累计三秒后提交。
func TestWakeCommandCollector_SubmitsCrossFinalCommandAfterThreeSeconds(t *testing.T) {
	wake, err := domainagent.NormalizeWakeWord("哈喽,会议助手")
	if err != nil {
		t.Fatal(err)
	}
	collector := &wakeCommandCollector{}
	matched, command := collector.observeFinal(agentrepository.WakeFinal{
		UtteranceID: "wake", MeetingID: "meeting", Text: "哈喽，会议助手。", StartSample: 0, EndSample: 16_000,
	}, domainagent.NewWakeMatcher(wake), wake.Hash)
	if !matched || command != nil || collector.statusValue() != WakeCommandWaiting {
		t.Fatalf("唤醒后状态错误：matched=%v command=%+v status=%s", matched, command, collector.statusValue())
	}

	collector.observeFrame(voiceFrame(24_000, 1_600))
	matched, command = collector.observeFinal(agentrepository.WakeFinal{
		UtteranceID: "command", MeetingID: "meeting", Text: "整理一下刚才的三个结论", StartSample: 24_000, EndSample: 40_000,
	}, nil, "")
	if !matched || command != nil {
		t.Fatalf("指令 final 不应提前提交：matched=%v command=%+v", matched, command)
	}
	command = collector.observeFrame(silenceFrame(87_999, 1))
	if command == nil || command.Question != "整理一下刚才的三个结论" || collector.statusValue() != WakeCommandBusy {
		t.Fatalf("三秒静音后未提交完整指令：command=%+v status=%s", command, collector.statusValue())
	}
}

// TestWakeCommandCollector_TimesOutWaitingForCommand 验证唤醒后六秒没有有效语音会取消等待。
func TestWakeCommandCollector_TimesOutWaitingForCommand(t *testing.T) {
	wake, err := domainagent.NormalizeWakeWord("AI 助手")
	if err != nil {
		t.Fatal(err)
	}
	collector := &wakeCommandCollector{}
	collector.observeFinal(agentrepository.WakeFinal{
		UtteranceID: "wake", MeetingID: "meeting", Text: "AI 助手。", StartSample: 0, EndSample: 16_000,
	}, domainagent.NewWakeMatcher(wake), wake.Hash)
	collector.observeFrame(silenceFrame(111_999, 1))
	if collector.statusValue() != WakeCommandIdle {
		t.Fatalf("等待六秒后应回到 idle：%s", collector.statusValue())
	}
}

// voiceFrame 创建稳定超过自适应门限的 16-bit PCM 测试帧。
func voiceFrame(startSample int64, samples int) port.AudioFrame {
	pcm := make([]byte, samples*2)
	for index := 0; index < samples; index++ {
		binary.LittleEndian.PutUint16(pcm[index*2:index*2+2], uint16(int16(4_000)))
	}
	return port.AudioFrame{PCM: pcm, StartSample: startSample}
}

// silenceFrame 创建指定样本长度的静音帧。
func silenceFrame(startSample int64, samples int) port.AudioFrame {
	return port.AudioFrame{PCM: make([]byte, samples*2), StartSample: startSample}
}
