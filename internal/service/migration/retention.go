package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"meet-sieve/internal/infra/filesystem"
)

// RetentionResult 统计实际删除的已验证备份对及不影响升级结果的清理告警。
type RetentionResult struct {
	RemovedPairs int
	Warnings     []string
}

// PruneBackups 只删除超过保留数量的完整可验证备份对，任何未知或不完整文件均保留。
func PruneBackups(directory string, keep int) (RetentionResult, error) {
	if keep < 1 {
		return RetentionResult{}, fmt.Errorf("备份保留数量必须大于零")
	}
	pairs, err := loadVerifiedBackupPairs(directory)
	if err != nil {
		return RetentionResult{}, err
	}
	sort.Slice(pairs, func(left int, right int) bool {
		return pairs[left].createdAt.After(pairs[right].createdAt)
	})

	result := RetentionResult{}
	for _, pair := range pairs[keep:] {
		if err := removeBackupPair(pair); err != nil {
			result.Warnings = append(result.Warnings, "backup_retention_cleanup_failed")
			continue
		}
		result.RemovedPairs++
	}
	return result, nil
}

// verifiedBackupPair 是 manifest、数据库文件、哈希、版本和来源身份均一致的可轮转备份对。
type verifiedBackupPair struct {
	databasePath string
	manifestPath string
	createdAt    time.Time
}

// loadVerifiedBackupPairs 仅从 manifest 发现候选；没有 manifest 的数据库文件永远不参与清理。
func loadVerifiedBackupPairs(directory string) ([]verifiedBackupPair, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取备份目录失败: %w", err)
	}

	var pairs []verifiedBackupPair
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if pair, ok := verifyBackupPair(directory, entry.Name()); ok {
			pairs = append(pairs, pair)
		}
	}
	return pairs, nil
}

// verifyBackupPair 验证 manifest 所属文件名、大小、哈希、schema 与身份，不把错误对象暴露为删除依据。
func verifyBackupPair(directory string, manifestFile string) (verifiedBackupPair, bool) {
	manifestPath := filepath.Join(directory, manifestFile)
	manifest, err := ParseBackupManifestFile(manifestPath)
	if err != nil {
		return verifiedBackupPair{}, false
	}
	databasePath := filepath.Join(directory, manifest.DatabaseFile)
	info, err := os.Stat(databasePath)
	if err != nil || info.IsDir() || info.Size() != manifest.SizeBytes {
		return verifiedBackupPair{}, false
	}
	digest, err := filesystem.SHA256File(databasePath)
	if err != nil || digest != manifest.SHA256 {
		return verifiedBackupPair{}, false
	}

	source := backupSource{version: manifest.FromVersion, sourceKind: manifest.SourceKind, databaseID: manifest.DatabaseID}
	if err := verifySnapshotDatabase(databasePath, source); err != nil {
		return verifiedBackupPair{}, false
	}
	createdAt, err := time.Parse(time.RFC3339, manifest.CreatedAtUTC)
	if err != nil {
		return verifiedBackupPair{}, false
	}
	return verifiedBackupPair{databasePath: databasePath, manifestPath: manifestPath, createdAt: createdAt}, true
}

// removeBackupPair 只删除已经在本次调用中完成验证的成对文件。
func removeBackupPair(pair verifiedBackupPair) error {
	if err := os.Remove(pair.databasePath); err != nil {
		return err
	}
	if err := os.Remove(pair.manifestPath); err != nil {
		return err
	}
	return nil
}
