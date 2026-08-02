package correction_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	correctionrepository "meet-sieve/internal/repository/correction"
	correctionservice "meet-sieve/internal/service/correction"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	meetingID     = "11111111-1111-4111-8111-111111111111"
	utteranceID   = "44444444-4444-4444-8444-444444444444"
	participantID = "66666666-6666-4666-8666-666666666666"
	resourceID    = "88888888-8888-4888-8888-888888888888"
	operatorID    = "99999999-9999-4999-8999-999999999999"
)

type failingFlusher struct{}

// Flush 模拟 SQLite 已提交后的 Markdown 刷新失败。
func (failingFlusher) Flush(context.Context, string) error { return errors.New("disk failed") }

// TestService_TextCorrectionIsAuditedAndIdempotent 验证原文保留、逐条审计、幂等和 revision 冲突。
func TestService_TextCorrectionIsAuditedAndIdempotent(t *testing.T) {
	db := prepareCorrectionDatabase(t)
	service := newCorrectionService(db, nil,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
	)
	command := correctionservice.TextCommand{CommandBase: baseCommand("dddddddd-dddd-4ddd-8ddd-dddddddddddd", utteranceID, 1), Text: " 修正文本 "}
	result, err := service.CorrectText(context.Background(), command)
	if err != nil || !result.Saved || result.ResultRevision != 2 {
		t.Fatalf("文本校对失败：result=%+v err=%v", result, err)
	}
	var utterance models.Utterance
	if err := db.Where("id = ?", utteranceID).Take(&utterance).Error; err != nil {
		t.Fatalf("读取文本校对结果失败：%v", err)
	}
	if utterance.OriginalText != "原始文本" || utterance.CurrentText != "修正文本" || utterance.TextRevision != 2 {
		t.Fatalf("文本事实被错误更新：%+v", utterance)
	}
	assertCount(t, db, "corrections", 1)
	assertCount(t, db, "correction_items", 1)
	assertCountWhere(t, db, "meeting_events", "kind = 'utterance.corrected'", 1)

	replay, err := service.CorrectText(context.Background(), command)
	if err != nil || !replay.Duplicate || replay.CorrectionID != result.CorrectionID {
		t.Fatalf("相同请求未幂等返回：result=%+v err=%v", replay, err)
	}
	conflict := command
	conflict.Text = "另一文本"
	_, err = service.CorrectText(context.Background(), conflict)
	assertAppError(t, err, apperr.CodeCorrectionIdempotencyConflict.ErrorCode)
	_, err = service.CorrectText(context.Background(), correctionservice.TextCommand{CommandBase: baseCommand("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", utteranceID, 1), Text: "再次修改"})
	assertAppError(t, err, apperr.CodeCorrectionRevisionConflict.ErrorCode)
}

// TestService_SpeakerAndResourceCorrectionsKeepOriginalFacts 验证单条 speaker 与 resource 当前投影边界。
func TestService_SpeakerAndResourceCorrectionsKeepOriginalFacts(t *testing.T) {
	db := prepareCorrectionDatabase(t)
	speaker := newCorrectionService(db, nil,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
	)
	result, err := speaker.CorrectSpeaker(context.Background(), correctionservice.SpeakerCommand{
		CommandBase: baseCommand("dddddddd-dddd-4ddd-8ddd-dddddddddddd", utteranceID, 1), ParticipantID: participantID,
	})
	if err != nil || !result.Saved {
		t.Fatalf("speaker 校对失败：result=%+v err=%v", result, err)
	}
	var participant, source, originalLabel string
	var revision int
	if err := db.Raw("SELECT current_participant_id, speaker_assignment_source, asr_speaker_label, speaker_revision FROM utterances WHERE id=?", utteranceID).Row().Scan(&participant, &source, &originalLabel, &revision); err != nil {
		t.Fatalf("读取 speaker 校对失败：%v", err)
	}
	if participant != participantID || source != "manual_single" || originalLabel != "speaker-1" || revision != 2 {
		t.Fatalf("speaker 当前/原始事实错误：participant=%s source=%s label=%s revision=%d", participant, source, originalLabel, revision)
	}

	resource := newCorrectionService(db, nil,
		"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", "ffffffff-ffff-4fff-8fff-ffffffffffff", "abababab-abab-4bab-8bab-abababababab",
	)
	_, err = resource.CorrectResource(context.Background(), correctionservice.ResourceCommand{
		CommandBase: baseCommand("acacacac-acac-4cac-8cac-acacacacacac", resourceID, 1), Description: "新说明",
	})
	if err != nil {
		t.Fatalf("resource 校对失败：%v", err)
	}
	var original, current string
	if err := db.Raw("SELECT original_description, current_description FROM resources WHERE id=?", resourceID).Row().Scan(&original, &current); err != nil {
		t.Fatalf("读取 resource 校对失败：%v", err)
	}
	if original != "原说明" || current != "新说明" {
		t.Fatalf("resource 原始事实错误：original=%s current=%s", original, current)
	}
}

