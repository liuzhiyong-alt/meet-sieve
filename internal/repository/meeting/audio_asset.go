package meeting

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

var (
	// ErrAudioAssetConflict 表示同一会议分片序号已登记为不同文件事实。
	ErrAudioAssetConflict = errors.New("音频资产元数据冲突")
)

// CreateReadyAudioAsset 幂等登记已安全完成的音频文件，不接受 writing 占位记录。
func (repository *Repository) CreateReadyAudioAsset(ctx context.Context, asset models.AudioAsset) error {
	if repository == nil || repository.reader == nil || asset.State != "ready" {
		return fmt.Errorf("登记音频资产：依赖或资产状态无效")
	}
	existing, err := repository.findAudioAsset(ctx, asset.MeetingID, asset.Kind, asset.SequenceNo)
	if err == nil {
		return compareAudioAsset(existing, asset)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := repository.reader.WithContext(ctx).Create(&asset).Error; err == nil {
		return nil
	}
	// 并发恢复可能已经插入相同事实；重新读取后仅接受完全一致的文件元数据。
	existing, findErr := repository.findAudioAsset(ctx, asset.MeetingID, asset.Kind, asset.SequenceNo)
	if findErr != nil {
		return fmt.Errorf("登记完成音频资产失败: %w", findErr)
	}
	return compareAudioAsset(existing, asset)
}

// ListReadyMicrophoneAssets 按会议内序号返回可用于对账和合并的完成分片。
func (repository *Repository) ListReadyMicrophoneAssets(ctx context.Context, meetingID string) ([]models.AudioAsset, error) {
	return repository.listReadyAudioAssets(ctx, meetingID, "microphone")
}

// ListReadyMixedAssets 返回会议结束后已经校验并登记的完整录音。
func (repository *Repository) ListReadyMixedAssets(ctx context.Context, meetingID string) ([]models.AudioAsset, error) {
	return repository.listReadyAudioAssets(ctx, meetingID, "mixed")
}

// listReadyAudioAssets 按会议内序号读取指定种类的 ready 音频事实。
func (repository *Repository) listReadyAudioAssets(ctx context.Context, meetingID string, kind string) ([]models.AudioAsset, error) {
	if repository == nil || repository.reader == nil || meetingID == "" {
		return nil, fmt.Errorf("读取音频资产：依赖或会议 ID 无效")
	}
	var assets []models.AudioAsset
	if err := repository.reader.WithContext(ctx).Select(audioAssetColumns()).
		Where("meeting_id = ? AND kind = ? AND state = ?", meetingID, kind, "ready").
		Order("sequence_no ASC").Find(&assets).Error; err != nil {
		return nil, fmt.Errorf("读取完成音频资产失败: %w", err)
	}
	return assets, nil
}

// findAudioAsset 按数据库唯一业务键读取一条音频资产。
func (repository *Repository) findAudioAsset(ctx context.Context, meetingID string, kind string, sequenceNo int) (models.AudioAsset, error) {
	var asset models.AudioAsset
	err := repository.reader.WithContext(ctx).Select(audioAssetColumns()).
		Where("meeting_id = ? AND kind = ? AND sequence_no = ?", meetingID, kind, sequenceNo).Take(&asset).Error
	if err != nil {
		return models.AudioAsset{}, err
	}
	return asset, nil
}

// compareAudioAsset 验证重复登记指向同一份不可变文件事实。
func compareAudioAsset(existing models.AudioAsset, candidate models.AudioAsset) error {
	if existing.RelativePath != candidate.RelativePath || existing.StartSample != candidate.StartSample ||
		existing.EndSample != candidate.EndSample || existing.SampleRate != candidate.SampleRate ||
		existing.BitDepth != candidate.BitDepth || existing.Channels != candidate.Channels ||
		existing.SizeBytes != candidate.SizeBytes || existing.SHA256 != candidate.SHA256 || existing.State != candidate.State {
		return ErrAudioAssetConflict
	}
	return nil
}

// audioAssetColumns 返回音频资产读取使用的显式字段。
func audioAssetColumns() []string {
	return []string{
		"id", "meeting_id", "kind", "sequence_no", "relative_path", "start_sample", "end_sample",
		"sample_rate", "bit_depth", "channels", "size_bytes", "sha256", "state", "created_at", "updated_at",
	}
}
