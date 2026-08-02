package wails_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/identity"
	infraLogger "meet-sieve/internal/infra/logger"
	peoplerepository "meet-sieve/internal/repository/people"
	peopleservice "meet-sieve/internal/service/people"
	wailstransport "meet-sieve/internal/transport/wails"

	"gorm.io/gorm"
)

// TestPeopleBinding_CreateAndListMemberDTO 验证成员写入与列表使用稳定 Wails DTO。
func TestPeopleBinding_CreateAndListMemberDTO(t *testing.T) {
	db := openPeopleBindingDatabase(t)
	transactions := database.NewTransactionManager(db)
	binding := wailstransport.NewPeopleBinding(func() (*peopleservice.MemberService, *peopleservice.GroupService, error) {
		members := peoplerepository.NewMemberRepository(db)
		return peopleservice.NewMemberService(peopleservice.MemberServiceDependencies{
				Repository: members, Transactions: transactions,
				IDs:   identity.NewFixedGenerator("11111111-1111-4111-8111-111111111111"),
				Clock: clock.NewFixed(time.UnixMilli(1000)),
			}), peopleservice.NewGroupService(peopleservice.GroupServiceDependencies{
				Repository: peoplerepository.NewGroupRepository(db), Members: members, Transactions: transactions,
				IDs: identity.NewFixedGenerator(), Clock: clock.NewFixed(time.UnixMilli(1000)),
			}), nil
	}, wailstransport.NewBoundary(infraLogger.NewNop()))

	created := binding.CreateMember(wailstransport.CreateMemberDTO{Name: " 张三 ", Notes: "主持人"})
	if created.Code != 200 || created.Data == nil || created.Data.Name != "张三" || created.Data.Notes == nil {
		t.Fatalf("创建成员 DTO 不正确：%+v", created)
	}
	listed := binding.ListMembers()
	if listed.Code != 200 || listed.Data == nil || len(*listed.Data) != 1 || (*listed.Data)[0].ID != created.Data.ID {
		t.Fatalf("成员列表 DTO 不正确：%+v", listed)
	}
	loaded := binding.GetMember(created.Data.ID)
	if loaded.Code != 200 || loaded.Data == nil || loaded.Data.Name != created.Data.Name {
		t.Fatalf("成员详情 DTO 不正确：%+v", loaded)
	}
}

// openPeopleBindingDatabase 创建最新 schema 的 Wails 契约测试数据库。
func openPeopleBindingDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "people-binding.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("执行 migration 失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db.WithContext(context.Background())
}
