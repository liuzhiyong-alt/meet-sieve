package people_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	"meet-sieve/internal/port"
	peoplerepository "meet-sieve/internal/repository/people"
	peopleservice "meet-sieve/internal/service/people"

	"gorm.io/gorm"
)

const createdMemberID = "11111111-1111-4111-8111-111111111111"

const duplicateMemberID = "22222222-2222-4222-8222-222222222222"

const archivedGroupID = "33333333-3333-4333-8333-333333333333"

const historicalMeetingID = "55555555-5555-4555-8555-555555555555"

// TestMemberService_CreatePersistsActiveMember 验证新增成员通过真实 SQLite 持久化并返回稳定投影。
func TestMemberService_CreatePersistsActiveMember(t *testing.T) {
	db := openPeopleDatabase(t)
	service := peopleservice.NewMemberService(peopleservice.MemberServiceDependencies{
		Repository:   peoplerepository.NewMemberRepository(db),
		Transactions: database.NewTransactionManager(db),
		IDs:          identity.NewFixedGenerator(createdMemberID),
		Clock:        clock.NewFixed(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)),
	})

	member, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{
		Name:  "\u3000Ｆｏｏ\u00a0BAR\u3000",
		Notes: "研发负责人",
	})
	if err != nil {
		t.Fatalf("新增成员失败：%v", err)
	}
	if member.ID != createdMemberID || member.Name != "Ｆｏｏ\u00a0BAR" || member.NameNormalized != "foo bar" {
		t.Fatalf("成员投影不正确：%+v", member)
	}
	if member.Notes == nil || *member.Notes != "研发负责人" || member.ArchivedAt != nil {
		t.Fatalf("成员可选字段不正确：%+v", member)
	}

	var persisted struct {
		ID             string
		NameNormalized string
		ArchivedAt     *int64
	}
	if err := db.Table("members").Select("id, name_normalized, archived_at").Where("id = ?", createdMemberID).Take(&persisted).Error; err != nil {
		t.Fatalf("读取持久化成员失败：%v", err)
	}
	if persisted.NameNormalized != "foo bar" || persisted.ArchivedAt != nil {
		t.Fatalf("持久化成员不正确：%+v", persisted)
	}
}

