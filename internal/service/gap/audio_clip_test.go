package gap

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/models"
)

// TestAudioClipServiceCreatesExpiringControlledURL 验证回放 URL 不泄漏路径且过期后不可读取。
func TestAudioClipServiceCreatesExpiringControlledURL(t *testing.T) {
	root := t.TempDir()
	content := []byte("RIFF-controlled-gap")
	relative := "meetings/m-1/audio/gaps/a-1.wav"
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	now := time.Unix(100, 0)
	service, err := NewAudioClipService(AudioClipDependencies{
		Repository:    &clipRepositoryStub{asset: models.AudioAsset{ID: "a-1", MeetingID: "m-1", Kind: "gap", State: "ready", RelativePath: relative, SHA256: hex.EncodeToString(digest[:])}},
		WorkspaceRoot: root,
		Clock:         clock.NewFixed(now),
		RandomRead: func(target []byte) (int, error) {
			copy(target, []byte("0123456789abcdef"))
			return len(target), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	clip, err := service.Create(context.Background(), "m-1", "a-1")
	if err != nil {
		t.Fatal(err)
	}
	if clip.URL != "/media/gap-clips/"+base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef")) || clip.ExpiresAt <= now.UnixMilli() {
		t.Fatalf("受控 URL 不正确：%+v", clip)
	}
	request := httptest.NewRequest(http.MethodGet, clip.URL, nil)
	response := httptest.NewRecorder()
	service.ServeHTTP(response, request)
	if response.Code != http.StatusOK || string(response.Body.Bytes()) != string(content) {
		t.Fatalf("回放响应不正确：code=%d body=%q", response.Code, response.Body.String())
	}
}

// TestAudioClipServiceRejectsTamperedAsset 验证派生文件被篡改时不签发可用 token。
func TestAudioClipServiceRejectsTamperedAsset(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "meetings/m-1/audio/gaps/a-1.wav")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewAudioClipService(AudioClipDependencies{
		Repository:    &clipRepositoryStub{asset: models.AudioAsset{ID: "a-1", MeetingID: "m-1", Kind: "gap", State: "ready", RelativePath: "meetings/m-1/audio/gaps/a-1.wav", SHA256: "deadbeef"}},
		WorkspaceRoot: root, Clock: clock.NewFixed(time.Unix(100, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), "m-1", "a-1"); err == nil {
		t.Fatal("篡改文件不应签发回放 token")
	}
}

type clipRepositoryStub struct{ asset models.AudioAsset }

// GetReadyGapAsset 返回测试固定派生音频。
func (stub *clipRepositoryStub) GetReadyGapAsset(context.Context, string, string) (models.AudioAsset, error) {
	return stub.asset, nil
}
