// Package voice 定义声纹资料的领域状态和值对象。
package voice

// Readiness 表示成员声纹资料相对于当前模型的可用状态。
type Readiness string

const (
	// ReadinessNotEnrolled 表示没有质量可用的声纹样本。
	ReadinessNotEnrolled Readiness = "not_enrolled"
	// ReadinessProcessing 表示正在评估样本或生成 embedding。
	ReadinessProcessing Readiness = "processing"
	// ReadinessReady 表示至少一个已接受样本具有当前模型 embedding。
	ReadinessReady Readiness = "ready"
	// ReadinessRebuildRequired 表示当前模型的 embedding 尚未全部重建。
	ReadinessRebuildRequired Readiness = "rebuild_required"
	// ReadinessUnavailable 表示模型或运行时当前不可用。
	ReadinessUnavailable Readiness = "unavailable"
)

// IsValid 判断 readiness 是否为技术方案登记的值。
func (readiness Readiness) IsValid() bool {
	switch readiness {
	case ReadinessNotEnrolled, ReadinessProcessing, ReadinessReady, ReadinessRebuildRequired, ReadinessUnavailable:
		return true
	default:
		return false
	}
}
