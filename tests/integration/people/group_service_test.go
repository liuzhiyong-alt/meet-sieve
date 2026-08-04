package people_test

import (
	"context"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	peoplerepository "meet-sieve/internal/repository/people"
	peopleservice "meet-sieve/internal/service/people"

	"gorm.io/gorm"
)

const createdGroupID = "77777777-7777-4777-8777-777777777777"

// TestGroupService_CreatePreservesSubmittedMemberOrder 验证创建小组时成员关系使用提交顺序生成连续 sort_order。
func TestGroupService_CreatePreservesSubmittedMemberOrder(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	firstMember := newMemberService(db, transactions, createdMemberID)
	if _, err := firstMember.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "张三"}); err != nil {
		t.Fatalf("准备第一个成员失败：%v", err)
	}
	secondMember := newMemberService(db, transactions, duplicateMemberID)
	if _, err := secondMember.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "李四"}); err != nil {
		t.Fatalf("准备第二个成员失败：%v", err)
	}

	service := peopleservice.NewGroupService(peopleservice.GroupServiceDependencies{
		Repository:   peoplerepository.NewGroupRepository(db),
		Members:      peoplerepository.NewMemberRepository(db),
		Transactions: transactions,
		IDs:          identity.NewFixedGenerator(createdGroupID, "88888888-8888-4888-8888-888888888888", "99999999-9999-4999-8999-999999999999"),
		Clock:        fixedTestClock(),
	})
	group, err := service.CreateGroup(context.Background(), peopleservice.CreateGroupInput{
		Name:              "研发组",
		DefaultLANEnabled: true,
		MemberIDs:         []string{duplicateMemberID, createdMemberID},
	})
	if err != nil {
		t.Fatalf("创建小组失败：%v", err)
	}
	if group.ID != createdGroupID || !group.DefaultLANEnabled || len(group.Members) != 2 {
		t.Fatalf("小组投影不正确：%+v", group)
	}
	if group.Members[0].MemberID != duplicateMemberID || group.Members[0].SortOrder != 0 || group.Members[1].MemberID != createdMemberID || group.Members[1].SortOrder != 1 {
		t.Fatalf("小组成员顺序不正确：%+v", group.Members)
	}
}

// TestGroupService_CreateRejectsArchivedMember 验证归档成员不能加入新小组。
func TestGroupService_CreateRejectsArchivedMember(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	memberService := newMemberService(db, transactions, createdMemberID)
	if _, err := memberService.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "已归档成员"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	if err := memberService.ArchiveMember(context.Background(), createdMemberID); err != nil {
		t.Fatalf("归档成员失败：%v", err)
	}
	service := newGroupService(db, transactions, createdGroupID, "88888888-8888-4888-8888-888888888888", "99999999-9999-4999-8999-999999999999")

	_, err := service.CreateGroup(context.Background(), peopleservice.CreateGroupInput{Name: "测试小组", MemberIDs: []string{createdMemberID}})
	if got := apperr.Normalize(err); got.ErrorCode != "GROUP_MEMBER_INVALID" || got.Kind != apperr.KindBusiness {
		t.Fatalf("归档成员错误语义不正确：%+v", got)
	}
}

// TestGroupService_DeleteKeepsMembers 验证删除小组不会删除成员资料。
func TestGroupService_DeleteKeepsMembers(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	memberService := newMemberService(db, transactions, createdMemberID)
	if _, err := memberService.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "保留成员"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	service := newGroupService(db, transactions, createdGroupID, "88888888-8888-4888-8888-888888888888", "99999999-9999-4999-8999-999999999999")
	if _, err := service.CreateGroup(context.Background(), peopleservice.CreateGroupInput{Name: "待删除小组", MemberIDs: []string{createdMemberID}}); err != nil {
		t.Fatalf("准备小组失败：%v", err)
	}

	if err := service.DeleteGroup(context.Background(), createdGroupID); err != nil {
		t.Fatalf("删除小组失败：%v", err)
	}
	var groupCount int64
	if err := db.Table("groups").Where("id = ?", createdGroupID).Count(&groupCount).Error; err != nil {
		t.Fatalf("统计小组失败：%v", err)
	}
	if groupCount != 0 {
		t.Fatalf("小组必须被删除：count=%d", groupCount)
	}
	var memberCount int64
	if err := db.Table("members").Where("id = ?", createdMemberID).Count(&memberCount).Error; err != nil {
		t.Fatalf("统计成员失败：%v", err)
	}
	if memberCount != 1 {
		t.Fatalf("删除小组不得删除成员：count=%d", memberCount)
	}
}

