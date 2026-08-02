package transcript

import (
	"context"
	"fmt"

	transcriptdomain "meet-sieve/internal/domain/transcript"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	transcriptrepository "meet-sieve/internal/repository/transcript"

	"gorm.io/gorm"
)

// RecoveryServiceDependencies 描述崩溃后 ASR session 与 gap 恢复依赖。
type RecoveryServiceDependencies struct {
	// Repository 收敛 session 与会议状态。
	Repository *transcriptrepository.Repository
	// Transactions 提供短事务。
	Transactions *database.TransactionManager
	// Events 幂等生成 recovery gap。
	Events *EventService
	// Clock 提供恢复审计时间。
	Clock clock.Clock
}

// RecoveryService 保留已有 final，并为未覆盖录音尾部生成唯一 recovery gap。
type RecoveryService struct {
	dependencies RecoveryServiceDependencies
}

// NewRecoveryService 创建 ASR 恢复服务；构造阶段不访问数据库。
func NewRecoveryService(dependencies RecoveryServiceDependencies) *RecoveryService {
	return &RecoveryService{dependencies: dependencies}
}

// Recover 收敛遗留 session，并从最后 final 到录音末尾生成幂等 gap。
func (service *RecoveryService) Recover(ctx context.Context, meetingID string, recordingEndSample int64) error {
	if service == nil || ctx == nil || meetingID == "" || recordingEndSample < 0 || service.dependencies.Repository == nil || service.dependencies.Transactions == nil || service.dependencies.Events == nil || service.dependencies.Clock == nil {
		return fmt.Errorf("ASR 恢复服务依赖或输入无效")
	}
	var lastFinal int64
	err := service.dependencies.Transactions.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var recoverErr error
		lastFinal, recoverErr = service.dependencies.Repository.FailActiveSessions(ctx, tx, meetingID, service.dependencies.Clock.Now().UnixMilli())
		return recoverErr
	})
	if err != nil {
		return err
	}
	if lastFinal >= recordingEndSample {
		return nil
	}
	rangeValue, err := transcriptdomain.NewSampleRange(lastFinal, recordingEndSample)
	if err != nil {
		return err
	}
	_, err = service.dependencies.Events.PersistGap(ctx, GapInput{MeetingID: meetingID, Range: rangeValue, Reason: transcriptdomain.GapRecovery})
	return err
}
