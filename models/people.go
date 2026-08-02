package models

// Member 映射可归档的本地成员。
type Member struct {
	ID             string  `gorm:"column:id"`
	Name           string  `gorm:"column:name"`
	NameNormalized string  `gorm:"column:name_normalized"`
	Notes          *string `gorm:"column:notes"`
	CreatedAt      int64   `gorm:"column:created_at"`
	UpdatedAt      int64   `gorm:"column:updated_at"`
	ArchivedAt     *int64  `gorm:"column:archived_at"`
}

// TableName 返回 Member 的显式数据库表名。
func (Member) TableName() string { return "members" }

// Group 映射可归档的小组。
type Group struct {
	ID                string `gorm:"column:id"`
	Name              string `gorm:"column:name"`
	NameNormalized    string `gorm:"column:name_normalized"`
	DefaultLANEnabled bool   `gorm:"column:default_lan_enabled"`
	CreatedAt         int64  `gorm:"column:created_at"`
	UpdatedAt         int64  `gorm:"column:updated_at"`
	ArchivedAt        *int64 `gorm:"column:archived_at"`
}

// TableName 返回 Group 的显式数据库表名。
func (Group) TableName() string { return "groups" }

// GroupMember 映射小组与成员的有序关系。
type GroupMember struct {
	ID        string `gorm:"column:id"`
	GroupID   string `gorm:"column:group_id"`
	MemberID  string `gorm:"column:member_id"`
	SortOrder int    `gorm:"column:sort_order"`
	CreatedAt int64  `gorm:"column:created_at"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

// TableName 返回 GroupMember 的显式数据库表名。
func (GroupMember) TableName() string { return "group_members" }
