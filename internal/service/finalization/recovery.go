package finalization

import (
	"context"
	"fmt"

	"meet-sieve/internal/infra/clock"
)

// GapRecovery 收敛异常退出时仍在运行的补转写请求。
type GapRecovery interface {
	RecoverInterruptedAt(context.Context, int64) error
}

// MinutesRecovery 收敛异常退出时仍在运行的纪要 turn。
type MinutesRecovery interface {
	RecoverInterruptedTurns(context.Context, string, int64) error
}

// AgentRecovery 收敛异常退出时仍在运行的结束同步。
type AgentRecovery interface {
	RecoverFinalSyncState(context.Context, string, int64) error
}

// RecoveryCoordinator 只执行 SQLite 状态收敛，绝不发起外部请求。
type RecoveryCoordinator struct {
	gaps    GapRecovery
	minutes MinutesRecovery
	agent   AgentRecovery
	clock   clock.Clock
}

// NewRecoveryCoordinator 创建会后任务恢复协调器。
func NewRecoveryCoordinator(gaps GapRecovery, minutes MinutesRecovery, agent AgentRecovery, appClock clock.Clock) *RecoveryCoordinator {
	return &RecoveryCoordinator{gaps: gaps, minutes: minutes, agent: agent, clock: appClock}
}

// RecoverInterrupted 按 gap、纪要、结束同步顺序收敛本地状态。
func (coordinator *RecoveryCoordinator) RecoverInterrupted(ctx context.Context) error {
	if coordinator == nil || coordinator.gaps == nil || coordinator.minutes == nil || coordinator.agent == nil || coordinator.clock == nil {
		return fmt.Errorf("会后恢复协调器依赖无效")
	}
	now := coordinator.clock.Now().UnixMilli()
	if err := coordinator.gaps.RecoverInterruptedAt(ctx, now); err != nil {
		return err
	}
	if err := coordinator.minutes.RecoverInterruptedTurns(ctx, "MINUTES_GENERATION_INTERRUPTED", now); err != nil {
		return err
	}
	return coordinator.agent.RecoverFinalSyncState(ctx, "AGENT_FINAL_SYNC_INTERRUPTED", now)
}
