package people

import (
	"context"
	"fmt"

	peopledomain "meet-sieve/internal/domain/people"
	"meet-sieve/internal/infra/apperr"
)

// MeetingPeopleService 组合活动成员与小组，不创建会议或参会者快照。
type MeetingPeopleService struct {
	members *MemberService
	groups  *GroupService
}

// NewMeetingPeopleService 创建会议候选只读服务。
func NewMeetingPeopleService(members *MemberService, groups *GroupService) *MeetingPeopleService {
	return &MeetingPeopleService{members: members, groups: groups}
}

// GetOptions 返回稳定排序的会议人员候选，并拒绝失效小组关系。
func (service *MeetingPeopleService) GetOptions(ctx context.Context) (peopledomain.MeetingPeopleOptions, error) {
	if service == nil || service.members == nil || service.groups == nil {
		return peopledomain.MeetingPeopleOptions{}, fmt.Errorf("会议候选服务依赖未初始化")
	}
	members, err := service.members.ListActiveMembers(ctx)
	if err != nil {
		return peopledomain.MeetingPeopleOptions{}, err
	}
	groups, err := service.groups.ListGroups(ctx)
	if err != nil {
		return peopledomain.MeetingPeopleOptions{}, err
	}
	memberOptions, memberByID := buildMeetingMembers(members)
	groupOptions, err := buildMeetingGroups(groups, memberByID)
	if err != nil {
		return peopledomain.MeetingPeopleOptions{}, err
	}
	return peopledomain.MeetingPeopleOptions{Groups: groupOptions, Members: memberOptions}, nil
}

// buildMeetingMembers 保留成员列表顺序并建立小组关系解析索引。
func buildMeetingMembers(members []peopledomain.Member) ([]peopledomain.MeetingMemberOption, map[string]peopledomain.MeetingMemberOption) {
	options := make([]peopledomain.MeetingMemberOption, 0, len(members))
	byID := make(map[string]peopledomain.MeetingMemberOption, len(members))
	for _, member := range members {
		option := peopledomain.MeetingMemberOption{ID: member.ID, Name: member.Name, VoiceReadiness: member.VoiceSummary.Readiness}
		options = append(options, option)
		byID[member.ID] = option
	}
	return options, byID
}

// buildMeetingGroups 按显式 sort_order 合并成员名称与 readiness。
func buildMeetingGroups(groups []peopledomain.Group, memberByID map[string]peopledomain.MeetingMemberOption) ([]peopledomain.MeetingGroupOption, error) {
	result := make([]peopledomain.MeetingGroupOption, 0, len(groups))
	for _, group := range groups {
		option := peopledomain.MeetingGroupOption{ID: group.ID, Name: group.Name, DefaultLANEnabled: group.DefaultLANEnabled, Members: make([]peopledomain.MeetingMemberOption, 0, len(group.Members))}
		for _, relation := range group.Members {
			member, found := memberByID[relation.MemberID]
			if !found {
				return nil, apperr.Biz(apperr.CodeDatabaseIntegrityFailed, apperr.WithOp("people.meeting_options.group_member"))
			}
			member.SortOrder = relation.SortOrder
			option.Members = append(option.Members, member)
		}
		result = append(result, option)
	}
	return result, nil
}
