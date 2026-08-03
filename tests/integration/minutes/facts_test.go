package minutes_test

import (
	"context"
	"testing"

	domainminutes "meet-sieve/internal/domain/minutes"
	minutesservice "meet-sieve/internal/service/minutes"
)

// TestReadFactSnapshot_ExcludesAIAndIncompleteResource 验证事实读取从数据入口排除 AI 与失败资源。
func TestReadFactSnapshot_ExcludesAIAndIncompleteResource(t *testing.T) {
	db, repository := openMinutesDatabase(t)
	statements := []string{
		`INSERT INTO meeting_events (id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, payload_json, created_at, updated_at) VALUES ('11111111-1111-4111-8111-111111111111','81818181-8181-4818-8818-818181818181',1,'utterance.final',100,'asr','utterance','12121212-1212-4121-8121-121212121212',NULL,1,1)`,
		`INSERT INTO asr_sessions (id, meeting_id, provider, provider_session_id, state, started_at, ended_at, reconnect_count, transport_mode, input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at) VALUES ('13131313-1313-4131-8131-131313131313','81818181-8181-4818-8818-818181818181','volcano','p1','stopped',0,1,0,'seed_v1',0,16000,16000,0,1)`,
		`INSERT INTO utterances (id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, speaker_assignment_source, text_revision, speaker_revision, created_at, updated_at) VALUES ('12121212-1212-4121-8121-121212121212','81818181-8181-4818-8818-818181818181','11111111-1111-4111-8111-111111111111','13131313-1313-4131-8131-131313131313','r1','旧文字','当前文字',0,16000,'unassigned',1,1,1,1)`,
		`INSERT INTO meeting_events (id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, payload_json, created_at, updated_at) VALUES ('14141414-1414-4141-8141-141414141414','81818181-8181-4818-8818-818181818181',2,'ai.answer',101,'agent','agent_turn','15151515-1515-4151-8151-151515151515','{"text":"绝不能成为事实"}',1,1)`,
		`INSERT INTO meeting_events (id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, payload_json, created_at, updated_at) VALUES ('16161616-1616-4161-8161-161616161616','81818181-8181-4818-8818-818181818181',3,'resource.created',102,'host','resource','17171717-1717-4171-8171-171717171717',NULL,1,1)`,
		`INSERT INTO resources (id, meeting_id, event_id, kind, original_name, safe_name, relative_path, state, description_revision, created_at, updated_at) VALUES ('17171717-1717-4171-8171-171717171717','81818181-8181-4818-8818-818181818181','16161616-1616-4161-8161-161616161616','attachment','失败.txt','失败.txt','resources/失败.txt','failed',1,1,1)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备纪要事实失败：%v", err)
		}
	}
	snapshot, err := repository.ReadFactSnapshot(context.Background(), meetingID)
	if err != nil {
		t.Fatalf("读取纪要事实：%v", err)
	}
	if len(snapshot.Facts) != 1 || snapshot.Facts[0].Kind != domainminutes.FactUtterance || snapshot.Facts[0].Text != "当前文字" {
		t.Fatalf("白名单事实错误：%#v", snapshot.Facts)
	}
	payload, validation, err := minutesservice.BuildProviderInput(snapshot)
	if err != nil || len(payload) == 0 || validation.FactText[1] != "当前文字" {
		t.Fatalf("构造 provider 输入失败：validation=%#v err=%v", validation, err)
	}
}
