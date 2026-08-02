package wails

import (
	"time"

	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/app/health"

	"go.uber.org/zap"
)

// SystemBinding 提供 Step 0 唯一允许暴露给前端的系统级能力。
type SystemBinding struct {
	health   *health.Registry
	boundary *Boundary
}

// NewSystemBinding 创建系统 binding。
func NewSystemBinding(registry *health.Registry, boundary *Boundary) *SystemBinding {
	return &SystemBinding{health: registry, boundary: boundary}
}

// GetBuildInfo 返回当前构建元数据。
func (b *SystemBinding) GetBuildInfo() Result[buildinfo.Info] {
	return Invoke(b.boundary, "wails.system.get_build_info", func(_ string) (buildinfo.Info, error) {
		return buildinfo.Current(), nil
	})
}

// GetHealth 返回当前应用健康状态。
func (b *SystemBinding) GetHealth() Result[health.Snapshot] {
	return Invoke(b.boundary, "wails.system.get_health", func(_ string) (health.Snapshot, error) {
		return b.health.Get(), nil
	})
}

// RunEventRoundTrip 创建仅供自动化验证的事件 envelope。
func (b *SystemBinding) RunEventRoundTrip(payload string) Result[AppEvent[string]] {
	return Invoke(b.boundary, "wails.system.event_round_trip", func(requestID string) (AppEvent[string], error) {
		event := NewEvent("system.event.roundtrip", time.Now(), 0, payload)
		if payload == "step0-smoke" {
			b.boundary.logInfo(
				"Wails event round-trip completed",
				requestID,
				zap.String("payload", payload),
			)
		}
		return event, nil
	})
}
