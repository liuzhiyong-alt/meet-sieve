package gap_test

import (
	"context"
	"testing"

	"meet-sieve/internal/infra/database"
	gaprepository "meet-sieve/internal/repository/gap"
	"meet-sieve/models"

	"gorm.io/gorm"
)

// TestCommitCompensation_CreatesSyntheticSessionAndOrderedFacts 验证无冲突补偿事实同事务提交。
func TestCommitCompensation_CreatesSyntheticSessionAndOrderedFacts(t *testing.T) {
	db := openGapDatabase(t)
	repository := gaprepository.NewRepository(db, database.NewTransactionManager(db))
	claim := newClaimInput("95959595-9595-4959-8959-959595959595", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := repository.ClaimGapAttempt(context.Background(), claim); err != nil {
		t.Fatalf("claim gap 失败：%v", err)
	}
	sessionID := "97979797-9797-4979-8979-979797979797"
	eventID := "98989898-9898-4989-8989-989898989898"
	utteranceID := "99999999-9999-4999-8999-999999999999"
	entityType := "utterance"
	providerID := claim.Attempt.ProviderRequestID
	endedAt := int64(11)
	input := gaprepository.CompensationInput{
		AttemptID: claim.Attempt.ID,
		Session: models.ASRSession{
			ID: sessionID, MeetingID: testMeetingID, Provider: "volcano", ProviderSessionID: &providerID,
			State: "stopped", StartedAt: 10, EndedAt: &endedAt, TransportMode: "auc_flash_v3",
			InputStartSample: 0, LastSentSample: 16000, LastFinalSample: 16000, CreatedAt: 10, UpdatedAt: 11,
		},
		Events: []models.MeetingEvent{{
			ID: eventID, MeetingID: testMeetingID, Kind: "asr.compensated", OccurredAt: 11,
			Source: "asr", EntityType: &entityType, EntityID: &utteranceID, CreatedAt: 11, UpdatedAt: 11,
		}},
		Utterances: []models.Utterance{{
			ID: utteranceID, MeetingID: testMeetingID, ASRSessionID: sessionID,
			ProviderResultID: providerID + ":0", OriginalText: "补偿文字", CurrentText: "补偿文字",
			StartSample: 0, EndSample: 16000, SpeakerAssignmentSource: "unassigned",
			TextRevision: 1, SpeakerRevision: 1, CreatedAt: 11, UpdatedAt: 11,
		}},
		ResponseJSON:        `{"no_speech":false,"segments":[{"text":"补偿文字","start_sample":0,"end_sample":16000}]}`,
		ProviderLogIDSuffix: "12345678", UpdatedAt: 11,
	}
	if err := repository.CommitCompensation(context.Background(), input); err != nil {
		t.Fatalf("提交补偿失败：%v", err)
	}
	var utterance models.Utterance
	if err := db.Where("id = ?", utteranceID).Take(&utterance).Error; err != nil || utterance.ASRSessionID != sessionID {
		t.Fatalf("补偿 utterance 错误：utterance=%#v err=%v", utterance, err)
	}
	var session models.ASRSession
	if err := db.Where("id = ?", sessionID).Take(&session).Error; err != nil || session.TransportMode != "auc_flash_v3" {
		t.Fatalf("synthetic session 错误：session=%#v err=%v", session, err)
	}
	assertGapAttemptTerminal(t, db, "completed", "completed")
}

// TestCommitNoSpeechCompensation_CompletesWithoutEmptyUtterance 验证静音补偿只写事件和终态。
func TestCommitNoSpeechCompensation_CompletesWithoutEmptyUtterance(t *testing.T) {
	db := openGapDatabase(t)
	repository := gaprepository.NewRepository(db, database.NewTransactionManager(db))
	claim := newClaimInput("95959595-9595-4959-8959-959595959595", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := repository.ClaimGapAttempt(context.Background(), claim); err != nil {
		t.Fatalf("claim gap 失败：%v", err)
	}
	entityType := "asr_gap"
	entityID := testGapID
	payload := `{"v":1,"resolution":"no_speech"}`
	input := gaprepository.NoSpeechInput{
		AttemptID: claim.Attempt.ID,
		Session: models.ASRSession{
			ID: "97979797-9797-4979-8979-979797979797", MeetingID: testMeetingID, Provider: "volcano",
			ProviderSessionID: &claim.Attempt.ProviderRequestID, State: "stopped", TransportMode: "auc_flash_v3",
			StartedAt: 10, EndedAt: int64Pointer(11), InputStartSample: 0, LastSentSample: 16000,
			LastFinalSample: 16000, CreatedAt: 10, UpdatedAt: 11,
		},
		Event: models.MeetingEvent{
			ID: "98989898-9898-4989-8989-989898989898", MeetingID: testMeetingID,
			Kind: "asr.compensated", OccurredAt: 11, Source: "asr", EntityType: &entityType,
			EntityID: &entityID, PayloadJSON: &payload, CreatedAt: 11, UpdatedAt: 11,
		},
		ResponseJSON: `{"no_speech":true,"segments":[]}`, ProviderLogIDSuffix: "12345678", UpdatedAt: 11,
	}
	if err := repository.CommitNoSpeechCompensation(context.Background(), input); err != nil {
		t.Fatalf("提交静音补偿失败：%v", err)
	}

	var utteranceCount int64
	if err := db.Model(&models.Utterance{}).Count(&utteranceCount).Error; err != nil || utteranceCount != 0 {
		t.Fatalf("静音补偿不得创建 utterance：count=%d err=%v", utteranceCount, err)
	}
	assertGapAttemptTerminal(t, db, "completed", "completed")
	var meeting models.Meeting
	if err := db.Where("id = ?", testMeetingID).Take(&meeting).Error; err != nil || meeting.GapState != "completed" {
		t.Fatalf("会议 gap 聚合错误：meeting=%#v err=%v", meeting, err)
	}
}

// TestCommitGapConflict_PreservesCandidateWithoutUtterance 验证冲突只保存有限证据。
func TestCommitGapConflict_PreservesCandidateWithoutUtterance(t *testing.T) {
	db := openGapDatabase(t)
	repository := gaprepository.NewRepository(db, database.NewTransactionManager(db))
	claim := newClaimInput("95959595-9595-4959-8959-959595959595", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := repository.ClaimGapAttempt(context.Background(), claim); err != nil {
		t.Fatalf("claim gap 失败：%v", err)
	}
	if err := repository.CommitGapConflict(context.Background(), gaprepository.ConflictInput{
		AttemptID: claim.Attempt.ID, ResponseJSON: `{"no_speech":false,"segments":[{"text":"候选","start_sample":0,"end_sample":16000}]}`,
		ConflictJSON:        `{"attempt_id":"95959595-9595-4959-8959-959595959595","overlaps":[{"utterance_id":"existing","start_sample":0,"end_sample":16000}]}`,
		ProviderLogIDSuffix: "12345678", UpdatedAt: 11,
	}); err != nil {
		t.Fatalf("提交冲突失败：%v", err)
	}
	var utteranceCount int64
	if err := db.Model(&models.Utterance{}).Count(&utteranceCount).Error; err != nil || utteranceCount != 0 {
		t.Fatalf("冲突不得创建 file utterance：count=%d err=%v", utteranceCount, err)
	}
	assertGapAttemptTerminal(t, db, "conflict", "conflict")
}

// TestRecoverInterrupted_FailsRunningAttemptWithoutNetworkRetry 验证重启只收敛本地 running 状态。
func TestRecoverInterrupted_FailsRunningAttemptWithoutNetworkRetry(t *testing.T) {
	db := openGapDatabase(t)
	repository := gaprepository.NewRepository(db, database.NewTransactionManager(db))
	claim := newClaimInput("95959595-9595-4959-8959-959595959595", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := repository.ClaimGapAttempt(context.Background(), claim); err != nil {
		t.Fatalf("claim gap 失败：%v", err)
	}
	if err := repository.RecoverInterrupted(context.Background()); err != nil {
		t.Fatalf("恢复 running attempt 失败：%v", err)
	}
	assertGapAttemptTerminal(t, db, "failed", "failed")
	var attempt models.GapTranscriptionAttempt
	if err := db.Where("id = ?", claim.Attempt.ID).Take(&attempt).Error; err != nil || attempt.LastErrorCode == nil || *attempt.LastErrorCode != "GAP_ATTEMPT_INTERRUPTED" {
		t.Fatalf("恢复错误码不正确：attempt=%#v err=%v", attempt, err)
	}
}

// assertGapAttemptTerminal 验证 gap 与 attempt 同事务到达指定终态。
func assertGapAttemptTerminal(t *testing.T, db *gorm.DB, gapState string, attemptState string) {
	t.Helper()
	var gap models.ASRGap
	if err := db.Where("id = ?", testGapID).Take(&gap).Error; err != nil || gap.State != gapState {
		t.Fatalf("gap 终态错误：gap=%#v err=%v", gap, err)
	}
	var attempt models.GapTranscriptionAttempt
	if err := db.Where("id = ?", "95959595-9595-4959-8959-959595959595").Take(&attempt).Error; err != nil || attempt.State != attemptState {
		t.Fatalf("attempt 终态错误：attempt=%#v err=%v", attempt, err)
	}
}

// int64Pointer 返回测试模型所需的时间指针。
func int64Pointer(value int64) *int64 { return &value }