// TestMemberService_ListActiveMembersReportsCurrentModelReadiness 验证当前模型四元组完整时为 ready，缺失时需重建。
func TestMemberService_ListActiveMembersReportsCurrentModelReadiness(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	service := peopleservice.NewMemberService(peopleservice.MemberServiceDependencies{
		Repository: peoplerepository.NewMemberRepository(db), Transactions: transactions,
		IDs: identity.NewFixedGenerator(createdMemberID), Clock: clock.NewFixed(time.UnixMilli(1000)),
		VoiceModel: func() (port.ModelInfo, error) {
			return port.ModelInfo{ID: "campplus", Version: "1.0.0-ms1", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Dimension: 192}, nil
		},
	})
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "当前模型成员"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO voice_samples (
		id,member_id,relative_path,duration_ms,sample_rate,channels,bit_depth,size_bytes,sha256,source_kind,environment_kind,processing_state,quality_state,created_at,updated_at
	) VALUES ('77777777-7777-4777-8777-777777777777',?,'data/voice-samples/sample.wav',3000,16000,1,16,44,
	'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','imported','quiet','ready','accepted',1,1)`, createdMemberID).Error; err != nil {
		t.Fatalf("准备 accepted 样本失败：%v", err)
	}
	members, err := service.ListActiveMembers(context.Background())
	if err != nil || members[0].VoiceSummary.Readiness != "rebuild_required" {
		t.Fatalf("缺少当前向量时应要求重建：members=%+v err=%v", members, err)
	}
	if err := db.Exec(`INSERT INTO voice_embeddings (
		id,voice_sample_id,model_id,model_version,model_sha256,dimension,embedding,created_at,updated_at
	) VALUES ('88888888-8888-4888-8888-888888888888','77777777-7777-4777-8777-777777777777','campplus','1.0.0-ms1',
	'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',192,?,2,2)`, make([]byte, 192*4)).Error; err != nil {
		t.Fatalf("准备当前模型向量失败：%v", err)
	}
	members, err = service.ListActiveMembers(context.Background())
	if err != nil || members[0].VoiceSummary.Readiness != "ready" {
		t.Fatalf("当前向量完整时应 ready：members=%+v err=%v", members, err)
	}
}

// TestMemberService_CreateRejectsNormalizedDuplicate 验证活动成员的规范化重名映射为稳定业务错误。
func TestMemberService_CreateRejectsNormalizedDuplicate(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	firstService := newMemberService(db, transactions, createdMemberID)
	if _, err := firstService.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "Ｆｏｏ BAR"}); err != nil {
		t.Fatalf("准备首个成员失败：%v", err)
	}

	secondService := newMemberService(db, transactions, duplicateMemberID)
	_, err := secondService.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "foo\tbar"})
	if got := apperr.Normalize(err); got.ErrorCode != "MEMBER_NAME_CONFLICT" || got.Kind != apperr.KindBusiness {
		t.Fatalf("重名错误语义不正确：%+v", got)
	}
}

// TestMemberService_ListActiveMembersReturnsPersistedMembers 验证默认成员查询只读取活动成员的稳定投影。
func TestMemberService_ListActiveMembersReturnsPersistedMembers(t *testing.T) {
	db := openPeopleDatabase(t)
	service := newMemberService(db, database.NewTransactionManager(db), createdMemberID)
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "王小明"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}

	members, err := service.ListActiveMembers(context.Background())
	if err != nil {
		t.Fatalf("查询活动成员失败：%v", err)
	}
	if len(members) != 1 || members[0].ID != createdMemberID || members[0].ArchivedAt != nil {
		t.Fatalf("活动成员投影不正确：%+v", members)
	}
}

// TestMemberService_ListActiveMembersUsesConfirmedOrder 验证成员列表按创建时间倒序、同一时间按 ID 升序。
func TestMemberService_ListActiveMembersUsesConfirmedOrder(t *testing.T) {
	db := openPeopleDatabase(t)
	transactions := database.NewTransactionManager(db)
	older := peopleservice.NewMemberService(peopleservice.MemberServiceDependencies{
		Repository: peoplerepository.NewMemberRepository(db), Transactions: transactions,
		IDs:   identity.NewFixedGenerator(createdMemberID),
		Clock: clock.NewFixed(time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)),
	})
	if _, err := older.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "子成员"}); err != nil {
		t.Fatalf("准备较早成员失败：%v", err)
	}
	newer := newMemberService(db, transactions, duplicateMemberID)
	if _, err := newer.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "阿成员"}); err != nil {
		t.Fatalf("准备较新成员失败：%v", err)
	}

	members, err := newer.ListActiveMembers(context.Background())
	if err != nil {
		t.Fatalf("查询成员失败：%v", err)
	}
	if len(members) != 2 || members[0].ID != duplicateMemberID || members[1].ID != createdMemberID {
		t.Fatalf("成员列表顺序不正确：%+v", members)
	}
}

// TestMemberService_ListActiveMembersIncludesVoiceSummary 验证样本聚合进入成员投影且不伪造模型 ready。
func TestMemberService_ListActiveMembersIncludesVoiceSummary(t *testing.T) {
	db := openPeopleDatabase(t)
	service := newMemberService(db, database.NewTransactionManager(db), createdMemberID)
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "声纹成员"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO voice_samples (
		id, member_id, relative_path, duration_ms, sample_rate, channels, bit_depth, size_bytes, sha256,
		source_kind, environment_kind, processing_state, quality_state, created_at, updated_at
	) VALUES (
		'77777777-7777-4777-8777-777777777777', ?, 'data/voice-samples/sample.wav', 1000, 16000, 1, 16, 44,
		'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'imported', 'other', 'ready', 'accepted', 0, 0
	)`, createdMemberID).Error; err != nil {
		t.Fatalf("准备声纹样本失败：%v", err)
	}

	members, err := service.ListActiveMembers(context.Background())
	if err != nil {
		t.Fatalf("查询成员失败：%v", err)
	}
	if len(members) != 1 || members[0].VoiceSummary.AcceptedSampleCount != 1 || members[0].VoiceSummary.Readiness != "unavailable" {
		t.Fatalf("成员声纹汇总不正确：%+v", members)
	}
}

