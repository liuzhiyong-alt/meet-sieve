package transcript

import (
	"context"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/models"
)

// TestRecoveryServicePreservesFinalAndCreatesUniqueTailGap 验证崩溃恢复收敛 session 且重复执行不重复 gap。
func TestRecoveryServicePreservesFinalAndCreatesUniqueTailGap(t *testing.T) {
	db, repository, events := newRealtimeCoordinatorDatabase(t)
	session := models.ASRSession{ID: testSessionID, MeetingID: testMeetingID, Provider: "volcano", State: "streaming", StartedAt: 1_000, TransportMode: "seed_v1", InputStartSample: 0, LastSentSample: 32000, LastFinalSample: 16000, CreatedAt: 1_000, UpdatedAt: 1_000}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("创建恢复 ASR session 失败：%v", err)
	}
	service := NewRecoveryService(RecoveryServiceDependencies{Repository: repository, Transactions: database.NewTransactionManager(db), Events: events, Clock: clock.NewFixed(time.UnixMilli(2_000))})
	for range 2 {
		if err := service.Recover(context.Background(), testMeetingID, 48000); err != nil {
			t.Fatalf("恢复 ASR session 失败：%v", err)
		}
	}
	var recovered models.ASRSession
	if err := db.Where("id = ?", testSessionID).Take(&recovered).Error; err != nil {
		t.Fatalf("读取恢复 session 失败：%v", err)
	}
	if recovered.State != "failed" || recovered.LastFinalSample != 16000 {
		t.Fatalf("恢复 session 终态错误：%+v", recovered)
	}
	var gaps []models.ASRGap
	if err := db.Where("meeting_id = ?", testMeetingID).Find(&gaps).Error; err != nil {
		t.Fatalf("读取 recovery gap 失败：%v", err)
	}
	if len(gaps) != 1 || gaps[0].StartSample != 16000 || gaps[0].EndSample != 48000 || gaps[0].Reason != "recovery" {
		t.Fatalf("recovery gap 应唯一且范围准确：%+v", gaps)
	}
}