// TestGroupService_UpdateReplacesMembersInSubmittedOrder 验证编辑小组时以用户提交顺序原子替换成员关系。
func TestGroupService_UpdateReplacesMembersInSubmittedOrder(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	firstMember := newMemberService(db, transactions, createdMemberID)
	if _, err := firstMember.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "张三"}); err != nil {
		t.Fatalf("准备第一个成员失败：%v", err)
	}
	secondMember := newMemberService(db, transactions, duplicateMemberID)
	if _, err := secondMember.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "李四"}); err != nil {
		t.Fatalf("准备第二个成员失败：%v", err)
	}
	service := newGroupService(db, transactions,
		createdGroupID,
		"88888888-8888-4888-8888-888888888888",
		"99999999-9999-4999-8999-999999999999",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	)
	if _, err := service.CreateGroup(context.Background(), peopleservice.CreateGroupInput{Name: "研发组", MemberIDs: []string{createdMemberID}}); err != nil {
		t.Fatalf("准备小组失败：%v", err)
	}

	updated, err := service.UpdateGroup(context.Background(), createdGroupID, peopleservice.UpdateGroupInput{
		Name:              "产品研发组",
		DefaultLANEnabled: true,
		MemberIDs:         []string{duplicateMemberID, createdMemberID},
	})
	if err != nil {
		t.Fatalf("修改小组失败：%v", err)
	}
	if updated.Name != "产品研发组" || !updated.DefaultLANEnabled || len(updated.Members) != 2 {
		t.Fatalf("修改后小组投影不正确：%+v", updated)
	}
	if updated.CreatedAt == 0 {
		t.Fatalf("修改后投影丢失原始创建时间：%+v", updated)
	}
	if updated.Members[0].MemberID != duplicateMemberID || updated.Members[0].SortOrder != 0 || updated.Members[1].MemberID != createdMemberID || updated.Members[1].SortOrder != 1 {
		t.Fatalf("修改后成员顺序不正确：%+v", updated.Members)
	}
}

// TestGroupService_UpdateRejectsStaleRevision 验证小组详情不会覆盖并发变化。
func TestGroupService_UpdateRejectsStaleRevision(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	memberService := newMemberService(db, transactions, createdMemberID)
	if _, err := memberService.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "成员"}); err != nil {
		t.Fatal(err)
	}
	service := newGroupService(
		db, transactions, createdGroupID,
		"88888888-8888-4888-8888-888888888888",
		"99999999-9999-4999-8999-999999999999",
	)
	created, err := service.CreateGroup(context.Background(), peopleservice.CreateGroupInput{Name: "初始小组", MemberIDs: []string{createdMemberID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("groups").Where("id = ?", createdGroupID).Update("updated_at", created.UpdatedAt+1).Error; err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdateGroup(context.Background(), createdGroupID, peopleservice.UpdateGroupInput{Name: "覆盖小组", MemberIDs: []string{createdMemberID}, Revision: created.UpdatedAt})
	if got := apperr.Normalize(err); got.ErrorCode != apperr.CodePeopleRevisionConflict.ErrorCode {
		t.Fatalf("旧 revision 必须返回人员冲突：err=%v normalized=%+v", err, got)
	}
}

// TestGroupService_ListGroupsUsesConfirmedOrder 验证小组列表使用已确认的稳定创建顺序。
func TestGroupService_ListGroupsUsesConfirmedOrder(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	older := peopleservice.NewGroupService(peopleservice.GroupServiceDependencies{
		Repository: peoplerepository.NewGroupRepository(db), Members: peoplerepository.NewMemberRepository(db),
		Transactions: transactions, IDs: identity.NewFixedGenerator(createdGroupID),
		Clock: clock.NewFixed(time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)),
	})
	if _, err := older.CreateGroup(context.Background(), peopleservice.CreateGroupInput{Name: "子小组"}); err != nil {
		t.Fatalf("准备较早小组失败：%v", err)
	}
	newerID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	newer := newGroupService(db, transactions, newerID)
	if _, err := newer.CreateGroup(context.Background(), peopleservice.CreateGroupInput{Name: "阿小组"}); err != nil {
		t.Fatalf("准备较新小组失败：%v", err)
	}

	groups, err := newer.ListGroups(context.Background())
	if err != nil {
		t.Fatalf("查询小组失败：%v", err)
	}
	if len(groups) != 2 || groups[0].ID != newerID || groups[1].ID != createdGroupID {
		t.Fatalf("小组列表顺序不正确：%+v", groups)
	}
}

// TestMeetingPeopleOptions_ContainsOnlyActiveMembersAndOrderedGroups 验证会议候选不创建会议且保持当前关系顺序。
func TestMeetingPeopleOptions_ContainsOnlyActiveMembersAndOrderedGroups(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	members := newMemberService(db, transactions, createdMemberID)
	if _, err := members.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "候选成员"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	groups := newGroupService(db, transactions, createdGroupID, "88888888-8888-4888-8888-888888888888")
	if _, err := groups.CreateGroup(context.Background(), peopleservice.CreateGroupInput{Name: "候选小组", MemberIDs: []string{createdMemberID}}); err != nil {
		t.Fatalf("准备小组失败：%v", err)
	}
	service := peopleservice.NewMeetingPeopleService(members, groups)

	options, err := service.GetOptions(context.Background())
	if err != nil {
		t.Fatalf("查询会议候选失败：%v", err)
	}
	if len(options.Members) != 1 || len(options.Groups) != 1 || options.Groups[0].Members[0].ID != createdMemberID {
		t.Fatalf("会议候选投影不正确：%+v", options)
	}
}

// newGroupService 使用真实 Repository 和固定基础设施组装单个测试服务。
func newGroupService(db *gorm.DB, transactions *database.TransactionManager, values ...string) *peopleservice.GroupService {
	return peopleservice.NewGroupService(peopleservice.GroupServiceDependencies{
		Repository:   peoplerepository.NewGroupRepository(db),
		Members:      peoplerepository.NewMemberRepository(db),
		Transactions: transactions,
		IDs:          identity.NewFixedGenerator(values...),
		Clock:        fixedTestClock(),
	})
}