// TestService_ReportsProjectionFailureWithoutRepeatingSavedCorrection 验证 SQLite 成功与 Markdown 失败分开报告。
func TestService_ReportsProjectionFailureWithoutRepeatingSavedCorrection(t *testing.T) {
	db := prepareCorrectionDatabase(t)
	service := newCorrectionService(db, failingFlusher{},
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
	)
	result, err := service.CorrectText(context.Background(), correctionservice.TextCommand{
		CommandBase: baseCommand("dddddddd-dddd-4ddd-8ddd-dddddddddddd", utteranceID, 1), Text: "已保存",
	})
	if err != nil || !result.Saved || result.ProjectionState != "failed" || result.ProjectionErrorCode != "RAW_RECORD_REFRESH_FAILED" {
		t.Fatalf("部分成功结果错误：result=%+v err=%v", result, err)
	}
	assertCount(t, db, "corrections", 1)
}

// TestService_RollsBackTargetWhenAuditFails 验证 event 写失败会回滚 current projection。
func TestService_RollsBackTargetWhenAuditFails(t *testing.T) {
	db := prepareCorrectionDatabase(t)
	service := newCorrectionService(db, nil,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "33333333-3333-4333-8333-333333333333",
	)
	_, err := service.CorrectText(context.Background(), correctionservice.TextCommand{
		CommandBase: baseCommand("dddddddd-dddd-4ddd-8ddd-dddddddddddd", utteranceID, 1), Text: "不应提交",
	})
	if err == nil {
		t.Fatal("重复 event ID 必须导致事务失败")
	}
	var text string
	if err := db.Raw("SELECT current_text FROM utterances WHERE id=?", utteranceID).Scan(&text).Error; err != nil || text != "原始文本" {
		t.Fatalf("审计失败后目标未回滚：text=%s err=%v", text, err)
	}
	assertCount(t, db, "corrections", 0)
}

// TestService_ClusterCorrectionAuditsEveryCurrentUtterance 验证批量范围、逐条审计与后续单条覆盖。
func TestService_ClusterCorrectionAuditsEveryCurrentUtterance(t *testing.T) {
	db := prepareCorrectionDatabase(t)
	clusterID := prepareCorrectionCluster(t, db)
	service := newCorrectionService(db, nil,
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		"cccccccc-cccc-4ccc-8ccc-cccccccccccc", "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
	)
	preview, err := service.PreviewCluster(context.Background(), meetingID, clusterID)
	if err != nil || preview.Revision != 1 || preview.ImpactedCount != 2 || preview.DisplayName != "未知说话人 1" {
		t.Fatalf("cluster preview 错误：preview=%+v err=%v", preview, err)
	}
	result, err := service.CorrectCluster(context.Background(), correctionservice.ClusterCommand{
		CommandBase:   baseCommand("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", clusterID, preview.Revision),
		ParticipantID: participantID, ExpectedCount: preview.ImpactedCount,
	})
	if err != nil || !result.Saved || result.ImpactedCount != 2 || result.ResultRevision != 2 {
		t.Fatalf("cluster correction 失败：result=%+v err=%v", result, err)
	}
	assertCountWhere(t, db, "correction_items", "correction_id = ?", 2, result.CorrectionID)
	assertCountWhere(t, db, "utterances", "speaker_cluster_id = ? AND current_participant_id = ? AND speaker_assignment_source = 'manual_cluster'", 2, clusterID, participantID)

	single := newCorrectionService(db, nil,
		"ffffffff-ffff-4fff-8fff-ffffffffffff", "aeaeaeae-aeae-4eae-8eae-aeaeaeaeaeae", "b0b0b0b0-b0b0-40b0-80b0-b0b0b0b0b0b0",
	)
	_, err = single.CorrectSpeaker(context.Background(), correctionservice.SpeakerCommand{
		CommandBase: baseCommand("adadadad-adad-4dad-8dad-adadadadadad", utteranceID, 2), ParticipantID: participantID,
	})
	if err != nil {
		t.Fatalf("批量后单条覆盖失败：%v", err)
	}
	var source string
	if err := db.Raw("SELECT speaker_assignment_source FROM utterances WHERE id=?", utteranceID).Scan(&source).Error; err != nil || source != "manual_single" {
		t.Fatalf("单条未覆盖批量投影：source=%s err=%v", source, err)
	}
}

