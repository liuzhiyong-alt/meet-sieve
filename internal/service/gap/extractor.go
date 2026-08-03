package gap

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domainaudio "meet-sieve/internal/domain/audio"
	"meet-sieve/internal/infra/filesystem"
	"meet-sieve/models"
)

// ExtractionRepository 提供可信会议、源音频和派生资产登记。
type ExtractionRepository interface {
	GetMeeting(context.Context, string) (models.Meeting, error)
	ListSourceAudio(context.Context, string) ([]models.AudioAsset, error)
	RegisterGapAsset(context.Context, models.AudioAsset) (models.AudioAsset, error)
}

// LoadPlanningPCM 校验源音频并返回静音拆分所需的 int16 样本。
func (extractor *Extractor) LoadPlanningPCM(ctx context.Context, meetingID string) ([]int16, error) {
	if extractor == nil || extractor.repository == nil || meetingID == "" {
		return nil, fmt.Errorf("读取 gap 计划音频：参数无效")
	}
	assets, err := extractor.repository.ListSourceAudio(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	pcmBytes, err := extractor.loadContinuousPCM(assets)
	if err != nil {
		return nil, err
	}
	pcm := make([]int16, len(pcmBytes)/2)
	for index := range pcm {
		pcm[index] = int16(binary.LittleEndian.Uint16(pcmBytes[index*2 : index*2+2]))
	}
	return pcm, nil
}

// DeleteDerived 删除已终结 attempt 的派生文件；SQLite 状态由调用方 Repository 收敛。
func (extractor *Extractor) DeleteDerived(asset models.AudioAsset) error {
	if extractor == nil || asset.Kind != "gap" {
		return fmt.Errorf("删除 gap 派生音频：参数无效")
	}
	path, err := trustedAssetPath(extractor.workspace, asset.RelativePath)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 gap 派生音频失败：%w", err)
	}
	return nil
}

// Extractor 从 mixed 或连续 microphone 资产提取标准 gap WAV。
type Extractor struct {
	repository ExtractionRepository
	workspace  string
}

// NewExtractor 创建只允许访问可信工作目录的音频提取器。
func NewExtractor(repository ExtractionRepository, workspace string) *Extractor {
	return &Extractor{repository: repository, workspace: workspace}
}

// Extract 验证源哈希、写入 `.wav.part`、fsync、rename 后登记 ready 资产。
func (extractor *Extractor) Extract(ctx context.Context, meetingID string, assetID string, sliceStart int64, sliceEnd int64, now int64) (models.AudioAsset, string, error) {
	if extractor == nil || extractor.repository == nil || !filepath.IsAbs(extractor.workspace) || meetingID == "" || assetID == "" || sliceStart < 0 || sliceEnd <= sliceStart {
		return models.AudioAsset{}, "", fmt.Errorf("提取 gap 音频：参数无效")
	}
	meeting, err := extractor.repository.GetMeeting(ctx, meetingID)
	if err != nil {
		return models.AudioAsset{}, "", err
	}
	assets, err := extractor.repository.ListSourceAudio(ctx, meetingID)
	if err != nil {
		return models.AudioAsset{}, "", err
	}
	pcm, err := extractor.loadContinuousPCM(assets)
	if err != nil || sliceEnd > int64(len(pcm)/2) {
		return models.AudioAsset{}, "", fmt.Errorf("读取连续 gap 音频失败：%w", err)
	}
	header, err := domainaudio.EncodeCanonicalWAVHeader(sliceEnd - sliceStart)
	if err != nil {
		return models.AudioAsset{}, "", err
	}
	wav := append(header, pcm[sliceStart*2:sliceEnd*2]...)
	directory, err := trustedGapDirectory(extractor.workspace, meeting.RelativeDir)
	if err != nil {
		return models.AudioAsset{}, "", err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return models.AudioAsset{}, "", fmt.Errorf("创建 gap 音频目录失败：%w", err)
	}
	finalPath := filepath.Join(directory, assetID+".wav")
	if err := writeWAVPart(finalPath, wav); err != nil {
		return models.AudioAsset{}, "", err
	}
	digest := sha256.Sum256(wav)
	relativePath, err := filepath.Rel(extractor.workspace, finalPath)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		_ = os.Remove(finalPath)
		return models.AudioAsset{}, "", fmt.Errorf("gap 音频路径逃逸工作目录")
	}
	asset := models.AudioAsset{
		ID: assetID, MeetingID: meetingID, Kind: "gap", RelativePath: filepath.ToSlash(relativePath),
		StartSample: sliceStart, EndSample: sliceEnd, SampleRate: domainaudio.SampleRate,
		BitDepth: domainaudio.BitDepth, Channels: domainaudio.Channels, SizeBytes: int64(len(wav)),
		SHA256: hex.EncodeToString(digest[:]), State: "ready", CreatedAt: now, UpdatedAt: now,
	}
	registered, err := extractor.repository.RegisterGapAsset(ctx, asset)
	if err != nil {
		_ = os.Remove(finalPath)
		return models.AudioAsset{}, "", err
	}
	return registered, finalPath, nil
}

