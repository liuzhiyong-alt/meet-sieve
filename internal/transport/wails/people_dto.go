package wails

import (
	peopledomain "meet-sieve/internal/domain/people"
	peopleservice "meet-sieve/internal/service/people"
)

// CreateMemberDTO 是创建成员的 Wails 输入。
type CreateMemberDTO struct {
	Name  string `json:"name"`
	Notes string `json:"notes"`
}

// UpdateMemberDTO 是修改成员的 Wails 输入。
type UpdateMemberDTO struct {
	Name     string `json:"name"`
	Notes    string `json:"notes"`
	Revision *int64 `json:"revision,omitempty"`
}

// MemberDTO 是成员列表与编辑结果的稳定投影。
type MemberDTO struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Notes               *string `json:"notes,omitempty"`
	AcceptedSampleCount int     `json:"accepted_sample_count"`
	RejectedSampleCount int     `json:"rejected_sample_count"`
	VoiceReadiness      string  `json:"voice_readiness"`
	CreatedAt           int64   `json:"created_at"`
	UpdatedAt           int64   `json:"updated_at"`
	ArchivedAt          *int64  `json:"archived_at,omitempty"`
}

// MemberDetailDTO 是成员独立详情页的引用和能力契约。
type MemberDetailDTO struct {
	Member             MemberDTO `json:"member"`
	Revision           int64     `json:"revision"`
	GroupCount         int64     `json:"group_count"`
	HistoricalMeetings int64     `json:"historical_meetings"`
	CanArchive         bool      `json:"can_archive"`
	CanRestore         bool      `json:"can_restore"`
	CanDelete          bool      `json:"can_delete"`
}

// GroupMemberDTO 是小组内显式排序的成员引用。
type GroupMemberDTO struct {
	MemberID  string `json:"member_id"`
	SortOrder int    `json:"sort_order"`
}

// CreateGroupDTO 是创建小组的 Wails 输入。
type CreateGroupDTO struct {
	Name              string   `json:"name"`
	DefaultLANEnabled bool     `json:"default_lan_enabled"`
	MemberIDs         []string `json:"member_ids"`
}

// UpdateGroupDTO 是完整替换小组当前设置的 Wails 输入。
type UpdateGroupDTO struct {
	Name              string   `json:"name"`
	DefaultLANEnabled bool     `json:"default_lan_enabled"`
	MemberIDs         []string `json:"member_ids"`
	Revision          *int64   `json:"revision,omitempty"`
}

// GroupDTO 是小组与有序成员关系的稳定投影。
type GroupDTO struct {
	ID                string           `json:"id"`
	Name              string           `json:"name"`
	DefaultLANEnabled bool             `json:"default_lan_enabled"`
	Members           []GroupMemberDTO `json:"members"`
	CreatedAt         int64            `json:"created_at"`
	UpdatedAt         int64            `json:"updated_at"`
}

// MeetingMemberOptionDTO 是创建会议可选成员的只读投影。
type MeetingMemberOptionDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortOrder      int    `json:"sort_order"`
	VoiceReadiness string `json:"voice_readiness"`
}

// MeetingGroupOptionDTO 是创建会议可选小组的只读投影。
type MeetingGroupOptionDTO struct {
	ID                string                   `json:"id"`
	Name              string                   `json:"name"`
	DefaultLANEnabled bool                     `json:"default_lan_enabled"`
	Members           []MeetingMemberOptionDTO `json:"members"`
}

// MeetingPeopleOptionsDTO 是 Step 3 创建会议可直接消费的候选契约。
type MeetingPeopleOptionsDTO struct {
	Groups  []MeetingGroupOptionDTO  `json:"groups"`
	Members []MeetingMemberOptionDTO `json:"members"`
}

// mapMemberDTO 将领域成员转换为前端契约。
func mapMemberDTO(member peopledomain.Member) MemberDTO {
	return MemberDTO{
		ID: member.ID, Name: member.Name, Notes: member.Notes,
		AcceptedSampleCount: member.VoiceSummary.AcceptedSampleCount,
		RejectedSampleCount: member.VoiceSummary.RejectedSampleCount,
		VoiceReadiness:      string(member.VoiceSummary.Readiness),
		CreatedAt:           member.CreatedAt, UpdatedAt: member.UpdatedAt, ArchivedAt: member.ArchivedAt,
	}
}

// mapMemberDetailDTO 转换成员详情业务投影。
func mapMemberDetailDTO(detail peopleservice.MemberDetail) MemberDetailDTO {
	return MemberDetailDTO{Member: mapMemberDTO(detail.Member), Revision: detail.Revision, GroupCount: detail.GroupCount,
		HistoricalMeetings: detail.HistoricalMeetings, CanArchive: detail.CanArchive, CanRestore: detail.CanRestore, CanDelete: detail.CanDelete}
}

// mapMemberDTOs 批量转换成员投影。
func mapMemberDTOs(members []peopledomain.Member) []MemberDTO {
	result := make([]MemberDTO, 0, len(members))
	for _, member := range members {
		result = append(result, mapMemberDTO(member))
	}
	return result
}

// mapGroupDTO 将领域小组转换为前端契约。
func mapGroupDTO(group peopledomain.Group) GroupDTO {
	members := make([]GroupMemberDTO, 0, len(group.Members))
	for _, member := range group.Members {
		members = append(members, GroupMemberDTO{MemberID: member.MemberID, SortOrder: member.SortOrder})
	}
	return GroupDTO{ID: group.ID, Name: group.Name, DefaultLANEnabled: group.DefaultLANEnabled, Members: members, CreatedAt: group.CreatedAt, UpdatedAt: group.UpdatedAt}
}

// mapGroupDTOs 批量转换小组投影。
func mapGroupDTOs(groups []peopledomain.Group) []GroupDTO {
	result := make([]GroupDTO, 0, len(groups))
	for _, group := range groups {
		result = append(result, mapGroupDTO(group))
	}
	return result
}

// mapMeetingPeopleOptionsDTO 转换会议候选领域投影。
func mapMeetingPeopleOptionsDTO(options peopledomain.MeetingPeopleOptions) MeetingPeopleOptionsDTO {
	members := make([]MeetingMemberOptionDTO, 0, len(options.Members))
	for _, member := range options.Members {
		members = append(members, mapMeetingMemberOptionDTO(member))
	}
	groups := make([]MeetingGroupOptionDTO, 0, len(options.Groups))
	for _, group := range options.Groups {
		groupMembers := make([]MeetingMemberOptionDTO, 0, len(group.Members))
		for _, member := range group.Members {
			groupMembers = append(groupMembers, mapMeetingMemberOptionDTO(member))
		}
		groups = append(groups, MeetingGroupOptionDTO{ID: group.ID, Name: group.Name, DefaultLANEnabled: group.DefaultLANEnabled, Members: groupMembers})
	}
	return MeetingPeopleOptionsDTO{Groups: groups, Members: members}
}

// mapMeetingMemberOptionDTO 转换单个会议成员候选。
func mapMeetingMemberOptionDTO(member peopledomain.MeetingMemberOption) MeetingMemberOptionDTO {
	return MeetingMemberOptionDTO{ID: member.ID, Name: member.Name, SortOrder: member.SortOrder, VoiceReadiness: string(member.VoiceReadiness)}
}
