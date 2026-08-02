package people

import voicedomain "meet-sieve/internal/domain/voice"

// MeetingPeopleOptions 是创建会议可直接消费的活动成员与小组只读投影。
type MeetingPeopleOptions struct {
	Groups  []MeetingGroupOption
	Members []MeetingMemberOption
}

// MeetingMemberOption 是不影响参会资格的成员声纹状态投影。
type MeetingMemberOption struct {
	ID             string
	Name           string
	SortOrder      int
	VoiceReadiness voicedomain.Readiness
}

// MeetingGroupOption 是带默认访客页设置和显式成员顺序的小组候选。
type MeetingGroupOption struct {
	ID                string
	Name              string
	DefaultLANEnabled bool
	Members           []MeetingMemberOption
}
