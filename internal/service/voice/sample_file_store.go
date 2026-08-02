package voice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"
	voicerepository "meet-sieve/internal/repository/voice"
	"meet-sieve/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SampleFileStore 协调 staging 文件、pending 数据库行和正式样本路径。
type SampleFileStore struct {
	root         string
	repository   *voicerepository.SampleRepository
	transactions *database.TransactionManager
}

// NewSampleFileStore 创建声纹样本文件与 pending 记录协调器。
func NewSampleFileStore(root string, repository *voicerepository.SampleRepository, transactions *database.TransactionManager) *SampleFileStore {
	return &SampleFileStore{root: filepath.Clean(root), repository: repository, transactions: transactions}
}

// PersistPending 先同步 staging 文件，再创建 pending 行，最后原子移动到正式路径。
func (store *SampleFileStore) PersistPending(ctx context.Context, sample models.VoiceSample, wav []byte) error {
	if store == nil || store.repository == nil || store.transactions == nil || store.root == "" {
		return fmt.Errorf("声纹样本文件服务依赖未初始化")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verifyPendingWAV(sample, wav); err != nil {
		return err
	}
	finalPath, err := store.resolveFinalPath(sample.RelativePath)
	if err != nil {
		return err
	}
	stagingDir := filepath.Join(store.root, "data", "voice-samples", ".staging", sample.ID)
	stagingPath := filepath.Join(stagingDir, "sample.wav")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return fmt.Errorf("创建声纹 staging 目录失败: %w", err)
	}
	if err := filesystem.WriteAtomic(stagingPath, wav, 0o600); err != nil {
		return fmt.Errorf("写入声纹 staging 文件失败: %w", err)
	}
	if err := store.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return store.repository.CreatePending(ctx, tx, sample)
	}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return fmt.Errorf("创建声纹正式目录失败: %w", err)
	}
	if err := os.Rename(stagingPath, finalPath); err != nil {
		return fmt.Errorf("移动声纹正式文件失败: %w", err)
	}
	_ = os.Remove(stagingDir)
	return nil
}

// RecoverPending 完成已提交 pending 行对应的 staging 原子移动，供应用启动恢复调用。
func (store *SampleFileStore) RecoverPending(ctx context.Context) error {
	if store == nil || store.repository == nil || store.root == "" {
		return fmt.Errorf("声纹样本文件服务依赖未初始化")
	}
	samples, err := store.repository.ListProcessing(ctx)
	if err != nil {
		return err
	}
	for _, sample := range samples {
		if err := store.recoverSample(ctx, sample); err != nil {
			return err
		}
	}
	return nil
}

// CleanupOrphanedStaging 删除超过截止时间且数据库无对应行的受控 UUID staging 目录。
func (store *SampleFileStore) CleanupOrphanedStaging(ctx context.Context, olderThan time.Time) error {
	if store == nil || store.repository == nil || store.root == "" {
		return fmt.Errorf("声纹样本文件服务依赖未初始化")
	}
	stagingRoot := filepath.Join(store.root, "data", "voice-samples", ".staging")
	entries, err := os.ReadDir(stagingRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取声纹 staging 目录失败: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || uuid.Validate(entry.Name()) != nil {
			continue
		}
		path := filepath.Join(stagingRoot, entry.Name())
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(olderThan) {
			continue
		}
		exists, err := store.repository.Exists(ctx, entry.Name())
		if err != nil {
			return err
		}
		if !exists {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("清理孤立声纹 staging 失败: %w", err)
			}
		}
	}
	return nil
}

// DeleteSample 先把正式 WAV 移入受控 trash，再以事务删除数据库记录。
func (store *SampleFileStore) DeleteSample(ctx context.Context, sample models.VoiceSample) error {
	if store == nil || store.repository == nil || store.transactions == nil {
		return fmt.Errorf("声纹样本文件服务依赖未初始化")
	}
	finalPath, err := store.resolveFinalPath(sample.RelativePath)
	if err != nil {
		return err
	}
	if !fileMatchesSample(finalPath, sample) {
		return apperr.Biz(apperr.CodeVoiceSampleFileInvalid, apperr.WithOp("voice.sample.delete.verify"))
	}
	trashDir := filepath.Join(store.root, "data", "voice-samples", ".trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return fmt.Errorf("创建声纹 trash 目录失败: %w", err)
	}
	trashPath := filepath.Join(trashDir, sample.ID+".wav")
	if err := os.Rename(finalPath, trashPath); err != nil {
		return fmt.Errorf("移动声纹样本到 trash 失败: %w", err)
	}
	deleted := false
	err = store.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var deleteErr error
		deleted, deleteErr = store.repository.Delete(ctx, tx, sample.ID)
		return deleteErr
	})
	if err != nil || !deleted {
		restoreErr := os.Rename(trashPath, finalPath)
		if restoreErr != nil {
			return apperr.Dependency(apperr.CodeVoiceSampleDeleteFailed, restoreErr, apperr.WithOp("voice.sample.delete.restore"))
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("待删除声纹样本不存在")
	}
	if err := os.Remove(trashPath); err != nil && !os.IsNotExist(err) {
		return apperr.Dependency(apperr.CodeVoiceSampleDeleteFailed, err, apperr.WithOp("voice.sample.delete.cleanup"))
	}
	return nil
}