// TestService_ClusterCorrectionRejectsChangedScope 验证确认后的新增范围不能静默批量修改。
func TestService_ClusterCorrectionRejectsChangedScope(t *testing.T) {
	db := prepareCorrectionDatabase(t)
	clusterID := prepareCorrectionCluster(t, db)
	service := newCorrectionService(db, nil)
	_, err := service.CorrectCluster(context.Background(), correctionservice.ClusterCommand{
		CommandBase:   baseCommand("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", clusterID, 1),
		ParticipantID: participantID, ExpectedCount: 1,
	})
	assertAppError(t, err, apperr.CodeCorrectionRevisionConflict.ErrorCode)
	assertCount(t, db, "corrections", 0)
}

// prepareCorrectionDatabase 创建 ended 会议及 utterance、participant、completed resource。
func prepareCorrectionDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "correction.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 correction migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 correction 数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	statements := []string{
		`INSERT INTO meetings(id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES ('11111111-1111-4111-8111-111111111111', 'MS-20260802-0020', 'Correction', 'meetings/correction', 'Asia/Shanghai', 0, 1, 'ended', 'saved', 'stopped', 'none', 'unchecked', 'not_generated', 'disabled', 0, 1)`,
		`INSERT INTO asr_sessions(id, meeting_id, provider, state, started_at, ended_at, reconnect_count, transport_mode, input_start_sample, last_sent_sample, last_final_sample, created_at, updated_at) VALUES ('22222222-2222-4222-8222-222222222222', '11111111-1111-4111-8111-111111111111', 'volcano', 'stopped', 0, 1, 0, 'seed_v1', 0, 16000, 16000, 0, 1)`,
		`INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES ('33333333-3333-4333-8333-333333333333', '11111111-1111-4111-8111-111111111111', 1, 'utterance.final', 0, 'asr', 'utterance', '44444444-4444-4444-8444-444444444444', 0, 0)`,
		`INSERT INTO utterances(id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, asr_speaker_label, speaker_assignment_source, text_revision, speaker_revision, created_at, updated_at) VALUES ('44444444-4444-4444-8444-444444444444', '11111111-1111-4111-8111-111111111111', '33333333-3333-4333-8333-333333333333', '22222222-2222-4222-8222-222222222222', 'result-1', '原始文本', '原始文本', 0, 16000, 'speaker-1', 'unassigned', 1, 1, 0, 0)`,
		`INSERT INTO members(id, name, name_normalized, created_at, updated_at) VALUES ('55555555-5555-4555-8555-555555555555', '张三', 'zhang-san', 0, 0)`,
		`INSERT INTO meeting_participants(id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at) VALUES ('66666666-6666-4666-8666-666666666666', '11111111-1111-4111-8111-111111111111', '55555555-5555-4555-8555-555555555555', 'member', '张三', 0, 0, 0)`,
		`INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES ('77777777-7777-4777-8777-777777777777', '11111111-1111-4111-8111-111111111111', 2, 'resource.created', 0, 'host', 'resource', '88888888-8888-4888-8888-888888888888', 0, 0)`,
		`INSERT INTO resources(id, meeting_id, event_id, kind, source_url, original_description, current_description, description_revision, state, created_at, updated_at) VALUES ('88888888-8888-4888-8888-888888888888', '11111111-1111-4111-8111-111111111111', '77777777-7777-4777-8777-777777777777', 'link', 'https://example.com', '原说明', '原说明', 1, 'completed', 0, 0)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备 correction fixture 失败：%v", err)
		}
	}
	return db
}

// prepareCorrectionCluster 创建同场两个片段的 unknown cluster，其中首条已有 manual_single 历史。
func prepareCorrectionCluster(t *testing.T, db *gorm.DB) string {
	t.Helper()
	clusterID := "abababab-abab-4bab-8bab-abababababab"
	statements := []string{
		`INSERT INTO speaker_clusters(id, meeting_id, display_no, assignment_source, track_count, revision, created_at, updated_at) VALUES ('abababab-abab-4bab-8bab-abababababab', '11111111-1111-4111-8111-111111111111', 1, 'unassigned', 1, 1, 0, 0)`,
		`UPDATE utterances SET speaker_cluster_id='abababab-abab-4bab-8bab-abababababab', current_participant_id='66666666-6666-4666-8666-666666666666', speaker_assignment_source='manual_single' WHERE id='44444444-4444-4444-8444-444444444444'`,
		`INSERT INTO meeting_events(id, meeting_id, seq, kind, occurred_at, source, entity_type, entity_id, created_at, updated_at) VALUES ('acacacac-acac-4cac-8cac-acacacacacac', '11111111-1111-4111-8111-111111111111', 3, 'utterance.final', 0, 'asr', 'utterance', 'adadadad-adad-4dad-8dad-adadadadadad', 0, 0)`,
		`INSERT INTO utterances(id, meeting_id, event_id, asr_session_id, provider_result_id, original_text, current_text, start_sample, end_sample, asr_speaker_label, speaker_cluster_id, speaker_assignment_source, text_revision, speaker_revision, created_at, updated_at) VALUES ('adadadad-adad-4dad-8dad-adadadadadad', '11111111-1111-4111-8111-111111111111', 'acacacac-acac-4cac-8cac-acacacacacac', '22222222-2222-4222-8222-222222222222', 'result-2', '第二段', '第二段', 16000, 32000, 'speaker-1', 'abababab-abab-4bab-8bab-abababababab', 'automatic_cluster', 1, 1, 0, 0)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备 correction cluster 失败：%v", err)
		}
	}
	return clusterID
}

