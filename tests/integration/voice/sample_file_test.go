package voice_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/database"
	voicerepository "meet-sieve/internal/repository/voice"
	voiceservice "meet-sieve/internal/service/voice"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestSampleFileStore_PersistsPendingThenMovesToFinalPath 验证 pending 行与正式 WAV 最终一致。
func TestSampleFileStore_PersistsPendingThenMovesToFinalPath(t *testing.T) {
	db := openVoiceDatabase(t)
	memberID := "11111111-1111-4111-8111-111111111111"
	if err := db.Exec(`INSERT INTO members (id,name,name_normalized,created_at,updated_at) VALUES (?, '测试成员', '测试成员', 0, 0)`, memberID).Error; err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	root := t.TempDir()
	store := voiceservice.NewSampleFileStore(root, voicerepository.NewSampleRepository(db), database.NewTransactionManager(db))
	wav := make([]byte, 46)
	digest := sha256.Sum256(wav)
	sample := models.VoiceSample{
		ID: "22222222-2222-4222-8222-222222222222", MemberID: memberID,
		RelativePath: "data/voice-samples/11111111-1111-4111-8111-111111111111/22222222-2222-4222-8222-222222222222.wav",
		DurationMS:   1, SampleRate: 16000, Channels: 1, BitDepth: 16, SizeBytes: 46,
		SHA256:     hex.EncodeToString(digest[:]),
		SourceKind: "imported", EnvironmentKind: "other", ProcessingState: "processing", QualityState: "pending",
	}
	if err := store.PersistPending(context.Background(), sample, wav); err != nil {
		t.Fatalf("保存 pending 样本失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sample.RelativePath))); err != nil {
		t.Fatalf("正式 WAV 不存在：%v", err)
	}
	var count int64
	if err := db.Model(&models.VoiceSample{}).Where("id = ? AND processing_state = 'processing'", sample.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("pending 行不正确：count=%d err=%v", count, err)
	}
}

// TestSampleFileStore_KeepsStagingWhenDatabaseInsertFails 验证数据库失败不会制造正式文件假成功。
func TestSampleFileStore_KeepsStagingWhenDatabaseInsertFails(t *testing.T) {
	db := openVoiceDatabase(t)
	root := t.TempDir()
	store := voiceservice.NewSampleFileStore(root, voicerepository.NewSampleRepository(db), database.NewTransactionManager(db))
	wav := make([]byte, 46)
	sample := buildPendingSample("33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444", wav)

	if err := store.PersistPending(context.Background(), sample, wav); err == nil {
		t.Fatal("期望不存在的成员触发数据库外键失败")
	}
	staging := filepath.Join(root, "data", "voice-samples", ".staging", sample.ID, "sample.wav")
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("数据库失败后 staging 必须保留：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sample.RelativePath))); !os.IsNotExist(err) {
		t.Fatalf("数据库失败后不得出现正式文件：%v", err)
	}
}

// TestSampleFileStore_RecoversValidPendingStaging 验证启动恢复可完成已登记 pending 文件的原子移动。
func TestSampleFileStore_RecoversValidPendingStaging(t *testing.T) {
	db := openVoiceDatabase(t)
	memberID := "11111111-1111-4111-8111-111111111111"
	if err := db.Exec(`INSERT INTO members (id,name,name_normalized,created_at,updated_at) VALUES (?, '恢复成员', '恢复成员', 0, 0)`, memberID).Error; err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	root := t.TempDir()
	wav := make([]byte, 46)
	sample := buildPendingSample("22222222-2222-4222-8222-222222222222", memberID, wav)
	if err := db.Create(&sample).Error; err != nil {
		t.Fatalf("准备 pending 行失败：%v", err)
	}
	stagingDir := filepath.Join(root, "data", "voice-samples", ".staging", sample.ID)
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatalf("准备 staging 目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "sample.wav"), wav, 0o600); err != nil {
		t.Fatalf("准备 staging 文件失败：%v", err)
	}
	store := voiceservice.NewSampleFileStore(root, voicerepository.NewSampleRepository(db), database.NewTransactionManager(db))

	if err := store.RecoverPending(context.Background()); err != nil {
		t.Fatalf("恢复 pending 样本失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(sample.RelativePath))); err != nil {
		t.Fatalf("恢复后的正式 WAV 不存在：%v", err)
	}
}

// TestSampleFileStore_MarksInvalidPendingFileFailed 验证哈希不符不会被恢复为正式样本。
func TestSampleFileStore_MarksInvalidPendingFileFailed(t *testing.T) {
	db := openVoiceDatabase(t)
	memberID := "11111111-1111-4111-8111-111111111111"
	if err := db.Exec(`INSERT INTO members (id,name,name_normalized,created_at,updated_at) VALUES (?, '损坏成员', '损坏成员', 0, 0)`, memberID).Error; err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	root := t.TempDir()
	sample := buildPendingSample("22222222-2222-4222-8222-222222222222", memberID, make([]byte, 46))
	if err := db.Create(&sample).Error; err != nil {
		t.Fatalf("准备 pending 行失败：%v", err)
	}
	stagingDir := filepath.Join(root, "data", "voice-samples", ".staging", sample.ID)
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		t.Fatalf("准备 staging 目录失败：%v", err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "sample.wav"), []byte("damaged"), 0o600); err != nil {
		t.Fatalf("准备损坏文件失败：%v", err)
	}
	store := voiceservice.NewSampleFileStore(root, voicerepository.NewSampleRepository(db), database.NewTransactionManager(db))

	if err := store.RecoverPending(context.Background()); err != nil {
		t.Fatalf("损坏样本应被记录而非阻断启动恢复：%v", err)
	}
	var persisted models.VoiceSample
	if err := db.Where("id = ?", sample.ID).Take(&persisted).Error; err != nil {
		t.Fatalf("读取失败样本失败：%v", err)
	}
	if persisted.ProcessingState != "failed" || persisted.LastErrorCode == nil || *persisted.LastErrorCode != "VOICE_SAMPLE_FILE_INVALID" {
		t.Fatalf("损坏样本状态不正确：%+v", persisted)
	}
}

// TestSampleFileStore_CleansOnlyExpiredOrphanStaging 验证清理不触碰新 staging 或正式目录。
func TestSampleFileStore_CleansOnlyExpiredOrphanStaging(t *testing.T) {
	db := openVoiceDatabase(t)
	root := t.TempDir()
	stagingRoot := filepath.Join(root, "data", "voice-samples", ".staging")
	expiredID := "22222222-2222-4222-8222-222222222222"
	freshID := "33333333-3333-4333-8333-333333333333"
	for _, id := range []string{expiredID, freshID} {
		path := filepath.Join(stagingRoot, id)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("准备 staging 失败：%v", err)
		}
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(stagingRoot, expiredID), oldTime, oldTime); err != nil {
		t.Fatalf("设置过期时间失败：%v", err)
	}
	store := voiceservice.NewSampleFileStore(root, voicerepository.NewSampleRepository(db), database.NewTransactionManager(db))

	if err := store.CleanupOrphanedStaging(context.Background(), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("清理孤立 staging 失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingRoot, expiredID)); !os.IsNotExist(err) {
		t.Fatalf("过期孤立 staging 应删除：%v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingRoot, freshID)); err != nil {
		t.Fatalf("新 staging 不得删除：%v", err)
	}
}

// buildPendingSample 构造与给定 WAV 大小和哈希一致的 pending 样本。
func buildPendingSample(sampleID string, memberID string, wav []byte) models.VoiceSample {
	digest := sha256.Sum256(wav)
	return models.VoiceSample{
		ID: sampleID, MemberID: memberID,
		RelativePath: "data/voice-samples/" + memberID + "/" + sampleID + ".wav",
		DurationMS:   1, SampleRate: 16000, Channels: 1, BitDepth: 16, SizeBytes: int64(len(wav)),
		SHA256: hex.EncodeToString(digest[:]), SourceKind: "imported", EnvironmentKind: "other",
		ProcessingState: "processing", QualityState: "pending",
	}
}

// openVoiceDatabase 创建迁移到最新 schema 的独立 SQLite 数据库。
func openVoiceDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "voice.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}