// loadContinuousPCM 优先接受单个 mixed，否则要求 microphone 覆盖从 0 开始且首尾连续。
func (extractor *Extractor) loadContinuousPCM(assets []models.AudioAsset) ([]byte, error) {
	if len(assets) == 0 {
		return nil, fmt.Errorf("没有 ready 源音频")
	}
	result := make([]byte, 0)
	expectedStart := int64(0)
	for index, asset := range assets {
		if asset.State != "ready" || asset.StartSample != expectedStart || asset.EndSample <= asset.StartSample {
			return nil, fmt.Errorf("源音频不连续")
		}
		path, err := trustedAssetPath(extractor.workspace, asset.RelativePath)
		if err != nil {
			return nil, err
		}
		if digest, err := filesystem.SHA256File(path); err != nil || digest != asset.SHA256 {
			return nil, fmt.Errorf("源音频 SHA 校验失败")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		pcm, err := domainaudio.DecodeCanonicalWAV(data)
		if err != nil {
			return nil, fmt.Errorf("源 WAV 样本范围无效：%w", err)
		}
		if int64(len(pcm)/2) != asset.EndSample-asset.StartSample {
			return nil, fmt.Errorf("源 WAV 样本范围与登记事实不一致")
		}
		result = append(result, pcm...)
		expectedStart = asset.EndSample
		if asset.Kind == "mixed" && (index != 0 || len(assets) != 1) {
			return nil, fmt.Errorf("mixed 源音频数量无效")
		}
	}
	return result, nil
}

// writeWAVPart 以同目录固定 `.part` 完成持久化后原子改名。
func writeWAVPart(finalPath string, content []byte) error {
	partPath := finalPath + ".part"
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建 gap WAV part 失败：%w", err)
	}
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(partPath)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("写入 gap WAV 失败：%w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("同步 gap WAV 失败：%w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭 gap WAV 失败：%w", err)
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		return fmt.Errorf("提交 gap WAV 失败：%w", err)
	}
	if err := filesystem.SyncDirectory(filepath.Dir(finalPath)); err != nil {
		return fmt.Errorf("同步 gap WAV 目录失败：%w", err)
	}
	committed = true
	return nil
}

// trustedGapDirectory 构造会议目录下固定 audio/gaps 目录。
func trustedGapDirectory(workspace string, relativeDirectory string) (string, error) {
	meetingDirectory, err := trustedAssetPath(workspace, relativeDirectory)
	if err != nil {
		return "", err
	}
	return filepath.Join(meetingDirectory, "audio", "gaps"), nil
}

// trustedAssetPath 把数据库相对路径限制在工作目录内。
func trustedAssetPath(workspace string, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(filepath.ToSlash(relative), "../") {
		return "", fmt.Errorf("音频相对路径不可信")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil || (target != root && !strings.HasPrefix(target, root+string(filepath.Separator))) {
		return "", fmt.Errorf("音频路径逃逸工作目录")
	}
	return target, nil
}
