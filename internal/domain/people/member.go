package people

import voicedomain "meet-sieve/internal/domain/voice"

// Member 表示成员资料的领域投影。
type Member struct {
	// ID 是不可变成员 UUID。
	ID string
	// Name 是用户输入后去除首尾空白的展示名称。
	Name string
	// NameNormalized 是活动成员名称唯一约束使用的稳定键。
	NameNormalized string
	// Notes 是可选成员备注。
	Notes *string
	// VoiceSummary 是当前本地样本相对于声纹模型的状态汇总。
	VoiceSummary VoiceSummary
	// CreatedAt 是成员创建时间的 Unix 毫秒值。
	CreatedAt int64
	// UpdatedAt 是成员最近修改时间的 Unix 毫秒值。
	UpdatedAt int64
	// ArchivedAt 非空表示成员已归档。
	ArchivedAt *int64
}

// VoiceSummary 是成员声纹样本数量与可用状态的只读投影。
type VoiceSummary struct {
	AcceptedSampleCount int
	RejectedSampleCount int
	Readiness           voicedomain.Readiness
}
