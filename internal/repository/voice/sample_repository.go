// Package voice 提供声纹样本与 embedding 的 SQLite 持久化操作。
package voice

import (
	"context"
	"errors"
	"fmt"

	"meet-sieve/models"

	"gorm.io/gorm"
)

const voiceSampleColumns = "id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256, source_kind, source_name, request_id, source_meeting_id, source_utterance_id, environment_kind, processing_state, quality_state, quality_code, quality_metrics_json, last_error_code, created_at, updated_at"

// SampleRepository 负责声纹样本记录持久化，不决定文件或事务边界。
type SampleRepository struct {
	reader *gorm.DB
}

// NewSampleRepository 创建声纹样本 Repository。
func NewSampleRepository(reader *gorm.DB) *SampleRepository { return &SampleRepository{reader: reader} }

// CreatePending 在调用方事务内创建尚未参与匹配的样本记录。
func (repository *SampleRepository) CreatePending(ctx context.Context, tx *gorm.DB, sample models.VoiceSample) error {
	if tx == nil {
		return fmt.Errorf("创建 pending 声纹样本：事务不能为空")
	}
	if sample.ProcessingState != "processing" || sample.QualityState != "pending" {
		return fmt.Errorf("创建声纹样本时状态必须为 processing/pending")
	}
	if err := tx.WithContext(ctx).Create(&sample).Error; err != nil {
		return fmt.Errorf("创建 pending 声纹样本失败: %w", err)
	}
	return nil
}

// ListProcessing 返回需要在启动时检查文件状态的样本。
func (repository *SampleRepository) ListProcessing(ctx context.Context) ([]models.VoiceSample, error) {
	if repository == nil || repository.reader == nil {
		return nil, fmt.Errorf("查询 processing 声纹样本：数据库不能为空")
	}
	var samples []models.VoiceSample
	if err := repository.reader.WithContext(ctx).
		Select(voiceSampleColumns).
		Where("processing_state = ?", "processing").
		Order("created_at ASC").Order("id ASC").Find(&samples).Error; err != nil {
		return nil, fmt.Errorf("查询 processing 声纹样本失败: %w", err)
	}
	return samples, nil
}

// GetByID 返回指定声纹样本及存在状态。
func (repository *SampleRepository) GetByID(ctx context.Context, sampleID string) (models.VoiceSample, bool, error) {
	if repository == nil || repository.reader == nil {
		return models.VoiceSample{}, false, fmt.Errorf("查询声纹样本：数据库不能为空")
	}
	var sample models.VoiceSample
	err := repository.reader.WithContext(ctx).Select(voiceSampleColumns).Where("id = ?", sampleID).Take(&sample).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.VoiceSample{}, false, nil
	}
	if err != nil {
		return models.VoiceSample{}, false, fmt.Errorf("查询声纹样本失败: %w", err)
	}
	return sample, true, nil
}

// GetByRequest 返回会议片段录入的幂等样本。
func (repository *SampleRepository) GetByRequest(ctx context.Context, requestID string) (models.VoiceSample, bool, error) {
	if repository == nil || repository.reader == nil || requestID == "" {
		return models.VoiceSample{}, false, fmt.Errorf("查询声纹录入请求：参数无效")
	}
	var sample models.VoiceSample
	err := repository.reader.WithContext(ctx).Select(voiceSampleColumns).Where("request_id = ?", requestID).Take(&sample).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.VoiceSample{}, false, nil
	}
	if err != nil {
		return models.VoiceSample{}, false, fmt.Errorf("查询声纹录入请求失败：%w", err)
	}
	return sample, true, nil
}

// ListByMember 返回成员的全部声纹样本，最近创建的排在前面。
func (repository *SampleRepository) ListByMember(ctx context.Context, memberID string) ([]models.VoiceSample, error) {
	if repository == nil || repository.reader == nil {
		return nil, fmt.Errorf("查询成员声纹样本：数据库不能为空")
	}
	var samples []models.VoiceSample
	err := repository.reader.WithContext(ctx).Select(voiceSampleColumns).
		Where("member_id = ?", memberID).Order("created_at DESC").Order("id ASC").Find(&samples).Error
	if err != nil {
		return nil, fmt.Errorf("查询成员声纹样本失败: %w", err)
	}
	return samples, nil
}

