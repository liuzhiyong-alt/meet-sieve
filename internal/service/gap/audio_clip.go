package gap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/models"
)

const gapClipTTL = 10 * time.Minute

// AudioClipRepository 读取仍处于 ready 的派生 gap 音频事实。
type AudioClipRepository interface {
	GetReadyGapAsset(context.Context, string, string) (models.AudioAsset, error)
}

// AudioClipDependencies 描述受控 gap 回放的事实、根目录与 token 依赖。
type AudioClipDependencies struct {
	Repository    AudioClipRepository
	WorkspaceRoot string
	Clock         clock.Clock
	RandomRead    func([]byte) (int, error)
}

// AudioClipResult 只向前端返回短期 URL 和到期时间。
type AudioClipResult struct {
	URL       string
	ExpiresAt int64
}

type audioClipEntry struct {
	path      string
	createdAt time.Time
	expiresAt time.Time
}

// AudioClipService 为已保留的冲突派生音频签发短期内存 token。
type AudioClipService struct {
	repository AudioClipRepository
	workspace  string
	clock      clock.Clock
	randomRead func([]byte) (int, error)
	mu         sync.Mutex
	entries    map[[32]byte]audioClipEntry
}

// NewAudioClipService 创建 gap 回放服务；构造阶段不访问文件或数据库。
func NewAudioClipService(dependencies AudioClipDependencies) (*AudioClipService, error) {
	if dependencies.Repository == nil || dependencies.Clock == nil || !filepath.IsAbs(dependencies.WorkspaceRoot) {
		return nil, fmt.Errorf("gap audio clip 依赖无效")
	}
	randomRead := dependencies.RandomRead
	if randomRead == nil {
		randomRead = rand.Read
	}
	return &AudioClipService{
		repository: dependencies.Repository, workspace: dependencies.WorkspaceRoot,
		clock: dependencies.Clock, randomRead: randomRead, entries: make(map[[32]byte]audioClipEntry),
	}, nil
}

// Create 校验数据库状态、受控路径和 SHA 后签发十分钟 URL。
func (service *AudioClipService) Create(ctx context.Context, meetingID string, assetID string) (AudioClipResult, error) {
	if service == nil || meetingID == "" || assetID == "" {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("gap.clip.validate"))
	}
	asset, err := service.repository.GetReadyGapAsset(ctx, meetingID, assetID)
	if err != nil {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("gap.clip.asset"))
	}
	path, err := trustedAssetPath(service.workspace, asset.RelativePath)
	if err != nil {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("gap.clip.path"))
	}
	digest, err := filesystem.SHA256File(path)
	if err != nil || !decodedDigest(asset.SHA256) || !strings.EqualFold(digest, asset.SHA256) {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("gap.clip.integrity"))
	}
	return service.issue(path)
}

// ServeHTTP 只提供短期 token 对应文件的 GET/HEAD 与标准 Range。
func (service *AudioClipService) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(request.URL.Path, "/media/gap-clips/")
	if token == request.URL.Path || token == "" || strings.Contains(token, "/") {
		http.NotFound(response, request)
		return
	}
	entry, status := service.authorize(token)
	if status != http.StatusOK {
		http.Error(response, http.StatusText(status), status)
		return
	}
	file, err := os.Open(entry.path)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	defer file.Close()
	response.Header().Set("Content-Type", "audio/wav")
	http.ServeContent(response, request, "gap.wav", entry.createdAt, file)
}

// issue 生成 128-bit 随机 token，服务端仅保存 token 哈希。
func (service *AudioClipService) issue(path string) (AudioClipResult, error) {
	randomBytes := make([]byte, 16)
	if count, err := service.randomRead(randomBytes); err != nil || count != len(randomBytes) {
		return AudioClipResult{}, apperr.Dependency(apperr.CodeAudioClipUnavailable, err, apperr.WithOp("gap.clip.token"))
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(token))
	now := service.clock.Now()
	expiresAt := now.Add(gapClipTTL)
	service.mu.Lock()
	service.entries[hash] = audioClipEntry{path: path, createdAt: now, expiresAt: expiresAt}
	service.mu.Unlock()
	return AudioClipResult{URL: "/media/gap-clips/" + token, ExpiresAt: expiresAt.UnixMilli()}, nil
}

// authorize 校验 token 并清除过期的内存授权；不会删除审计保留的派生文件。
func (service *AudioClipService) authorize(token string) (audioClipEntry, int) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 16 {
		return audioClipEntry{}, http.StatusNotFound
	}
	hash := sha256.Sum256([]byte(token))
	service.mu.Lock()
	entry, exists := service.entries[hash]
	if exists && !service.clock.Now().Before(entry.expiresAt) {
		delete(service.entries, hash)
	}
	service.mu.Unlock()
	if !exists {
		return audioClipEntry{}, http.StatusNotFound
	}
	if !service.clock.Now().Before(entry.expiresAt) {
		return audioClipEntry{}, http.StatusGone
	}
	return entry, http.StatusOK
}

// decodedDigest 校验数据库 SHA 使用完整十六进制格式。
func decodedDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