// TestMemberService_ArchiveRemovesCurrentGroupMemberships 验证归档成员时会在同一事务移除当前小组关系。
func TestMemberService_ArchiveRemovesCurrentGroupMemberships(t *testing.T) {
	db := openPeopleDatabase(t)
	service := newMemberService(db, database.NewTransactionManager(db), createdMemberID)
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "张三"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO groups (
		id, name, name_normalized, default_lan_enabled, created_at, updated_at
	) VALUES (?, '项目组', '项目组', 0, 0, 0)`, archivedGroupID).Error; err != nil {
		t.Fatalf("准备小组失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO group_members (
		id, group_id, member_id, sort_order, created_at, updated_at
	) VALUES ('44444444-4444-4444-8444-444444444444', ?, ?, 0, 0, 0)`, archivedGroupID, createdMemberID).Error; err != nil {
		t.Fatalf("准备小组成员关系失败：%v", err)
	}

	if err := service.ArchiveMember(context.Background(), createdMemberID); err != nil {
		t.Fatalf("归档成员失败：%v", err)
	}

	var archivedAt *int64
	if err := db.Table("members").Select("archived_at").Where("id = ?", createdMemberID).Scan(&archivedAt).Error; err != nil {
		t.Fatalf("读取归档成员失败：%v", err)
	}
	if archivedAt == nil {
		t.Fatal("成员必须被标记为归档")
	}
	var relationCount int64
	if err := db.Table("group_members").Where("member_id = ?", createdMemberID).Count(&relationCount).Error; err != nil {
		t.Fatalf("统计小组关系失败：%v", err)
	}
	if relationCount != 0 {
		t.Fatalf("归档成员不得保留当前小组关系：%d", relationCount)
	}
}

// TestMemberService_DeleteUnreferencedRemovesMember 验证没有历史引用的成员可被永久删除。
func TestMemberService_DeleteUnreferencedRemovesMember(t *testing.T) {
	db := openPeopleDatabase(t)
	service := newMemberService(db, database.NewTransactionManager(db), createdMemberID)
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "李四"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}

	if err := service.DeleteMember(context.Background(), createdMemberID); err != nil {
		t.Fatalf("删除未引用成员失败：%v", err)
	}
	var count int64
	if err := db.Table("members").Where("id = ?", createdMemberID).Count(&count).Error; err != nil {
		t.Fatalf("统计成员失败：%v", err)
	}
	if count != 0 {
		t.Fatalf("未引用成员必须被永久删除：count=%d", count)
	}
}

// TestMemberService_DeleteUnreferencedCleansVoiceFilesFirst 验证永久删除成员前先调用受控声纹文件清理。
func TestMemberService_DeleteUnreferencedCleansVoiceFilesFirst(t *testing.T) {
	db := openPeopleDatabase(t)
	cleaned := false
	service := peopleservice.NewMemberService(peopleservice.MemberServiceDependencies{
		Repository: peoplerepository.NewMemberRepository(db), Transactions: database.NewTransactionManager(db),
		IDs: identity.NewFixedGenerator(createdMemberID), Clock: clock.NewFixed(time.UnixMilli(1000)),
		DeleteVoiceSamples: func(_ context.Context, memberID string) error {
			cleaned = memberID == createdMemberID
			return nil
		},
	})
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "待删除成员"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	if err := service.DeleteMember(context.Background(), createdMemberID); err != nil {
		t.Fatalf("永久删除成员失败：%v", err)
	}
	if !cleaned {
		t.Fatal("永久删除成员前必须清理受控声纹文件")
	}
}

