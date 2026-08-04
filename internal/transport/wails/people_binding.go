package wails

import (
	"context"

	peopleservice "meet-sieve/internal/service/people"
)

// PeopleServiceProvider 只在工作目录 ready 后提供当前数据库对应的业务服务。
type PeopleServiceProvider func() (*peopleservice.MemberService, *peopleservice.GroupService, error)

// PeopleBinding 暴露成员与小组当前资料操作，不泄漏数据库模型。
type PeopleBinding struct {
	services PeopleServiceProvider
	boundary *Boundary
}

// NewPeopleBinding 创建成员与小组 binding。
func NewPeopleBinding(services PeopleServiceProvider, boundary *Boundary) *PeopleBinding {
	return &PeopleBinding{services: services, boundary: boundary}
}

// ListMembers 返回全部活动成员。
func (binding *PeopleBinding) ListMembers() Result[[]MemberDTO] {
	return Invoke(binding.boundary, "wails.people.list_members", func(_ string) ([]MemberDTO, error) {
		members, _, err := binding.services()
		if err != nil {
			return nil, err
		}
		result, err := members.ListActiveMembers(context.Background())
		return mapMemberDTOs(result), err
	})
}

// GetMember 返回单个活动成员详情。
func (binding *PeopleBinding) GetMember(memberID string) Result[MemberDTO] {
	return Invoke(binding.boundary, "wails.people.get_member", func(_ string) (MemberDTO, error) {
		members, _, err := binding.services()
		if err != nil {
			return MemberDTO{}, err
		}
		result, err := members.GetMember(context.Background(), memberID)
		return mapMemberDTO(result), err
	})
}

// GetMemberDetail 返回活动或归档成员的独立详情投影。
func (binding *PeopleBinding) GetMemberDetail(memberID string) Result[MemberDetailDTO] {
	return Invoke(binding.boundary, "wails.people.get_member_detail", func(_ string) (MemberDetailDTO, error) {
		members, _, err := binding.services()
		if err != nil {
			return MemberDetailDTO{}, err
		}
		result, err := members.GetMemberDetail(context.Background(), memberID)
		return mapMemberDetailDTO(result), err
	})
}

// CreateMember 创建活动成员。
func (binding *PeopleBinding) CreateMember(input CreateMemberDTO) Result[MemberDTO] {
	return Invoke(binding.boundary, "wails.people.create_member", func(_ string) (MemberDTO, error) {
		members, _, err := binding.services()
		if err != nil {
			return MemberDTO{}, err
		}
		result, err := members.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: input.Name, Notes: input.Notes})
		return mapMemberDTO(result), err
	})
}

// UpdateMember 修改活动成员名称与备注。
func (binding *PeopleBinding) UpdateMember(memberID string, input UpdateMemberDTO) Result[MemberDTO] {
	return Invoke(binding.boundary, "wails.people.update_member", func(_ string) (MemberDTO, error) {
		members, _, err := binding.services()
		if err != nil {
			return MemberDTO{}, err
		}
		revision := int64(0)
		if input.Revision != nil {
			revision = *input.Revision
		}
		result, err := members.UpdateMember(context.Background(), memberID, peopleservice.UpdateMemberInput{Name: input.Name, Notes: input.Notes, Revision: revision})
		return mapMemberDTO(result), err
	})
}

// DeleteAllMemberVoiceSamples 删除显式声纹样本但保留成员和历史会议。
func (binding *PeopleBinding) DeleteAllMemberVoiceSamples(memberID string) Result[bool] {
	return Invoke(binding.boundary, "wails.people.delete_all_voice_samples", func(_ string) (bool, error) {
		members, _, err := binding.services()
		if err != nil {
			return false, err
		}
		err = members.DeleteAllVoiceSamples(context.Background(), memberID)
		return err == nil, err
	})
}

// DeleteMember 删除当前成员；后端根据历史引用决定墓碑或硬删除。
func (binding *PeopleBinding) DeleteMember(memberID string) Result[bool] {
	return Invoke(binding.boundary, "wails.people.delete_member", func(_ string) (bool, error) {
		members, _, err := binding.services()
		if err != nil {
			return false, err
		}
		err = members.DeleteMember(context.Background(), memberID)
		return err == nil, err
	})
}

// ListGroups 返回全部当前小组及有序成员关系。
func (binding *PeopleBinding) ListGroups() Result[[]GroupDTO] {
	return Invoke(binding.boundary, "wails.people.list_groups", func(_ string) ([]GroupDTO, error) {
		_, groups, err := binding.services()
		if err != nil {
			return nil, err
		}
		result, err := groups.ListGroups(context.Background())
		return mapGroupDTOs(result), err
	})
}

// GetGroup 返回单个当前小组详情。
func (binding *PeopleBinding) GetGroup(groupID string) Result[GroupDTO] {
	return Invoke(binding.boundary, "wails.people.get_group", func(_ string) (GroupDTO, error) {
		_, groups, err := binding.services()
		if err != nil {
			return GroupDTO{}, err
		}
		result, err := groups.GetGroup(context.Background(), groupID)
		return mapGroupDTO(result), err
	})
}

// CreateGroup 创建小组并保存提交的成员顺序。
func (binding *PeopleBinding) CreateGroup(input CreateGroupDTO) Result[GroupDTO] {
	return Invoke(binding.boundary, "wails.people.create_group", func(_ string) (GroupDTO, error) {
		_, groups, err := binding.services()
		if err != nil {
			return GroupDTO{}, err
		}
		result, err := groups.CreateGroup(context.Background(), peopleservice.CreateGroupInput{
			Name: input.Name, DefaultLANEnabled: input.DefaultLANEnabled, MemberIDs: input.MemberIDs,
		})
		return mapGroupDTO(result), err
	})
}

// UpdateGroup 完整替换小组当前设置与成员顺序。
func (binding *PeopleBinding) UpdateGroup(groupID string, input UpdateGroupDTO) Result[GroupDTO] {
	return Invoke(binding.boundary, "wails.people.update_group", func(_ string) (GroupDTO, error) {
		_, groups, err := binding.services()
		if err != nil {
			return GroupDTO{}, err
		}
		revision := int64(0)
		if input.Revision != nil {
			revision = *input.Revision
		}
		result, err := groups.UpdateGroup(context.Background(), groupID, peopleservice.UpdateGroupInput{
			Name: input.Name, DefaultLANEnabled: input.DefaultLANEnabled, MemberIDs: input.MemberIDs, Revision: revision,
		})
		return mapGroupDTO(result), err
	})
}

// DeleteGroup 删除小组及当前关系，不删除成员。
func (binding *PeopleBinding) DeleteGroup(groupID string) Result[bool] {
	return Invoke(binding.boundary, "wails.people.delete_group", func(_ string) (bool, error) {
		_, groups, err := binding.services()
		if err != nil {
			return false, err
		}
		err = groups.DeleteGroup(context.Background(), groupID)
		return err == nil, err
	})
}

// GetMeetingPeopleOptions 返回创建会议需要的活动成员与有序小组候选。
func (binding *PeopleBinding) GetMeetingPeopleOptions() Result[MeetingPeopleOptionsDTO] {
	return Invoke(binding.boundary, "wails.people.get_meeting_options", func(_ string) (MeetingPeopleOptionsDTO, error) {
		members, groups, err := binding.services()
		if err != nil {
			return MeetingPeopleOptionsDTO{}, err
		}
		result, err := peopleservice.NewMeetingPeopleService(members, groups).GetOptions(context.Background())
		return mapMeetingPeopleOptionsDTO(result), err
	})
}
