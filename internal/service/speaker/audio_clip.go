package speaker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
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
	speakerrepository "meet-sieve/internal/repository/speaker"
)

const (
	clipContextSamples = int64(4000)
	clipMaxSamples     = int64(120 * speakerSampleRate)
	clipTTL            = 10 * time.Minute
)

// AudioClipDependencies 描述短期回放片段的事实、音频、临时目录和随机源。
type AudioClipDependencies struct {
	Repository *speakerrepository.Repository
	Audio      EvidenceAudioReader
	TempRoot   string
	Clock      clock.Clock
	RandomRead func([]byte) (int, error)
}

// AudioClipResult 只向客户端返回短期 URL 和实际采样范围，不暴露文件路径。
type AudioClipResult struct {
	URL         string
	StartSample int64
	EndSample   int64
	ExpiresAt   int64
}

type audioClipEntry struct {
	path      string
	createdAt time.Time
	expiresAt time.Time
}

// AudioClipService 生成、撤销和提供受控最小 WAV。
type AudioClipService struct {
	repository *speakerrepository.Repository
	audio      EvidenceAudioReader
	tempRoot   string
	clock      clock.Clock
	randomRead func([]byte) (int, error)
	mu         sync.Mutex
	entries    map[[32]byte]audioClipEntry
}

// NewAudioClipService 创建服务并清理上次进程遗留的受控 `.wav` 临时文件。
func NewAudioClipService(dependencies AudioClipDependencies) (*AudioClipService, error) {
	if dependencies.Repository == nil || dependencies.Audio == nil || dependencies.Clock == nil || !filepath.IsAbs(dependencies.TempRoot) {
		return nil, fmt.Errorf("audio clip 依赖无效")
	}
	randomRead := dependencies.RandomRead
	if randomRead == nil {
		randomRead = rand.Read
	}
	if err := os.MkdirAll(dependencies.TempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("创建 audio clip 临时目录失败：%w", err)
	}
	if err := cleanupClipDirectory(dependencies.TempRoot); err != nil {
		return nil, err
	}
	return &AudioClipService{
		repository: dependencies.Repository, audio: dependencies.Audio, tempRoot: dependencies.TempRoot,
		clock: dependencies.Clock, randomRead: randomRead, entries: make(map[[32]byte]audioClipEntry),
	}, nil
}

// Create 从原录音读取 utterance 及最多前后 250ms，生成十分钟短期回放 URL。
func (service *AudioClipService) Create(ctx context.Context, utteranceID string) (AudioClipResult, error) {
	if service == nil || utteranceID == "" {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("speaker.clip.validate"))
	}
	fact, err := service.repository.LoadAudioClipFact(ctx, utteranceID)
	if err != nil || fact.AudioEnd <= 0 || fact.Utterance.EndSample > fact.AudioEnd {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("speaker.clip.fact"))
	}
	start, end, err := clipRange(fact.Utterance.StartSample, fact.Utterance.EndSample, fact.AudioEnd)
	if err != nil {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("speaker.clip.range"))
	}
	samples, err := service.audio.Read(ctx, fact.Utterance.MeetingID, start, end)
	if err != nil {
		return AudioClipResult{}, apperr.Biz(apperr.CodeAudioClipUnavailable, apperr.WithOp("speaker.clip.audio"))
	}
	return service.persistClip(samples, start, end)
}

// Revoke 主动撤销 token 并删除对应临时文件；重复撤销幂等。
func (service *AudioClipService) Revoke(token string) error {
	hash, err := decodeClipToken(token)
	if err != nil {
		return apperr.Biz(apperr.CodeAudioClipExpired, apperr.WithOp("speaker.clip.revoke"))
	}
	service.mu.Lock()
	entry, exists := service.entries[hash]
	if exists {
		delete(service.entries, hash)
	}
	service.mu.Unlock()
	if exists {
		if removeErr := os.Remove(entry.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("撤销 audio clip 失败")
		}
	}
	return nil
}

