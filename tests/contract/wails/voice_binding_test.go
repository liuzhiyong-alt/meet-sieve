package wails_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"meet-sieve/internal/infra/logger"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	voicerepository "meet-sieve/internal/repository/voice"
	voiceservice "meet-sieve/internal/service/voice"
	wailstransport "meet-sieve/internal/transport/wails"
)

// TestVoiceBinding_ListSamplesDoesNotExposeFilesOrEmbeddings 验证声纹 DTO 不泄漏路径、哈希或向量正文。
func TestVoiceBinding_ListSamplesDoesNotExposeFilesOrEmbeddings(t *testing.T) {
	db := openPeopleBindingDatabase(t)
	memberID := "11111111-1111-4111-8111-111111111111"
	if err := db.Exec(`INSERT INTO members (id,name,name_normalized,created_at,updated_at) VALUES (?, '成员', '成员', 1, 1)`, memberID).Error; err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO voice_samples
		(id,member_id,relative_path,duration_ms,sample_rate,channels,bit_depth,size_bytes,sha256,source_kind,environment_kind,processing_state,quality_state,created_at,updated_at)
		VALUES ('22222222-2222-4222-8222-222222222222',?,'data/voice-samples/private.wav',3000,16000,1,16,96044,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','recorded','quiet','ready','accepted',2,2)`, memberID).Error; err != nil {
		t.Fatalf("准备声纹样本失败：%v", err)
	}
	service := voiceservice.NewVoiceEnrollmentService(voiceservice.VoiceEnrollmentDependencies{
		Members: peoplerepository.NewMemberRepository(db), Repository: voicerepository.NewSampleRepository(db),
	})
	binding := wailstransport.NewVoiceBinding(
		func() (*voiceservice.VoiceEnrollmentService, *voiceservice.RebuildRunner, error) {
			return service, nil, nil
		},
		emptyCapture{}, func() context.Context { return context.Background() }, wailstransport.NewBoundary(logger.NewNop()),
	)
	result := binding.ListVoiceSamples(memberID)
	if result.Code != 200 || result.Data == nil || len(*result.Data) != 1 {
		t.Fatalf("声纹列表响应不正确：%+v", result)
	}
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("序列化声纹 DTO 失败：%v", err)
	}
	text := string(encoded)
	if strings.Contains(text, "private.wav") || strings.Contains(text, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") || strings.Contains(text, "embedding") {
		t.Fatalf("声纹 DTO 泄漏内部文件或向量信息：%s", text)
	}
}

// emptyCapture 为只读契约测试提供未使用的音频 Port。
type emptyCapture struct{}

// ListInputDevices 返回空设备列表。
func (emptyCapture) ListInputDevices(context.Context) ([]port.InputDevice, error) { return nil, nil }

// TestInputDevice 在本测试中不使用。
func (emptyCapture) TestInputDevice(context.Context, string) error { return nil }

// Start 在本测试中不使用。
func (emptyCapture) Start(context.Context, string, port.AudioFormat) (port.AudioStream, error) {
	return nil, nil
}
