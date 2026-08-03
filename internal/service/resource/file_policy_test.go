package resource

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// TestValidateDeclaredSize_UsesExact500MiB 验证附件硬上限精确为 524,288,000 bytes。
func TestValidateDeclaredSize_UsesExact500MiB(t *testing.T) {
	t.Parallel()

	if MaxAttachmentBytes != 524_288_000 {
		t.Fatalf("附件上限=%d, want 524288000", MaxAttachmentBytes)
	}
	if err := ValidateDeclaredSize(MaxAttachmentBytes); err != nil {
		t.Fatalf("精确 500 MiB 被拒绝：%v", err)
	}
	if err := ValidateDeclaredSize(MaxAttachmentBytes + 1); err == nil {
		t.Fatal("500 MiB + 1 byte 必须被拒绝")
	}
}

// TestCopyExactAndHash_RejectsLimitPlusOne 验证实际读取使用 limit+1 而不信任声明大小。
func TestCopyExactAndHash_RejectsLimitPlusOne(t *testing.T) {
	t.Parallel()

	if _, err := copyExactAndHash(context.Background(), io.Discard, bytes.NewReader([]byte("12345678")), 8, 8); err != nil {
		t.Fatalf("精确长度流式复制失败：%v", err)
	}
	if _, err := copyExactAndHash(context.Background(), io.Discard, bytes.NewReader([]byte("123456789")), 8, 8); err == nil {
		t.Fatal("limit+1 内容必须被拒绝")
	}
	if _, err := copyExactAndHash(context.Background(), io.Discard, bytes.NewReader([]byte("1234567")), 8, 8); err == nil {
		t.Fatal("实际大小与声明大小不一致必须被拒绝")
	}
}

// TestFilePolicy_BlocksExecutableFormats 验证扩展名、magic 和 shebang 任一命中即拒绝。
func TestFilePolicy_BlocksExecutableFormats(t *testing.T) {
	t.Parallel()

	policy := NewFilePolicy()
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "setup.exe", content: []byte("ordinary")},
		{name: "renamed.txt", content: append([]byte("MZ"), make([]byte, 20)...)},
		{name: "script.txt", content: []byte("#!/bin/sh\necho unsafe")},
		{name: "binary", content: []byte{0x7f, 'E', 'L', 'F', 2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := policy.Validate(tt.name, "application/octet-stream", tt.content); err == nil {
				t.Fatalf("危险附件被接受：%s", tt.name)
			}
		})
	}
	if result, err := policy.Validate("design.pdf", "application/pdf", []byte("%PDF-1.7\n")); err != nil || result.Extension != ".pdf" {
		t.Fatalf("普通文档被拒绝：result=%#v err=%v", result, err)
	}
}

// TestUploadCoordinator_EnforcesSessionAndGlobalLimits 验证同 session 一个、全局三个活动上传。
func TestUploadCoordinator_EnforcesSessionAndGlobalLimits(t *testing.T) {
	t.Parallel()

	coordinator := NewUploadCoordinator()
	first, err := coordinator.Reserve(context.Background(), "meeting", "session-1", "request-1")
	if err != nil {
		t.Fatalf("占用第一个上传失败：%v", err)
	}
	defer first.Release()
	if _, err := coordinator.Reserve(context.Background(), "meeting", "session-1", "request-2"); err == nil {
		t.Fatal("同 session 第二个上传必须被拒绝")
	}
	second, _ := coordinator.Reserve(context.Background(), "meeting", "session-2", "request-2")
	third, _ := coordinator.Reserve(context.Background(), "meeting", "session-3", "request-3")
	defer second.Release()
	defer third.Release()
	if _, err := coordinator.Reserve(context.Background(), "meeting", "session-4", "request-4"); err == nil {
		t.Fatal("全局第四个上传必须被拒绝")
	}
}

// TestUploadCoordinator_CancelMeetingCancelsActiveContexts 验证会议结束立即取消本场所有上传。
func TestUploadCoordinator_CancelMeetingCancelsActiveContexts(t *testing.T) {
	t.Parallel()

	coordinator := NewUploadCoordinator()
	reservation, err := coordinator.Reserve(context.Background(), "meeting", "session", "request")
	if err != nil {
		t.Fatalf("占用上传失败：%v", err)
	}
	defer reservation.Release()
	coordinator.CancelMeeting("meeting")
	select {
	case <-reservation.Context().Done():
	default:
		t.Fatal("会议取消未传递到上传 context")
	}
}

// TestUploadCoordinator_Snapshot 验证宿主只读取当前会议的真实活动上传进度。
func TestUploadCoordinator_Snapshot(t *testing.T) {
	t.Parallel()

	coordinator := NewUploadCoordinator()
	reservation, err := coordinator.ReserveAttachment(context.Background(), "meeting", "session", "request", "资料.pdf", 100)
	if err != nil {
		t.Fatalf("占用附件上传失败：%v", err)
	}
	reservation.ReportProgress(40)
	snapshot := coordinator.Snapshot("meeting")
	if len(snapshot) != 1 || snapshot[0].Written != 40 || snapshot[0].Total != 100 || snapshot[0].Name != "资料.pdf" {
		t.Fatalf("活动上传投影不正确：%+v", snapshot)
	}
	reservation.Release()
	if remaining := coordinator.Snapshot("meeting"); len(remaining) != 0 {
		t.Fatalf("上传释放后仍有活动投影：%+v", remaining)
	}
}