// ServeHTTP 只提供 `/media/audio-clips/<token>` 的 GET/HEAD 与标准 Range。
func (service *AudioClipService) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.TrimPrefix(request.URL.Path, "/media/audio-clips/")
	if token == request.URL.Path || strings.Contains(token, "/") || token == "" {
		http.NotFound(response, request)
		return
	}
	hash, err := decodeClipToken(token)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	entry, status := service.authorizeClip(hash)
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
	http.ServeContent(response, request, "clip.wav", entry.createdAt, file)
}

// persistClip 写入 0600 WAV，并只以内存 SHA-256 保存 token 事实。
func (service *AudioClipService) persistClip(samples []int16, start int64, end int64) (AudioClipResult, error) {
	bytes := make([]byte, 16)
	if count, err := service.randomRead(bytes); err != nil || count != len(bytes) {
		return AudioClipResult{}, apperr.Dependency(apperr.CodeAudioClipUnavailable, err, apperr.WithOp("speaker.clip.token"))
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	hash := sha256.Sum256([]byte(token))
	path := filepath.Join(service.tempRoot, fmt.Sprintf("%x.wav", hash[:16]))
	if err := filesystem.WriteAtomic(path, encodeClipWAV(samples), 0o600); err != nil {
		return AudioClipResult{}, apperr.Dependency(apperr.CodeAudioClipUnavailable, err, apperr.WithOp("speaker.clip.write"))
	}
	now, expiresAt := service.clock.Now(), service.clock.Now().Add(clipTTL)
	service.mu.Lock()
	service.entries[hash] = audioClipEntry{path: path, createdAt: now, expiresAt: expiresAt}
	service.mu.Unlock()
	return AudioClipResult{URL: "/media/audio-clips/" + token, StartSample: start, EndSample: end, ExpiresAt: expiresAt.UnixMilli()}, nil
}

// authorizeClip 校验哈希和过期时间；过期时同步撤销临时文件。
func (service *AudioClipService) authorizeClip(hash [32]byte) (audioClipEntry, int) {
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
		_ = os.Remove(entry.path)
		return audioClipEntry{}, http.StatusGone
	}
	return entry, http.StatusOK
}

// clipRange 计算带上下文且不超过会议/120 秒边界的实际范围。
func clipRange(start int64, end int64, audioEnd int64) (int64, int64, error) {
	if start < 0 || end <= start || audioEnd < end || end-start > clipMaxSamples {
		return 0, 0, ErrAudioRangeInvalid
	}
	clipStart, clipEnd := maxInt64(0, start-clipContextSamples), minInt64(audioEnd, end+clipContextSamples)
	if clipEnd-clipStart > clipMaxSamples {
		clipStart, clipEnd = start, end
	}
	return clipStart, clipEnd, nil
}

// maxInt64 返回两个 int64 的较大值。
func maxInt64(left int64, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

// decodeClipToken 严格要求 128-bit URL-safe token，并返回仅存储的 SHA-256 key。
func decodeClipToken(token string) ([32]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 16 {
		return [32]byte{}, fmt.Errorf("audio clip token 无效")
	}
	return sha256.Sum256([]byte(token)), nil
}

// encodeClipWAV 生成 16kHz/16-bit/mono 标准 PCM WAV。
func encodeClipWAV(samples []int16) []byte {
	dataSize := len(samples) * 2
	result := make([]byte, canonicalWAVHeaderSize+dataSize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], speakerSampleRate)
	binary.LittleEndian.PutUint32(result[28:32], speakerSampleRate*2)
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], uint32(dataSize))
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(result[44+index*2:], uint16(sample))
	}
	return result
}

// cleanupClipDirectory 只删除受控根目录下本服务命名的普通 `.wav` 文件。
func cleanupClipDirectory(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("读取 audio clip 临时目录失败：%w", err)
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".wav" {
			if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
				return fmt.Errorf("清理遗留 audio clip 失败：%w", err)
			}
		}
	}
	return nil
}
