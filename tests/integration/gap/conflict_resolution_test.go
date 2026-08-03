package gap_test

import (
	"context"
	"testing"
	"time"

	domaingap "meet-sieve/internal/domain/gap"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	gaprepository "meet-sieve/internal/repository/gap"
	gapservice "meet-sieve/internal/service/gap"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestResolutionService_KeepExistingCompletesGapWithoutChangingUtterance 验证保留现有内容只提交解决审计。
func TestResolutionService_KeepExistingCompletesGapWithoutChangingUtterance(t *testing.T) {
	db := openGapDatabase(t)
	repository := gaprepository.NewRepository(db, database.NewTransactionManager(db))
	prepareConflict(t, db)
	query := gapservice.NewConflictQueryService(repository)
	evidence, err := query.GetConflict(context.Background(), testMeetingID, testGapID)
	if err != nil || len(evidence.Candidates) != 1 || len(evidence.Existing) != 1 || evidence.AudioClipID != testAudioID {
		t.Fatalf("冲突证据错误：evidence=%#v err=%v", evidence, err)
	}
	service := gapservice.NewResolutionService(repository, gapservice.NewExtractor(repository, t.TempDir()), conflictFlusher{}, identity.NewFixedGenerator("30303030-3030-4030-8030-303030303030"), clock.NewFixed(time.UnixMilli(100)))
	err = service.Resolve(context.Background(), gapservice.ResolutionCommand{
		MeetingID: testMeetingID, GapID: testGapID, Revision: evidence.Revision,
		Resolution: domaingap.ResolutionKeepExisting, RequestID: "31313131-3131-4131-8131-313131313131", OperatorID: "32323232-3232-4232-8232-323232323232",
	})
	if err != nil {
		t.Fatalf("解决冲突失败：%v", err)
	}
	if err := service.Resolve(context.Background(), gapservice.ResolutionCommand{
		MeetingID: testMeetingID, GapID: testGapID, Revision: evidence.Revision,
		Resolution: domaingap.ResolutionKeepExisting, RequestID: "31313131-3131-4131-8131-313131313131", OperatorID: "32323232-3232-4232-8232-323232323232",
	}); err != nil {
		t.Fatalf("相同请求重放应幂等成功：%v", err)
	}
	if err := service.Resolve(context.Background(), gapservice.ResolutionCommand{
		MeetingID: testMeetingID, GapID: "different-gap", Revision: evidence.Revision,
		Resolution: domaingap.ResolutionKeepExisting, RequestID: "31313131-3131-4131-8131-313131313131", OperatorID: "32323232-3232-4232-8232-323232323232",
	}); err == nil {
		t.Fatal("同一 request ID 不得跨 gap 复用")
	}
	var gap models.ASRGap
	if err := db.Where("id = ?", testGapID).Take(&gap).Error; err != nil || gap.State != "completed" || gap.ConflictJSON != nil {
		t.Fatalf("gap 未完成：gap=%#v err=%v", gap, err)
	}
	var utterance models.Utterance
	if err := db.Where("id = '34343434-3434-4434-8434-343434343434'").Take(&utterance).Error; err != nil || utterance.CurrentText != "现有文字" || utterance.TextRevision != 1 {
		t.Fatalf("保留现有动作修改了正式转写：utterance=%#v err=%v", utterance, err)
	}
}

// prepareConflict 写入一组真实 conflict attempt 与现有 utterance。
func prepareConflict(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`UPDATE asr_gaps SET state='conflict', conflict_json='{"attempt_id":"33333333-3333-4333-8333-333333333333","overlaps":[{"id":"34343434-3434-4434-8434-343434343434"}]}', updated_at=10 WHERE id='93939393-9393-4939-8939-939393939393'`,
		`UPDATE meetings SET gap_state='conflict', updated_at=10 WHERE id='91919191-9191-4919-8919-919191919191'`,
		`INSERT INTO gap_transcription_attempts (id,meeting_id,audio_asset_id,provider,provider_request_id,core_start_sample,core_end_sample,audio_start_sample,audio_end_sample,state,attempt_no,request_sha256,response_json,started_at,ended_at,created_at,updated_at) VALUES ('33333333-3333-4333-8333-333333333333','91919191-9191-4919-8919-919191919191','94949494-9494-4949-8949-949494949494','volcano','35353535-3535-4535-8535-353535353535',0,16000,0,16000,'conflict',1,'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd','{"no_speech":false,"segments":[{"text":"文件文字","speaker_id":"","start_sample":0,"end_sample":16000}]}',1,10,1,10)`,
		`INSERT INTO gap_transcription_attempt_items (attempt_id,gap_id,item_order,created_at) VALUES ('33333333-3333-4333-8333-333333333333','93939393-9393-4939-8939-939393939393',0,1)`,
		`INSERT INTO asr_sessions (id,meeting_id,provider,provider_session_id,state,started_at,ended_at,reconnect_count,transport_mode,input_start_sample,last_sent_sample,last_final_sample,created_at,updated_at) VALUES ('36363636-3636-4636-8636-363636363636','91919191-9191-4919-8919-919191919191','volcano','existing-session','stopped',0,1,0,'seed_v1',0,16000,16000,0,1)`,
		`INSERT INTO meeting_events (id,meeting_id,seq,kind,occurred_at,source,entity_type,entity_id,created_at,updated_at) VALUES ('37373737-3737-4737-8737-373737373737','91919191-9191-4919-8919-919191919191',2,'utterance.final',1,'asr','utterance','34343434-3434-4434-8434-343434343434',1,1)`,
		`INSERT INTO utterances (id,meeting_id,event_id,asr_session_id,provider_result_id,original_text,current_text,start_sample,end_sample,speaker_assignment_source,text_revision,speaker_revision,created_at,updated_at) VALUES ('34343434-3434-4434-8434-343434343434','91919191-9191-4919-8919-919191919191','37373737-3737-4737-8737-373737373737','36363636-3636-4636-8636-363636363636','existing','现有文字','现有文字',100,15000,'unassigned',1,1,1,1)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备冲突事实失败：%v", err)
		}
	}
}

type conflictFlusher struct{}

// Flush 模拟成功刷新原始记录。
func (conflictFlusher) Flush(context.Context, string) error { return nil }