// ReadVerifiedWAV 读取已登记样本，并在返回前复验大小和 SHA-256。
func (store *SampleFileStore) ReadVerifiedWAV(sample models.VoiceSample) ([]byte, error) {
	path, err := store.resolveFinalPath(sample.RelativePath)
	if err != nil {
		return nil, err
	}
	if !fileMatchesSample(path, sample) {
		return nil, apperr.Biz(apperr.CodeVoiceSampleFileInvalid, apperr.WithOp("voice.sample.read"))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取声纹 WAV 失败: %w", err)
	}
	return content, nil
}

// RecoverTrash 恢复数据库仍存在的删除现场，并清理已提交删除的孤立 trash。
func (store *SampleFileStore) RecoverTrash(ctx context.Context) error {
	trashDir := filepath.Join(store.root, "data", "voice-samples", ".trash")
	entries, err := os.ReadDir(trashDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取声纹 trash 目录失败: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".wav" {
			continue
		}
		sampleID := strings.TrimSuffix(entry.Name(), ".wav")
		if uuid.Validate(sampleID) != nil {
			continue
		}
		trashPath := filepath.Join(trashDir, entry.Name())
		sample, exists, err := store.repository.GetByID(ctx, sampleID)
		if err != nil {
			return err
		}
		if !exists {
			if err := os.Remove(trashPath); err != nil {
				return fmt.Errorf("清理已提交删除的 trash 失败: %w", err)
			}
			continue
		}
		finalPath, err := store.resolveFinalPath(sample.RelativePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
			return err
		}
		if _, err := os.Stat(finalPath); os.IsNotExist(err) {
			if !fileMatchesSample(trashPath, sample) {
				return apperr.Biz(apperr.CodeVoiceSampleFileInvalid, apperr.WithOp("voice.sample.trash_recovery"))
			}
			if err := os.Rename(trashPath, finalPath); err != nil {
				return fmt.Errorf("恢复 trash 声纹样本失败: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("检查声纹正式文件失败: %w", err)
		} else if fileMatchesSample(finalPath, sample) {
			// 正式文件已经恢复时，trash 是上次清理中断留下的重复副本。
			if err := os.Remove(trashPath); err != nil {
				return fmt.Errorf("清理重复声纹 trash 失败: %w", err)
			}
		} else {
			return apperr.Biz(apperr.CodeVoiceSampleFileInvalid, apperr.WithOp("voice.sample.trash_conflict"))
		}
	}
	return nil
}

// recoverSample 校验正式文件或 staging 文件，并仅对校验通过的内容执行移动。
func (store *SampleFileStore) recoverSample(ctx context.Context, sample models.VoiceSample) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	finalPath, err := store.resolveFinalPath(sample.RelativePath)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(finalPath); statErr == nil {
		if fileMatchesSample(finalPath, sample) {
			return nil
		}
		return store.markInvalid(ctx, sample.ID)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("检查声纹正式文件失败: %w", statErr)
	}
	stagingDir := filepath.Join(store.root, "data", "voice-samples", ".staging", sample.ID)
	stagingPath := filepath.Join(stagingDir, "sample.wav")
	if !fileMatchesSample(stagingPath, sample) {
		return store.markInvalid(ctx, sample.ID)
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return fmt.Errorf("创建声纹正式目录失败: %w", err)
	}
	if err := os.Rename(stagingPath, finalPath); err != nil {
		return fmt.Errorf("恢复声纹正式文件失败: %w", err)
	}
	_ = os.Remove(stagingDir)
	return nil
}

// markInvalid 持久化稳定错误码，使损坏样本不会阻断其他启动恢复任务。
func (store *SampleFileStore) markInvalid(ctx context.Context, sampleID string) error {
	return store.transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return store.repository.MarkFailed(ctx, tx, sampleID, apperr.CodeVoiceSampleFileInvalid.ErrorCode)
	})
}

// resolveFinalPath 限制数据库相对路径只能落在当前工作目录内。
func (store *SampleFileStore) resolveFinalPath(relativePath string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("声纹样本相对路径不安全")
	}
	return filepath.Join(store.root, clean), nil
}

// verifyPendingWAV 校验文件大小与正式 WAV 哈希，避免 pending 行指向错误内容。
func verifyPendingWAV(sample models.VoiceSample, wav []byte) error {
	if sample.SizeBytes != int64(len(wav)) || len(wav) == 0 {
		return fmt.Errorf("声纹 WAV 大小与样本记录不一致")
	}
	digest := sha256.Sum256(wav)
	if hex.EncodeToString(digest[:]) != sample.SHA256 {
		return fmt.Errorf("声纹 WAV SHA-256 与样本记录不一致")
	}
	return nil
}

// fileMatchesSample 使用大小与 SHA-256 校验磁盘文件是否对应数据库样本。
func fileMatchesSample(path string, sample models.VoiceSample) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != sample.SizeBytes {
		return false
	}
	digest, err := filesystem.SHA256File(path)
	return err == nil && digest == sample.SHA256
}
