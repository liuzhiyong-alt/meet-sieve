package port_test

import (
	"reflect"
	"testing"

	"meet-sieve/internal/port"
)

// TestFileTranscriber_ExposesNormalizedAuditableContract 固定补转写请求与规范化结果的业务字段。
func TestFileTranscriber_ExposesNormalizedAuditableContract(t *testing.T) {
	t.Parallel()

	assertStructFields(t, reflect.TypeOf(port.FileTranscriptionRequest{}), []string{
		"MeetingID", "RequestID", "AudioPath", "AudioSHA256", "CoreStartSample",
		"CoreEndSample", "AudioStartSample", "AudioEndSample", "SampleRate",
	})
	assertStructFields(t, reflect.TypeOf(port.FileTranscriptionResult{}), []string{
		"ProviderLogIDSuffix", "NoSpeech", "Segments",
	})
}

// TestAgentTurnKind_DistinguishesMinutesFromFinalIngest 固定纪要与结束同步 ingest 的不同业务用途。
func TestAgentTurnKind_DistinguishesMinutesFromFinalIngest(t *testing.T) {
	t.Parallel()

	if !port.AgentTurnMinutes.Valid() {
		t.Fatal("纪要 turn 必须是合法的稳定业务用途")
	}
	if port.AgentTurnMinutes == port.AgentTurnIngest {
		t.Fatal("纪要生成不能与结束同步使用的 ingest 用途混同")
	}
}

// assertStructFields 验证业务契约只暴露指定字段，避免渗漏 HTTP 或厂商响应结构。
func assertStructFields(t *testing.T, structType reflect.Type, want []string) {
	t.Helper()

	if structType.NumField() != len(want) {
		t.Fatalf("%s 字段数量错误：got %d, want %d", structType.Name(), structType.NumField(), len(want))
	}
	for index, name := range want {
		if got := structType.Field(index).Name; got != name {
			t.Fatalf("%s 第 %d 个字段错误：got %s, want %s", structType.Name(), index, got, name)
		}
	}
}