// CompleteAccepted 在同一事务中写入 embedding 并把样本标记为质量可用。
func (repository *SampleRepository) CompleteAccepted(ctx context.Context, tx *gorm.DB, sampleID string, metricsJSON string, embedding models.VoiceEmbedding) error {
	if tx == nil {
		return fmt.Errorf("完成声纹样本：事务不能为空")
	}
	if err := tx.WithContext(ctx).Create(&embedding).Error; err != nil {
		return fmt.Errorf("写入声纹 embedding 失败: %w", err)
	}
	result := tx.WithContext(ctx).Model(&models.VoiceSample{}).
		Where("id = ? AND processing_state = ?", sampleID, "processing").
		Updates(map[string]any{
			"processing_state": "ready", "quality_state": "accepted", "quality_code": nil,
			"quality_metrics_json": metricsJSON, "last_error_code": nil, "updated_at": embedding.UpdatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("完成声纹样本状态失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("待完成声纹样本不存在或状态已变化")
	}
	return nil
}

// CompleteRejected 保存质量指标与稳定原因，但不创建 embedding。
func (repository *SampleRepository) CompleteRejected(ctx context.Context, tx *gorm.DB, sampleID string, qualityCode string, metricsJSON string, updatedAt int64) error {
	if tx == nil {
		return fmt.Errorf("拒绝声纹样本：事务不能为空")
	}
	result := tx.WithContext(ctx).Model(&models.VoiceSample{}).
		Where("id = ? AND processing_state = ?", sampleID, "processing").
		Updates(map[string]any{
			"processing_state": "ready", "quality_state": "rejected", "quality_code": qualityCode,
			"quality_metrics_json": metricsJSON, "last_error_code": nil, "updated_at": updatedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("保存声纹质量拒绝状态失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("待拒绝声纹样本不存在或状态已变化")
	}
	return nil
}

// Delete 在调用方事务内删除样本，embedding 由外键级联。
func (repository *SampleRepository) Delete(ctx context.Context, tx *gorm.DB, sampleID string) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("删除声纹样本：事务不能为空")
	}
	result := tx.WithContext(ctx).Where("id = ?", sampleID).Delete(&models.VoiceSample{})
	if result.Error != nil {
		return false, fmt.Errorf("删除声纹样本失败: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// ListAcceptedMissingEmbedding 返回缺少当前模型四元组的已接受样本。
func (repository *SampleRepository) ListAcceptedMissingEmbedding(ctx context.Context, modelID string, modelVersion string, modelSHA string, dimension int) ([]models.VoiceSample, error) {
	if repository == nil || repository.reader == nil {
		return nil, fmt.Errorf("查询待重建声纹样本：数据库不能为空")
	}
	var samples []models.VoiceSample
	err := repository.reader.WithContext(ctx).Select(voiceSampleColumns).
		Where("quality_state = ? AND processing_state = ?", "accepted", "ready").
		Where(`NOT EXISTS (SELECT 1 FROM voice_embeddings e WHERE e.voice_sample_id = voice_samples.id
			AND e.model_id = ? AND e.model_version = ? AND e.model_sha256 = ? AND e.dimension = ?)`, modelID, modelVersion, modelSHA, dimension).
		Order("created_at ASC").Order("id ASC").Find(&samples).Error
	if err != nil {
		return nil, fmt.Errorf("查询待重建声纹样本失败: %w", err)
	}
	return samples, nil
}

// UpsertEmbedding 幂等写入一个样本的当前模型 embedding。
func (repository *SampleRepository) UpsertEmbedding(ctx context.Context, tx *gorm.DB, embedding models.VoiceEmbedding) error {
	if tx == nil {
		return fmt.Errorf("重建声纹 embedding：事务不能为空")
	}
	return tx.WithContext(ctx).Where(
		"voice_sample_id = ? AND model_id = ? AND model_version = ? AND model_sha256 = ? AND dimension = ?",
		embedding.VoiceSampleID, embedding.ModelID, embedding.ModelVersion, embedding.ModelSHA256, embedding.Dimension,
	).Assign(map[string]any{"embedding": embedding.Embedding, "updated_at": embedding.UpdatedAt}).FirstOrCreate(&embedding).Error
}

// DeleteNonCurrentEmbeddings 删除不属于当前模型四元组的旧向量。
func (repository *SampleRepository) DeleteNonCurrentEmbeddings(ctx context.Context, tx *gorm.DB, modelID string, modelVersion string, modelSHA string, dimension int) error {
	if tx == nil {
		return fmt.Errorf("清理旧声纹 embedding：事务不能为空")
	}
	return tx.WithContext(ctx).Where(
		"NOT (model_id = ? AND model_version = ? AND model_sha256 = ? AND dimension = ?)",
		modelID, modelVersion, modelSHA, dimension,
	).Delete(&models.VoiceEmbedding{}).Error
}

// MarkFailed 在调用方事务内把不可恢复样本排除出后续编码与匹配。
func (repository *SampleRepository) MarkFailed(ctx context.Context, tx *gorm.DB, sampleID string, errorCode string) error {
	if tx == nil {
		return fmt.Errorf("标记失败声纹样本：事务不能为空")
	}
	result := tx.WithContext(ctx).Model(&models.VoiceSample{}).
		Where("id = ? AND processing_state = ?", sampleID, "processing").
		Updates(map[string]any{"processing_state": "failed", "last_error_code": errorCode})
	if result.Error != nil {
		return fmt.Errorf("标记失败声纹样本失败: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("待标记声纹样本不存在或状态已变化")
	}
	return nil
}

// Exists 判断数据库是否登记指定样本 ID。
func (repository *SampleRepository) Exists(ctx context.Context, sampleID string) (bool, error) {
	if repository == nil || repository.reader == nil {
		return false, fmt.Errorf("检查声纹样本：数据库不能为空")
	}
	var count int64
	if err := repository.reader.WithContext(ctx).Model(&models.VoiceSample{}).
		Where("id = ?", sampleID).Limit(1).Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查声纹样本失败: %w", err)
	}
	return count == 1, nil
}
