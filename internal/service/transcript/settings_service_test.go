package transcript

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/port"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestSettingsService_SaveMasksAndKeepsOtherMode 验证显式动作、另一模式保留和掩码读取。
func TestSettingsService_SaveMasksAndKeepsOtherMode(t *testing.T) {
	service, db := newSettingsServiceForTest(t)
	view, err := service.SaveSettings(context.Background(), SaveASRSettingsInput{Mode: transcriptdomain.AuthModeAPIKey, AppID: CredentialChange{Action: CredentialKeep}, AccessToken: CredentialChange{Action: CredentialKeep}, APIKey: CredentialChange{Action: CredentialReplace, Value: "new-api-key-5678"}})
	if err != nil {
		t.Fatalf("保存 API Key 设置失败：%v", err)
	}
	if !view.APIKeyConfigured || view.APIKeyMask != "••••5678" || !view.AppIDConfigured || view.AppIDMask != "••••1234" {
		t.Fatalf("掩码设置投影错误：%+v", view)
	}
	var settings models.Settings
	if err = db.Where("singleton_key = 1").Take(&settings).Error; err != nil {
		t.Fatalf("读取保存后的设置失败：%v", err)
	}
	if valueOrEmpty(settings.VolcAPIAppKey) != "legacy-app-1234" || valueOrEmpty(settings.VolcAPIAccessKey) != "legacy-token-9876" || valueOrEmpty(settings.VolcAPIKey) != "new-api-key-5678" {
		t.Fatal("切换模式不得清除另一模式凭据")
	}
}

// TestSettingsService_ClearsCurrentCredentials 验证用户可以明确删除当前模式凭据，后续连接再报告未配置。
func TestSettingsService_ClearsCurrentCredentials(t *testing.T) {
	service, _ := newSettingsServiceForTest(t)
	view, err := service.SaveSettings(context.Background(), SaveASRSettingsInput{
		Mode:  transcriptdomain.AuthModeLegacy,
		AppID: CredentialChange{Action: CredentialClear}, AccessToken: CredentialChange{Action: CredentialClear},
		APIKey: CredentialChange{Action: CredentialKeep},
	})
	if err != nil {
		t.Fatalf("清除当前凭据失败：%v", err)
	}
	if view.AppIDConfigured || view.AccessTokenConfigured || view.AppIDMask != "" || view.AccessTokenMask != "" {
		t.Fatalf("清除后不得保留配置或掩码：%+v", view)
	}
	_, err = service.CurrentCredentials(context.Background())
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeASRSettingsInvalid.ErrorCode {
		t.Fatalf("清除后实时连接应报告设置不完整：%v", err)
	}
}

// TestSettingsService_RejectsActiveMeetingChange 验证活动会议阻止凭据修改且事务不留下部分更新。
func TestSettingsService_RejectsActiveMeetingChange(t *testing.T) {
	service, db := newSettingsServiceForTest(t)
	startedAt := int64(1_000)
	meeting := models.Meeting{ID: "22222222-2222-4222-8222-222222222222", MeetingNo: "MS-20260802-0002", Subject: "活动会议", RelativeDir: "meetings/active", LocalTimezone: "Asia/Shanghai", StartedAt: &startedAt, LifecycleState: "recording", LocalSaveState: "saving", RealtimeASRState: "streaming", GapState: "none", AgentState: "unchecked", MinuteState: "not_generated", LANState: "disabled", CreatedAt: startedAt, UpdatedAt: startedAt}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatalf("创建活动会议失败：%v", err)
	}
	_, err := service.SaveSettings(context.Background(), SaveASRSettingsInput{Mode: transcriptdomain.AuthModeLegacy, AppID: CredentialChange{Action: CredentialReplace, Value: "changed-app"}, AccessToken: CredentialChange{Action: CredentialKeep}, APIKey: CredentialChange{Action: CredentialKeep}})
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeASRSettingsChangeBlocked.ErrorCode {
		t.Fatalf("活动会议应阻止修改：%v", err)
	}
	var settings models.Settings
	if err = db.Where("singleton_key = 1").Take(&settings).Error; err != nil {
		t.Fatalf("读取设置失败：%v", err)
	}
	if valueOrEmpty(settings.VolcAPIAppKey) != "legacy-app-1234" {
		t.Fatal("阻止修改后数据库不得发生部分更新")
	}
}

