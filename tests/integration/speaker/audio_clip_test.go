package speaker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	speakerrepository "meet-sieve/internal/repository/speaker"
	speakerservice "meet-sieve/internal/service/speaker"

	"gorm.io/gorm"
)

type clipAudioReader struct{}

// Read 返回请求范围等长 PCM。
func (clipAudioReader) Read(_ context.Context, _ string, start int64, end int64) ([]int16, error) {
	return make([]int16, end-start), nil
}

type mutableClock struct {
	mu    sync.Mutex
	value time.Time
}

// Now 返回测试控制的当前时间。
func (clock *mutableClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.value
}

// Add 推进测试时间，不依赖 sleep。
func (clock *mutableClock) Add(duration time.Duration) {
	clock.mu.Lock()
	clock.value = clock.value.Add(duration)
	clock.mu.Unlock()
}

// TestAudioClip_CreateServeRangeRevokeAndExpire 验证最小 WAV、Range、撤销和过期安全边界。
func TestAudioClip_CreateServeRangeRevokeAndExpire(t *testing.T) {
	db := prepareObserveDatabase(t)
	prepareReadyClipAsset(t, db)
	tempRoot := filepath.Join(t.TempDir(), "clips")
	clock := &mutableClock{value: time.Unix(100, 0)}
	service, err := speakerservice.NewAudioClipService(speakerservice.AudioClipDependencies{
		Repository: speakerrepository.NewRepository(db), Audio: clipAudioReader{}, TempRoot: tempRoot, Clock: clock,
		RandomRead: func(target []byte) (int, error) {
			for index := range target {
				target[index] = byte(index + 1)
			}
			return len(target), nil
		},
	})
	if err != nil {
		t.Fatalf("创建 AudioClipService 失败：%v", err)
	}
	clip, err := service.Create(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil || clip.StartSample != 0 || clip.EndSample != 4032 || !strings.HasPrefix(clip.URL, "/media/audio-clips/") {
		t.Fatalf("创建 audio clip 错误：clip=%+v err=%v", clip, err)
	}
	entries, err := os.ReadDir(tempRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("clip 临时文件错误：entries=%d err=%v", len(entries), err)
	}
	info, _ := entries[0].Info()
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("clip 权限必须为 0600：%o", info.Mode().Perm())
	}
	request := httptest.NewRequest(http.MethodGet, clip.URL, nil)
	request.Header.Set("Range", "bytes=0-3")
	response := httptest.NewRecorder()
	service.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Header().Get("Content-Type") != "audio/wav" || response.Header().Get("Cache-Control") != "no-store" || response.Body.String() != "RIFF" {
		t.Fatalf("Range 响应错误：code=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
	token := strings.TrimPrefix(clip.URL, "/media/audio-clips/")
	if err := service.Revoke(token); err != nil {
		t.Fatalf("撤销 clip 失败：%v", err)
	}
	assertClipStatus(t, service, http.MethodGet, clip.URL, http.StatusNotFound)

	clip, err = service.Create(context.Background(), "77777777-7777-4777-8777-777777777777")
	if err != nil {
		t.Fatalf("重建 clip 失败：%v", err)
	}
	clock.Add(11 * time.Minute)
	assertClipStatus(t, service, http.MethodGet, clip.URL, http.StatusGone)
	assertClipStatus(t, service, http.MethodPost, clip.URL, http.StatusMethodNotAllowed)
	assertClipStatus(t, service, http.MethodGet, "/media/audio-clips/../../recording.wav", http.StatusNotFound)
}

// prepareReadyClipAsset 写入只用于确定会议音频边界的 ready 资产事实。
func prepareReadyClipAsset(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`INSERT INTO audio_assets(id, meeting_id, kind, sequence_no, relative_path, start_sample, end_sample, sample_rate, bit_depth, channels, size_bytes, sha256, state, created_at, updated_at) VALUES ('abababab-abab-4bab-8bab-abababababab', '11111111-1111-4111-8111-111111111111', 'mixed', 1, 'meetings/observe/audio/mixed.wav', 0, 10000, 16000, 16, 1, 20044, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'ready', 0, 0)`).Error; err != nil {
		t.Fatalf("准备 ready clip asset 失败：%v", err)
	}
}

// assertClipStatus 验证 AssetServer 安全响应码。
func assertClipStatus(t *testing.T, service *speakerservice.AudioClipService, method string, path string, want int) {
	t.Helper()
	response := httptest.NewRecorder()
	service.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	if response.Code != want {
		t.Fatalf("clip HTTP 状态错误：method=%s path=%s got=%d want=%d", method, path, response.Code, want)
	}
}
