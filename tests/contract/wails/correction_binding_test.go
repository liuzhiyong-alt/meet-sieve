package wails_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	wailstransport "meet-sieve/internal/transport/wails"
)

// TestCorrectionBinding_ExposesConfirmedCommands 验证 Step 5 Wails 公共方法完整且名称稳定。
func TestCorrectionBinding_ExposesConfirmedCommands(t *testing.T) {
	typeOf := reflect.TypeOf(&wailstransport.CorrectionBinding{})
	methods := []string{
		"GetSpeakerStatus", "ListCorrectionEntries", "GetCorrectionEntry", "CorrectUtteranceText", "CorrectUtteranceSpeaker",
		"CorrectSpeakerCluster", "CorrectResourceDescription", "CreateUtteranceAudioClip",
		"RevokeAudioClip", "AddUtteranceToVoiceSamples", "RetrySpeakerProcessing", "RetryRawRecordProjection",
	}
	for _, method := range methods {
		if _, exists := typeOf.MethodByName(method); !exists {
			t.Fatalf("CorrectionBinding 缺少方法：%s", method)
		}
	}
}

// TestCorrectionEntryDTO_DoesNotExposeSensitiveStorage 验证分页 DTO 不包含 embedding、路径、token hash 或 event payload。
func TestCorrectionEntryDTO_DoesNotExposeSensitiveStorage(t *testing.T) {
	encoded, err := json.Marshal(wailstransport.CorrectionEntryDTO{})
	if err != nil {
		t.Fatalf("序列化 CorrectionEntryDTO 失败：%v", err)
	}
	text := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"embedding", "relative_path", "absolute_path", "token_hash", "payload_json", "model_sha"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("CorrectionEntryDTO 泄漏敏感字段 %s：%s", forbidden, text)
		}
	}
}
