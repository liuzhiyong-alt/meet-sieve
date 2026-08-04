package wails_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	wailstransport "meet-sieve/internal/transport/wails"
)

// TestStep9QueryBindingExposesFrozenMethods 验证 Step 9 查询契约完整。
func TestStep9QueryBindingExposesFrozenMethods(t *testing.T) {
	typeOf := reflect.TypeOf((*wailstransport.QueryBinding)(nil))
	for _, method := range []string{"GetHome", "ListMeetings", "GetMeetingDetail", "ListTranscript", "ListMeetingContent", "GetInterruptedRecovery"} {
		if _, found := typeOf.MethodByName(method); !found {
			t.Fatalf("Step 9 QueryBinding 缺少方法 %s", method)
		}
	}
}

// TestStep9QueryDTOsDoNotExposePaths 验证普通查询 DTO 不提供文件系统通道。
func TestStep9QueryDTOsDoNotExposePaths(t *testing.T) {
	encoded, err := json.Marshal(struct {
		Detail  wailstransport.MeetingDetailDTO `json:"detail"`
		Content wailstransport.ContentPageDTO   `json:"content"`
	}{})
	if err != nil {
		t.Fatalf("序列化 Step 9 DTO 失败：%v", err)
	}
	for _, forbidden := range []string{"relative_path", "absolute_path", "workspace_path", "source_url", "access_token", "provider_header"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("Step 9 DTO 泄漏禁止字段 %q：%s", forbidden, encoded)
		}
	}
}

// TestStep9QueryDTOIncludesAdditivePrimaryAction 验证会议摘要以加法字段暴露稳定主动作。
func TestStep9QueryDTOIncludesAdditivePrimaryAction(t *testing.T) {
	dto := wailstransport.MeetingSummaryDTO{
		ID: "meeting-1", HighestStatus: "gap_conflict",
		PrimaryAction: wailstransport.MeetingPrimaryActionDTO{
			Kind: "resolve_gap", Label: "处理缺口", TargetID: "gap-1", Enabled: true,
		},
	}
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("序列化会议主动作失败：%v", err)
	}
	value := string(encoded)
	if !strings.Contains(value, `"primary_action"`) || !strings.Contains(value, `"target_id":"gap-1"`) {
		t.Fatalf("会议摘要缺少主动作加法字段：%s", value)
	}
	if strings.Contains(value, "#/") || strings.Contains(value, "relative_path") {
		t.Fatalf("会议主动作泄漏前端路由或路径：%s", value)
	}
}

// TestStep9LifecycleBindingsExposeFrozenMethods 验证删除、诊断和资源系统操作契约完整。
func TestStep9LifecycleBindingsExposeFrozenMethods(t *testing.T) {
	bindings := []struct {
		Type    reflect.Type
		Methods []string
	}{
		{reflect.TypeOf((*wailstransport.DeletionBinding)(nil)), []string{"PreviewRecordingDeletion", "DeleteRecording", "PreviewMeetingDeletion", "DeleteMeeting", "GetDeletionJob", "RetryDeletion"}},
		{reflect.TypeOf((*wailstransport.DiagnosticBinding)(nil)), []string{"StartStorageScan", "GetStorageScan", "ExportGlobalDiagnostic", "ExportMeetingDiagnostic", "OpenLogDirectory"}},
		{reflect.TypeOf((*wailstransport.ResourceBinding)(nil)), []string{"OpenResource", "RevealResource", "OpenExternalLink"}},
	}
	for _, binding := range bindings {
		for _, method := range binding.Methods {
			if _, found := binding.Type.MethodByName(method); !found {
				t.Fatalf("Step 9 Binding 缺少方法 %s", method)
			}
		}
	}
}

// TestStep9LifecycleDTOsDoNotExposePaths 验证生命周期 DTO 不暴露绝对/相对路径或原始 URL。
func TestStep9LifecycleDTOsDoNotExposePaths(t *testing.T) {
	encoded, err := json.Marshal(struct {
		Preview    wailstransport.DeletionPreviewDTO  `json:"preview"`
		Job        wailstransport.DeletionJobDTO      `json:"job"`
		Diagnostic wailstransport.DiagnosticExportDTO `json:"diagnostic"`
		Resource   wailstransport.ResourceOpenDTO     `json:"resource"`
	}{})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"relative_path", "absolute_path", "target_path", "source_url", "manifest_json"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("生命周期 DTO 泄漏禁止字段 %q：%s", forbidden, encoded)
		}
	}
}
