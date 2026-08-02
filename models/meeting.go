package models

// MeetingNumberSequence 映射每日设备内会议编号序列。
type MeetingNumberSequence struct {
	ID           string `gorm:"column:id"`
	LocalDate    string `gorm:"column:local_date"`
	DeviceCode   string `gorm:"column:device_code"`
	NextSequence int    `gorm:"column:next_sequence"`
	CreatedAt    int64  `gorm:"column:created_at"`
	UpdatedAt    int64  `gorm:"column:updated_at"`
}

// TableName 返回 MeetingNumberSequence 的显式数据库表名。
func (MeetingNumberSequence) TableName() string { return "meeting_number_sequences" }

// Meeting 映射会议聚合及其正交运行状态。
type Meeting struct {
	ID               string `gorm:"column:id"`
	MeetingNo        string `gorm:"column:meeting_no"`
	Subject          string `gorm:"column:subject"`
	RelativeDir      string `gorm:"column:relative_dir"`
	LocalTimezone    string `gorm:"column:local_timezone"`
	StartedAt        *int64 `gorm:"column:started_at"`
	EndedAt          *int64 `gorm:"column:ended_at"`
	LifecycleState   string `gorm:"column:lifecycle_state"`
	LocalSaveState   string `gorm:"column:local_save_state"`
	RealtimeASRState string `gorm:"column:realtime_asr_state"`
	GapState         string `gorm:"column:gap_state"`
	AgentState       string `gorm:"column:agent_state"`
	MinuteState      string `gorm:"column:minute_state"`
	LANState         string `gorm:"column:lan_state"`
	CreatedAt        int64  `gorm:"column:created_at"`
	UpdatedAt        int64  `gorm:"column:updated_at"`
}

// TableName 返回 Meeting 的显式数据库表名。
func (Meeting) TableName() string { return "meetings" }

// MeetingParticipant 映射会议的成员或临时参会者快照。
type MeetingParticipant struct {
	ID                  string  `gorm:"column:id"`
	MeetingID           string  `gorm:"column:meeting_id"`
	MemberID            *string `gorm:"column:member_id"`
	ParticipantKind     string  `gorm:"column:participant_kind"`
	DisplayNameSnapshot string  `gorm:"column:display_name_snapshot"`
	SortOrder           int     `gorm:"column:sort_order"`
	CreatedAt           int64   `gorm:"column:created_at"`
	UpdatedAt           int64   `gorm:"column:updated_at"`
}

// TableName 返回 MeetingParticipant 的显式数据库表名。
func (MeetingParticipant) TableName() string { return "meeting_participants" }
