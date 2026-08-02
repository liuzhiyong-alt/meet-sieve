package voice_test

import (
	"context"
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

// TestVoiceEnrollmentService_MeetingClipUsesPermanentPipeline 验证会议片段复用正式文件、质量和 embedding 流水线且请求幂等。
func TestVoiceEnrollmentService_MeetingClipUsesPermanentPipeline(t *testing.T) {
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
		IDs:     identity.NewFixedGenerator("22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"),
		Clock:   clock.NewFixed(time.UnixMilli(1000)),
	})
	samples := make([]int16, 16000*3)
	for index := range samples {
		if index%2 == 0 {
			samples[index] = 4000
		} else {
			samples[index] = -4000
		}
	}
	requestID := "44444444-4444-4444-8444-444444444444"
	meetingID := "55555555-5555-4555-8555-555555555555"
	utteranceID := "66666666-6666-4666-8666-666666666666"
	prepareMeetingClipSources(t, db, meetingID, utteranceID)
	result, err := service.PrepareMeetingPCM(context.Background(), requestID, memberID, meetingID, utteranceID, "meeting_room", samples)
	if err != nil || result.SourceKind != "meeting_clip" || result.QualityState != "accepted" {
		t.Fatalf("会议片段录入失败：result=%+v err=%v", result, err)
	}
	var stored models.VoiceSample
	if err := db.Where("id = ?", result.ID).Take(&stored).Error; err != nil {
		t.Fatalf("读取会议片段样本失败：%v", err)
	}
	if stored.RequestID == nil || *stored.RequestID != requestID || stored.SourceMeetingID == nil || *stored.SourceMeetingID != meetingID || stored.SourceUtteranceID == nil || *stored.SourceUtteranceID != utteranceID {
		t.Fatalf("会议片段来源事实错误：%+v", stored)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.RelativePath))); err != nil {
		t.Fatalf("会议片段永久 WAV 不存在：%v", err)
	}
	replay, err := service.PrepareMeetingPCM(context.Background(), requestID, memberID, meetingID, utteranceID, "meeting_room", samples)
	if err != nil || replay.ID != result.ID {
		t.Fatalf("会议片段请求未幂等：replay=%+v err=%v", replay, err)
	}
}

// prepareMeetingClipSources 满足永久样本 source meeting/utterance 外键。
func prepareMeetingClipSources(t *testing.T, db *gorm.DB, meetingID string, utteranceID string) {
	t.Helper()
	statements := []struct {
		query string
		args  []any
	}{
		{query: `INSERT INTO meetings(id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES (?, 'MS-20260802-0030', 'Voice clip', 'meetings/voice-clip', 'Asia/Shanghai', 0, 1, 'ended', 'saved', 'stopped', 'none', 'unchecked', 'not_generated', 'disabled', 0, 1)`, args: []any{meetingID}},
		{query: `INSERT INTO asr_sessions(id, meeting_id, provider, state, started_at, ended_at, reconnect_count, transport_mode, input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at) VALUES ('77777777-7777-4777-8777-777777777777', ?, 'volcano', 'stopped', 0, 1, 0, 'seed_v1', 0, 48000, 48000, 0, 1)`, args: []any{meetingID}},
		{query: `INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES ('88888888-8888-4888-8888-888888888888', ?, 1, 'utterance.final', 0, 'asr', 'utterance', ?, 0, 0)`, args: []any{meetingID, utteranceID}},
		{query: `INSERT INTO utterances(id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, speaker_assignment_source, text_revision, speaker_revision, created_at, updated_at) VALUES (?, ?, '88888888-8888-4888-8888-888888888888', '77777777-7777-4777-8777-777777777777', 'voice-result', '文本', '文本', 0, 48000, 'unassigned', 1, 1, 0, 0)`, args: []any{utteranceID, meetingID}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("准备会议片段来源失败：%v", err)
		}
	}
}
