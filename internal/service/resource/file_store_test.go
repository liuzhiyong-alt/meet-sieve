package resource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileStore_OpenRejectsTraversalAndSymlink 验证下载只打开会议 resources 中的普通文件。
func TestFileStore_OpenRejectsTraversalAndSymlink(t *testing.T) {
	t.Parallel()

	meetingDirectory := t.TempDir()
	resources := filepath.Join(meetingDirectory, "resources")
	if err := os.MkdirAll(resources, 0o700); err != nil {
		t.Fatalf("创建 resources 失败：%v", err)
	}
	target := filepath.Join(resources, "11111111-1111-4111-8111-111111111111.txt")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
	store := NewFileStore()
	file, _, err := store.Open(meetingDirectory, "resources/11111111-1111-4111-8111-111111111111.txt")
	if err != nil {
		t.Fatalf("安全文件无法打开：%v", err)
	}
	_ = file.Close()
	for _, invalid := range []string{"../secret", "/tmp/secret", "resources/../../secret"} {
		if file, _, err := store.Open(meetingDirectory, invalid); err == nil {
			_ = file.Close()
			t.Fatalf("越界路径被打开：%q", invalid)
		}
	}
	symlink := filepath.Join(resources, "22222222-2222-4222-8222-222222222222.txt")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatalf("创建 symlink 失败：%v", err)
	}
	if file, _, err := store.Open(meetingDirectory, "resources/22222222-2222-4222-8222-222222222222.txt"); err == nil {
		_ = file.Close()
		t.Fatal("symlink 附件不应被打开")
	}
}

// TestServeDownload_SupportsHeadAndSingleRange 验证 GET/HEAD/单 Range 契约且拒绝多段 Range。
func TestServeDownload_SupportsHeadAndSingleRange(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatalf("写入下载文件失败：%v", err)
	}
	for _, test := range []struct {
		method      string
		rangeHeader string
		wantStatus  int
		wantBody    string
	}{
		{method: http.MethodGet, wantStatus: http.StatusOK, wantBody: "0123456789"},
		{method: http.MethodHead, wantStatus: http.StatusOK},
		{method: http.MethodGet, rangeHeader: "bytes=2-5", wantStatus: http.StatusPartialContent, wantBody: "2345"},
		{method: http.MethodGet, rangeHeader: "bytes=0-1,4-5", wantStatus: http.StatusRequestedRangeNotSatisfiable},
	} {
		t.Run(test.method+test.rangeHeader, func(t *testing.T) {
			t.Parallel()
			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("打开下载文件失败：%v", err)
			}
			defer file.Close()
			request := httptest.NewRequest(test.method, "/attachment", nil)
			request.Header.Set("Range", test.rangeHeader)
			recorder := httptest.NewRecorder()
			ServeDownload(recorder, request, file, "设计\r\n稿.txt", "text/plain")
			if recorder.Code != test.wantStatus || recorder.Body.String() != test.wantBody {
				t.Fatalf("下载响应不正确：status=%d body=%q", recorder.Code, recorder.Body.String())
			}
			if strings.ContainsAny(recorder.Header().Get("Content-Disposition"), "\r\n") {
				t.Fatal("Content-Disposition 包含 CR/LF")
			}
		})
	}
}

// TestRecovery_RemovesOnlyOwnedOrphans 验证恢复只删除 `.part` 和未入库 UUID 候选，保留用户文件。
func TestRecovery_RemovesOnlyOwnedOrphans(t *testing.T) {
	t.Parallel()

	meetingDirectory := t.TempDir()
	resources := filepath.Join(meetingDirectory, "resources")
	staging := filepath.Join(resources, ".staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatalf("创建恢复目录失败：%v", err)
	}
	part := filepath.Join(staging, "11111111-1111-4111-8111-111111111111.part")
	orphan := filepath.Join(resources, "22222222-2222-4222-8222-222222222222.pdf")
	referenced := filepath.Join(resources, "33333333-3333-4333-8333-333333333333.pdf")
	userFile := filepath.Join(resources, "my-notes.pdf")
	for _, path := range []string{part, orphan, referenced, userFile} {
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			t.Fatalf("写入恢复 fixture 失败：%v", err)
		}
	}
	recovery := NewRecovery(func(context.Context, string) (map[string]struct{}, error) {
		return map[string]struct{}{"33333333-3333-4333-8333-333333333333.pdf": {}}, nil
	})
	if err := recovery.RecoverMeeting(context.Background(), "meeting", meetingDirectory); err != nil {
		t.Fatalf("恢复附件目录失败：%v", err)
	}
	for _, removed := range []string{part, orphan} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("应删除的应用孤儿仍存在：%s", removed)
		}
	}
	for _, kept := range []string{referenced, userFile} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("不应删除的文件丢失：%s err=%v", kept, err)
		}
	}
}
