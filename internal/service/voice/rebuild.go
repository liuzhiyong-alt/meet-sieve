package voice

import (
	"context"
	"fmt"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	voicerepository "meet-sieve/internal/repository/voice"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RebuildProgress 描述可由数据库重新计算的重建进度快照。
type RebuildProgress struct {
	Total     int
	Completed int
	Failed    int
}

// RebuildDependencies 描述 embedding 重建需要的显式依赖。
type RebuildDependencies struct {
	Repository   *voicerepository.SampleRepository
	Files        *SampleFileStore
	Transactions *database.TransactionManager
	Encoder      EncoderProvider
	IDs          identity.Generator
	Clock        clock.Clock
	Progress     func(RebuildProgress)
}

// RebuildRunner 使用当前模型逐项重建历史 accepted 样本，不读取旧模型向量。
type RebuildRunner struct {
	repository   *voicerepository.SampleRepository
	files        *SampleFileStore
	transactions *database.TransactionManager
	encoder      EncoderProvider
	ids          identity.Generator
	clock        clock.Clock
	progress     func(RebuildProgress)
}

// NewRebuildRunner 创建可重复执行、可从已完成进度继续的重建器。
func NewRebuildRunner(dependencies RebuildDependencies) *RebuildRunner {
	return &RebuildRunner{
		repository: dependencies.Repository, files: dependencies.Files, transactions: dependencies.Transactions,
		encoder: dependencies.Encoder, ids: dependencies.IDs, clock: dependencies.Clock, progress: dependencies.Progress,
	}
}

// Run 为缺少当前模型向量的样本生成 embedding；全部成功后才清理旧向量。
func (runner *RebuildRunner) Run(ctx context.Context) (RebuildProgress, error) {
	if err := runner.validateDependencies(); err != nil {
		return RebuildProgress{}, err
	}
	encoder, err := runner.encoder()
	if err != nil || encoder == nil {
		return RebuildProgress{}, fmt.Errorf("声纹模型不可用，不能重建 embedding: %w", err)
	}
	model := encoder.ModelInfo()
	samples, err := runner.repository.ListAcceptedMissingEmbedding(ctx, model.ID, model.Version, model.SHA256, model.Dimension)
	if err != nil {
		return RebuildProgress{}, err
	}
	progress := RebuildProgress{Total: len(samples)}
	for _, sample := range samples {
		if err := ctx.Err(); err != nil {
			return progress, err
		}
		if err := runner.rebuildSample(ctx, encoder, model, sample); err != nil {
			progress.Failed++
			runner.publish(progress)
			continue
		}
		progress.Completed++
		runner.publish(progress)
	}
	if progress.Failed > 0 {
		return progress, fmt.Errorf("%d 个声纹样本重建失败", progress.Failed)
	}
	err = runner.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return runner.repository.DeleteNonCurrentEmbeddings(ctx, tx, model.ID, model.Version, model.SHA256, model.Dimension)
	})
	return progress, err
}

// rebuildSample 校验正式 WAV 后生成并幂等写入当前模型向量。
func (runner *RebuildRunner) rebuildSample(ctx context.Context, encoder port.VoiceEncoder, model port.ModelInfo, sample models.VoiceSample) error {
	wav, err := runner.files.ReadVerifiedWAV(sample)
	if err != nil {
		return err
	}
	normalized, err := NormalizeWAV(ctx, wav)
	if err != nil {
		return err
	}
	embedding, err := encoder.Encode(ctx, toAudioPCM(normalized.Samples))
	if err != nil {
		return err
	}
	blob, err := EncodeEmbeddingBlob(embedding, model.Dimension)
	if err != nil {
		return err
	}
	now := runner.clock.Now().UnixMilli()
	record := models.VoiceEmbedding{
		ID: runner.ids.New(), VoiceSampleID: sample.ID, ModelID: model.ID, ModelVersion: model.Version,
		ModelSHA256: model.SHA256, Dimension: model.Dimension, Embedding: blob, CreatedAt: now, UpdatedAt: now,
	}
	if uuid.Validate(record.ID) != nil {
		return fmt.Errorf("生成重建 embedding UUID 失败")
	}
	return runner.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return runner.repository.UpsertEmbedding(ctx, tx, record)
	})
}

// publish 只在调用方提供事件回调时发布低频逐样本进度。
func (runner *RebuildRunner) publish(progress RebuildProgress) {
	if runner.progress != nil {
		runner.progress(progress)
	}
}

// validateDependencies 在开始 I/O 前拒绝不完整装配。
func (runner *RebuildRunner) validateDependencies() error {
	if runner == nil || runner.repository == nil || runner.files == nil || runner.transactions == nil || runner.encoder == nil || runner.ids == nil || runner.clock == nil {
		return fmt.Errorf("声纹 embedding 重建依赖未初始化")
	}
	return nil
}
