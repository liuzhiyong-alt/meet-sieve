package diagnostics

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/database"
)

// TestExportGlobalUsesWhitelistAndSecondRedaction 验证诊断包不包含路径、凭据或业务文件。
func TestExportGlobalUsesWhitelistAndSecondRedaction(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "data", "meetings.db")
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(databasePath); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close(db)
	logRoot := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := `token=abc123 path=/Users/liu/private/file.txt id=11111111-1111-4111-8111-111111111111`
	if err := os.WriteFile(filepath.Join(logRoot, "app.jsonl"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewExportService(ExportDependencies{Reader: db, Health: health.NewRegistry(), WorkspaceRoot: root, LogRoot: logRoot, Now: func() time.Time { return time.Unix(100, 0) }})
	target := filepath.Join(t.TempDir(), "diagnostic.zip")
	result, err := service.ExportGlobal(context.Background(), target)
	if err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if result.FileName != "diagnostic.zip" || result.SizeBytes == 0 {
		t.Fatalf("导出结果错误：%+v", result)
	}
	archive, err := zip.OpenReader(target)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var combined strings.Builder
	for _, file := range archive.File {
		reader, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		combined.Write(content)
	}
	text := combined.String()
	for _, forbidden := range []string{"abc123", "/Users/liu", "11111111-1111-4111-8111-111111111111"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("诊断包泄漏敏感内容 %q", forbidden)
		}
	}
	if strings.Contains(text, "meetings.db") {
		t.Fatal("诊断包不得包含数据库文件内容或名称")
	}
}