// newCorrectionService 使用确定性 ID 装配真实事务服务。
func newCorrectionService(db *gorm.DB, flusher correctionservice.RawRecordFlusher, ids ...string) *correctionservice.Service {
	return correctionservice.NewService(correctionservice.ServiceDependencies{
		Repository: correctionrepository.NewRepository(), Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(ids...), Clock: clock.NewFixed(time.UnixMilli(2)), RawRecord: flusher,
	})
}

// baseCommand 构造本地主机校对公共字段。
func baseCommand(requestID string, targetID string, revision int) correctionservice.CommandBase {
	return correctionservice.CommandBase{RequestID: requestID, MeetingID: meetingID, TargetID: targetID, ExpectedRevision: revision, OperatorID: operatorID}
}

// assertAppError 验证稳定业务错误码。
func assertAppError(t *testing.T, err error, code string) {
	t.Helper()
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) || appErr.ErrorCode != code {
		t.Fatalf("错误码错误：want=%s err=%v", code, err)
	}
}

// assertCount 验证表总行数。
func assertCount(t *testing.T, db *gorm.DB, table string, want int64) {
	t.Helper()
	assertCountWhere(t, db, table, "1 = 1", want)
}

// assertCountWhere 验证条件行数。
func assertCountWhere(t *testing.T, db *gorm.DB, table string, condition string, want int64, args ...any) {
	t.Helper()
	var count int64
	if err := db.Table(table).Where(condition, args...).Count(&count).Error; err != nil || count != want {
		t.Fatalf("行数错误：table=%s got=%d want=%d err=%v", table, count, want, err)
	}
}
