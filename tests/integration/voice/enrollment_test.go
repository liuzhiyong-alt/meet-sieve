package voice_test

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	voicerepository "meet-sieve/internal/repository/voice"
	voiceservice "meet-sieve/internal/service/voice"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestVoiceEnrollmentService_ImportsWAVAndCompletesEmbedding 验证上传文件形成正式 WAV、质量状态和当前模型向量。
func TestVoiceEnrollmentService_ImportsWAVAndCompletesEmbedding(t *testing.T) {
	db := openVoiceDatabase(t)
	memberID := "11111111-1111-4111-8111-111111111111"
	insertVoiceMember(t, db, memberID)
	root := t.TempDir()
	repository := voicerepository.NewSampleRepository(db)
	transactions := database.NewTransactionManager(db)
	service := voiceservice.NewVoiceEnrollmentService(voiceservice.VoiceEnrollmentDependencies{
		Members: peoplerepository.NewMemberRepository(db), Repository: repository,
		Files: voiceservice.NewSampleFileStore(root, repository, transactions), Transactions: transactions,
		Encoder: func() (port.VoiceEncoder, error) { return fixedEncoder{}, nil },
		IDs: identity.NewFixedGenerator(
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		),
		Clock: clock.NewFixed(time.UnixMilli(1000)),
	})
	samples := make([]int16, 16000*3)
	for index := range samples {
		if index%2 == 0 {
			samples[index] = 4000
		} else {
			samples[index] = -4000
		}
	}

	result, err := service.PrepareImported(context.Background(), memberID, "/private/input/alice.wav", "quiet", buildEnrollmentWAV(samples))
	if err != nil {
		t.Fatalf("上传声纹样本失败：%v", err)
	}
	if result.QualityState != "accepted" || result.ProcessingState != "ready" || result.SourceName != "alice.wav" {
		t.Fatalf("声纹样本结果不正确：%+v", result)
	}
	var embedding models.VoiceEmbedding
	if err := db.Where("voice_sample_id = ?", result.ID).Take(&embedding).Error; err != nil {
		t.Fatalf("当前模型 embedding 未持久化：%v", err)
	}
	if embedding.Dimension != 192 || len(embedding.Embedding) != 192*4 {
		t.Fatalf("embedding 结构不正确：%+v", embedding)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.RelativePath))); err != nil {
		t.Fatalf("正式样本文件不存在：%v", err)
	}
}

// fixedEncoder 返回结构有效的固定向量，只替代外部模型边界。
type fixedEncoder struct{}

// Encode 返回 192 维有限 embedding。
func (fixedEncoder) Encode(_ context.Context, _ port.AudioPCM) (port.Embedding, error) {
	result := make(port.Embedding, 192)
	for index := range result {
		result[index] = float32(index+1) / 192
	}
	return result, nil
}

// ModelInfo 返回测试使用的稳定模型身份。
func (fixedEncoder) ModelInfo() port.ModelInfo {
	return port.ModelInfo{ID: "model-id", Version: "1.0.0", Dimension: 192, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}

// insertVoiceMember 准备一个活动成员。
func insertVoiceMember(t *testing.T, db *gorm.DB, memberID string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO members (id,name,name_normalized,created_at,updated_at) VALUES (?, '录入成员', '录入成员', 0, 0)`, memberID).Error; err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
}

// buildEnrollmentWAV 构造 16kHz 单声道 PCM16 WAV。
func buildEnrollmentWAV(samples []int16) []byte {
	dataSize := len(samples) * 2
	buffer := make([]byte, 44+dataSize)
	copy(buffer[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buffer[4:8], uint32(36+dataSize))
	copy(buffer[8:12], "WAVE")
	copy(buffer[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buffer[16:20], 16)
	binary.LittleEndian.PutUint16(buffer[20:22], 1)
	binary.LittleEndian.PutUint16(buffer[22:24], 1)
	binary.LittleEndian.PutUint32(buffer[24:28], 16000)
	binary.LittleEndian.PutUint32(buffer[28:32], 32000)
	binary.LittleEndian.PutUint16(buffer[32:34], 2)
	binary.LittleEndian.PutUint16(buffer[34:36], 16)
	copy(buffer[36:40], "data")
	binary.LittleEndian.PutUint32(buffer[40:44], uint32(dataSize))
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(buffer[44+index*2:], uint16(sample))
	}
	return buffer
}