// TestSettingsService_TestConnectionDoesNotClaimRealAudio 验证连接探测不把无音频握手描述为真实转写成功。
func TestSettingsService_TestConnectionDoesNotClaimRealAudio(t *testing.T) {
	service, _ := newSettingsServiceForTest(t)
	service.transcriber = func(transcriptdomain.Credentials) port.RealtimeTranscriber { return &probeTranscriber{} }
	result, err := service.TestConnection(context.Background(), transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeLegacy, AppID: "draft-app", AccessToken: "draft-token"})
	if err != nil {
		t.Fatalf("连接测试失败：%v", err)
	}
	if !result.ConnectionEstablished || result.RealAudioVerified {
		t.Fatalf("连接测试状态表述错误：%+v", result)
	}
}

// TestSettingsService_TestConnectionRejectsUnprovenAPIKey 验证文件 API 的鉴权方式不会被套用到实时 WebSocket。
func TestSettingsService_TestConnectionRejectsUnprovenAPIKey(t *testing.T) {
	service, _ := newSettingsServiceForTest(t)
	service.transcriber = func(transcriptdomain.Credentials) port.RealtimeTranscriber { return &probeTranscriber{} }
	_, err := service.TestConnection(context.Background(), transcriptdomain.Credentials{Mode: transcriptdomain.AuthModeAPIKey, APIKey: "draft-key"})
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != apperr.CodeASRProtocolIncompatible.ErrorCode {
		t.Fatalf("API Key 实时探测应明确返回协议不兼容：%v", err)
	}
}

// newSettingsServiceForTest 创建带 settings singleton 的临时 SQLite 服务。
func newSettingsServiceForTest(t *testing.T) (*SettingsService, *gorm.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移设置测试数据库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开设置测试数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	mode, appID, accessToken := "legacy", "legacy-app-1234", "legacy-token-9876"
	settings := models.Settings{ID: "11111111-1111-4111-8111-111111111111", SingletonKey: 1, VolcAuthMode: &mode, VolcAPIAppKey: &appID, VolcAPIAccessKey: &accessToken, WakeWord: "AI 助手", CreatedAt: 1_000, UpdatedAt: 1_000}
	if err = db.Create(&settings).Error; err != nil {
		t.Fatalf("创建 settings singleton 失败：%v", err)
	}
	fixedClock := clock.NewFixed(time.UnixMilli(2_000))
	service := NewSettingsService(SettingsServiceDependencies{Repository: transcriptrepository.NewRepository(db), Transactions: database.NewTransactionManager(db), Clock: fixedClock})
	return service, db
}

type probeTranscriber struct{}

// Start 返回一个只验证生命周期的本地探测 session。
func (transcriber *probeTranscriber) Start(context.Context, port.RealtimeTranscriptionRequest) (port.RealtimeTranscriptionSession, error) {
	return &probeSession{events: make(chan port.TranscriptionEvent)}, nil
}

type probeSession struct {
	events chan port.TranscriptionEvent
}

// WriteFrame 在连接测试中不应被调用。
func (session *probeSession) WriteFrame(context.Context, port.AudioFrame) error { return nil }

// LastSentSample 返回探测 session 没有发送真实音频。
func (session *probeSession) LastSentSample() int64 { return 0 }

// Events 返回探测 session 的空事件流。
func (session *probeSession) Events() <-chan port.TranscriptionEvent { return session.events }

// Stop 关闭探测 session，模拟独立连接已释放。
func (session *probeSession) Stop(context.Context) error {
	close(session.events)
	return nil
}
