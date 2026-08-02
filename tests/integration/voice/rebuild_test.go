package voice_test

import (
	"context"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	voicerepository "meet-sieve/internal/repository/voice"
	voiceservice "meet-sieve/internal/service/voice"
)

// TestRebuildRunner_RebuildsCurrentEmbedding 验证模型变化后从正式 WAV 生成当前向量并清理旧向量。
func TestRebuildRunner_RebuildsCurrentEmbedding(t *testing.T) {
	db := openVoiceDatabase(t)
	memberID := "11111111-1111-4111-8111-111111111111"
	insertVoiceMember(t, db, memberID)
	repository := voicerepository.NewSampleRepository(db)
	transactions := database.NewTransactionManager(db)
	files := voiceservice.NewSampleFileStore(t.TempDir(), repository, transactions)
	enrollment := voiceservice.NewVoiceEnrollmentService(voiceservice.VoiceEnrollmentDependencies{
		Members: peoplerepository.NewMemberRepository(db), Repository: repository, Files: files, Transactions: transactions,
		Encoder: func() (port.VoiceEncoder, error) { return fixedEncoder{}, nil },
		IDs:     identity.NewFixedGenerator("22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"),
		Clock:   clock.NewFixed(time.UnixMilli(1000)),
	})
	samples := make([]int16, 16000*3)
	for index := range samples {
		samples[index] = 4000
		if index%2 == 1 {
			samples[index] = -4000
		}
	}
	if _, err := enrollment.PrepareImported(context.Background(), memberID, "member.wav", "quiet", buildEnrollmentWAV(samples)); err != nil {
		t.Fatalf("准备旧模型样本失败：%v", err)
	}

	runner := voiceservice.NewRebuildRunner(voiceservice.RebuildDependencies{
		Repository: repository, Files: files, Transactions: transactions,
		Encoder: func() (port.VoiceEncoder, error) { return rebuiltEncoder{}, nil },
		IDs:     identity.NewFixedGenerator("44444444-4444-4444-8444-444444444444"), Clock: clock.NewFixed(time.UnixMilli(2000)),
	})
	progress, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("重建 embedding 失败：%v", err)
	}
	if progress.Total != 1 || progress.Completed != 1 || progress.Failed != 0 {
		t.Fatalf("重建进度不正确：%+v", progress)
	}
	var count int64
	if err := db.Table("voice_embeddings").Where("model_version = ?", "2.0.0").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("当前模型向量不正确：count=%d err=%v", count, err)
	}
	if err := db.Table("voice_embeddings").Where("model_version = ?", "1.0.0").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("旧模型向量未清理：count=%d err=%v", count, err)
	}
}

// rebuiltEncoder 模拟同一模型的新锁定版本，只替代外部 ONNX 边界。
type rebuiltEncoder struct{ fixedEncoder }

// ModelInfo 返回重建目标的稳定模型身份。
func (rebuiltEncoder) ModelInfo() port.ModelInfo {
	return port.ModelInfo{ID: "model-id", Version: "2.0.0", Dimension: 192, SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
}