// TestMemberService_DeleteHistoricallyReferencedMemberReturnsArchiveOnlyError 验证历史会议引用成员只能归档。
func TestMemberService_DeleteHistoricallyReferencedMemberReturnsArchiveOnlyError(t *testing.T) {
	db := openPeopleDatabase(t)
	service := newMemberService(db, database.NewTransactionManager(db), createdMemberID)
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "王五"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}
	insertHistoricalMemberReference(t, db, createdMemberID)

	err := service.DeleteMember(context.Background(), createdMemberID)
	if got := apperr.Normalize(err); got.ErrorCode != "MEMBER_HISTORICALLY_REFERENCED" || got.Kind != apperr.KindBusiness {
		t.Fatalf("历史引用错误语义不正确：%+v", got)
	}
}

// TestMemberService_UpdateChangesOnlyEditableMemberFields 验证活动成员可以修改名称和备注。
func TestMemberService_UpdateChangesOnlyEditableMemberFields(t *testing.T) {
	db := openPeopleDatabase(t)
	service := newMemberService(db, database.NewTransactionManager(db), createdMemberID)
	if _, err := service.CreateMember(context.Background(), peopleservice.CreateMemberInput{Name: "旧名称", Notes: "旧备注"}); err != nil {
		t.Fatalf("准备成员失败：%v", err)
	}

	updated, err := service.UpdateMember(context.Background(), createdMemberID, peopleservice.UpdateMemberInput{
		Name:  "新名称",
		Notes: "新备注",
	})
	if err != nil {
		t.Fatalf("修改成员失败：%v", err)
	}
	if updated.Name != "新名称" || updated.NameNormalized != "新名称" || updated.Notes == nil || *updated.Notes != "新备注" {
		t.Fatalf("修改后的成员投影不正确：%+v", updated)
	}
}

// newMemberService 使用真实 Repository 和固定基础设施组装单个测试服务。
func newMemberService(db *gorm.DB, transactions *database.TransactionManager, memberID string) *peopleservice.MemberService {
	return peopleservice.NewMemberService(peopleservice.MemberServiceDependencies{
		Repository:   peoplerepository.NewMemberRepository(db),
		Transactions: transactions,
		IDs:          identity.NewFixedGenerator(memberID),
		Clock:        fixedTestClock(),
	})
}

// fixedTestClock 返回所有成员和小组集成测试共用的确定性时钟。
func fixedTestClock() *clock.Fixed {
	return clock.NewFixed(time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
}

// insertHistoricalMemberReference 写入最小会议参会者快照，模拟不可改写的历史成员引用。
func insertHistoricalMemberReference(t *testing.T, db *gorm.DB, memberID string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO meetings (
		id, meeting_no, subject, relative_dir, local_timezone,
		lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at
	) VALUES (
		?, 'MS-20260801-0001', '历史会议', 'meetings/history', 'Asia/Shanghai',
		'ended', 'saved', 'stopped', 'none', 'unchecked', 'not_generated', 'disabled', 0, 0
	)`, historicalMeetingID).Error; err != nil {
		t.Fatalf("准备历史会议失败：%v", err)
	}
	if err := db.Exec(`INSERT INTO meeting_participants (
		id, meeting_id, member_id, participant_kind, display_name_snapshot, sort_order, created_at, updated_at
	) VALUES ('66666666-6666-4666-8666-666666666666', ?, ?, 'member', '王五', 0, 0, 0)`, historicalMeetingID, memberID).Error; err != nil {
		t.Fatalf("准备历史成员引用失败：%v", err)
	}
}

// openPeopleDatabase 创建迁移到最新 schema 的独立 SQLite 数据库。
func openPeopleDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "people.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开 SQLite 失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}
