package people

// Group 表示可复用的当前成员小组投影。
type Group struct {
	// ID 是不可变小组 UUID。
	ID string
	// Name 是用户输入后去除首尾空白的展示名称。
	Name string
	// NameNormalized 是活动小组名称唯一约束使用的稳定键。
	NameNormalized string
	// DefaultLANEnabled 表示创建会议时的默认访客页开关。
	DefaultLANEnabled bool
	// Members 保留用户提交的当前成员顺序。
	Members []GroupMember
	// CreatedAt 是小组创建时间的 Unix 毫秒值。
	CreatedAt int64
	// UpdatedAt 是小组最近修改时间的 Unix 毫秒值。
	UpdatedAt int64
}

// GroupMember 表示一个小组内成员及其显式顺序。
type GroupMember struct {
	// MemberID 是成员 UUID。
	MemberID string
	// SortOrder 从零开始，且在小组内连续。
	SortOrder int
}
