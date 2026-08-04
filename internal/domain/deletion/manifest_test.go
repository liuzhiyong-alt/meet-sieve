package deletion

import (
	"os"
	"path/filepath"
	"testing"
)

// TestManifestStrictDecode 验证清单拒绝未知字段和路径逃逸。
func TestManifestStrictDecode(t *testing.T) {
	valid := `{"version":1,"meeting_id":"meeting-a","meeting_no":"20260803-A7K2-01","kind":"meeting","revision":7,"items":[{"id":"item-1","relative_path":"resources/a.txt","type":"file","size_bytes":3,"known":true}],"digest":"` + string(make([]byte, 64)) + `"}`
	if _, err := Decode([]byte(valid)); err == nil {
		t.Fatal("非法 digest 不应通过")
	}
	unknown := `{"version":1,"meeting_id":"meeting-a","meeting_no":"20260803-A7K2-01","kind":"meeting","revision":7,"items":[],"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","extra":true}`
	if _, err := Decode([]byte(unknown)); err == nil {
		t.Fatal("未知字段不应通过")
	}
	escape := Manifest{Version: 1, MeetingID: "meeting-a", MeetingNo: "20260803-A7K2-01", Kind: KindMeeting, Revision: 1, Items: []Item{{ID: "item-1", RelativePath: "../outside", Type: ItemFile}}}
	if _, err := Encode(escape); err == nil {
		t.Fatal("路径逃逸不应通过")
	}
}

// TestScannerDoesNotFollowSymlink 验证扫描器只登记符号链接本身。
func TestScannerDoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	items, err := Scan(root, nil)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(items) != 1 || items[0].Type != ItemSymlink || items[0].RelativePath != "external" {
		t.Fatalf("符号链接扫描结果错误: %+v", items)
	}
}
