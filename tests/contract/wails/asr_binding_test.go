package wails_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	infraLogger "meet-sieve/internal/infra/logger"
	"meet-sieve/internal/port"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	transcriptservice "meet-sieve/internal/service/transcript"
	wailstransport "meet-sieve/internal/transport/wails"
	"meet-sieve/models"
)

// TestASRBindingUsesAPIKeyOnlyContract 验证 Wails 只接受 APP Key 动作且不返回旧鉴权字段。
func TestASRBindingUsesAPIKeyOnlyContract(t *testing.T) {
	service := newASRBindingService(t)
	binding := wailstransport.NewASRBinding(
		func() (*transcriptservice.SettingsService, error) { return service, nil },
		nil,
		func() context.Context { return context.Background() },
		wailstransport.NewBoundary(infraLogger.NewNop()),
	)
	result := binding.SaveASRSettings(wailstransport.SaveASRSettingsDTO{
		APIKey: wailstransport.CredentialChangeDTO{Action: "replace", Value: "draft-secret-5678"},
	})
	if result.Code != 200 || result.Data == nil || result.Data.APIKeyMask != "••••5678" {
		t.Fatalf("ASR 保存契约错误：%+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("序列化 ASR 设置结果失败：%v", err)
	}
	if strings.Contains(string(encoded), "draft-secret-5678") || strings.Contains(string(encoded), "legacy-token-9876") {
		t.Fatalf("ASR 设置契约泄漏凭证明文：%s", encoded)
	}
	for _, legacyField := range []string{"\"mode\"", "app_id", "access_token"} {
		if strings.Contains(string(encoded), legacyField) {
			t.Fatalf("ASR 设置响应仍包含旧鉴权字段 %s：%s", legacyField, encoded)
		}
	}
}

// TestASRBindingConnectionProbeDoesNotClaimRealAudio 验证连接探测契约不会伪称已完成真实音频转写。
func TestASRBindingConnectionProbeDoesNotClaimRealAudio(t *testing.T) {
	service := newASRBindingService(t)
	binding := wailstransport.NewASRBinding(
		func() (*transcriptservice.SettingsService, error) { return service, nil },
		nil,
		func() context.Context { return context.Background() },
		wailstransport.NewBoundary(infraLogger.NewNop()),
	)
	result := binding.TestASRConnection(wailstransport.TestASRConnectionDTO{APIKey: "probe-key"})
	if result.Code != 200 || result.Data == nil || !result.Data.ConnectionEstablished || result.Data.RealAudioVerified {
		t.Fatalf("ASR 连接探测契约错误：%+v", result)
	}
}

// TestASRBindingRejectsEmptyAPIKeyProbe 验证边界拒绝空 APP Key。
func TestASRBindingRejectsEmptyAPIKeyProbe(t *testing.T) {
	service := newASRBindingService(t)
	binding := wailstransport.NewASRBinding(
		func() (*transcriptservice.SettingsService, error) { return service, nil },
		nil,
		func() context.Context { return context.Background() },
		wailstransport.NewBoundary(infraLogger.NewNop()),
	)
	result := binding.TestASRConnection(wailstransport.TestASRConnectionDTO{})
	if result.Code == 200 || result.ErrorCode != "ASR_SETTINGS_INVALID" {
		t.Fatalf("空 APP Key 探测应返回稳定设置错误：%+v", result)
	}
}

// newASRBindingService 创建带安全设置和本地探测 adapter 的契约测试服务。
func newASRBindingService(t *testing.T) *transcriptservice.SettingsService {
	t.Helper()
	path := filepath.Join(t.TempDir(), "asr-binding.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移 ASR binding 数据库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 ASR binding 数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	mode, appID, token := "legacy", "legacy-app-1234", "legacy-token-9876"
	settings := models.Settings{ID: "11111111-1111-4111-8111-111111111111", SingletonKey: 1, VolcAuthMode: &mode, VolcAPIAppKey: &appID, VolcAPIAccessKey: &token, WakeWord: "AI 助手", CreatedAt: 1_000, UpdatedAt: 1_000}
	if err = db.Create(&settings).Error; err != nil {
		t.Fatalf("创建 ASR settings 失败：%v", err)
	}
	return transcriptservice.NewSettingsService(transcriptservice.SettingsServiceDependencies{
		Repository: transcriptrepository.NewRepository(db), Transactions: database.NewTransactionManager(db),
		Clock:       clock.NewFixed(time.UnixMilli(2_000)),
		Transcriber: func(transcriptdomain.Credentials) port.RealtimeTranscriber { return &bindingProbeTranscriber{} },
	})
}

type bindingProbeTranscriber struct{}

// Start 返回不发送音频的连接探测 session。
func (*bindingProbeTranscriber) Start(context.Context, port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	return &bindingProbeSession{events: make(chan port.TranscriptionEvent)}, nil
}

type bindingProbeSession struct {
	events chan port.TranscriptionEvent
}

// WriteFrame 在本地连接探测中不执行网络写入。
func (*bindingProbeSession) WriteFrame(context.Context, port.AudioFrame) error { return nil }

// LastSentSample 返回探测 session 没有发送真实音频。
func (*bindingProbeSession) LastSentSample() int64 { return 0 }

// Events 返回空探测事件流。
func (session *bindingProbeSession) Events() <-chan port.TranscriptionEvent { return session.events }

// Stop 关闭本地探测 session。
func (session *bindingProbeSession) Stop(context.Context) error {
	close(session.events)
	return nil
}
