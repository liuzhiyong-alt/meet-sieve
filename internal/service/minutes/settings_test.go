package minutes

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"meet-sieve/internal/infra/clock"
	"meet-sieve/internal/infra/database"
	minutesrepository "meet-sieve/internal/repository/minutes"

	"gorm.io/gorm"
)

// TestSettingsService_DefaultSaveAndReset 验证默认、自定义和恢复默认的完整设置语义。
func TestSettingsService_DefaultSaveAndReset(t *testing.T) {
	db := openMinuteSettingsDatabase(t)
	service := NewSettingsService(
		minutesrepository.NewRepository(db, database.NewTransactionManager(db)),
		clock.NewFixed(time.UnixMilli(1234)),
	)

	view, err := service.Get(context.Background())
	if err != nil || view.Prompt != DefaultPrompt || !view.IsDefault {
		t.Fatalf("读取默认会议纪要要求失败：view=%#v err=%v", view, err)
	}

	view, err = service.Save(context.Background(), "突出决策和负责人")
	if err != nil || view.Prompt != "突出决策和负责人" || view.IsDefault || view.UpdatedAt != 1234 {
		t.Fatalf("保存会议纪要要求失败：view=%#v err=%v", view, err)
	}
	current, err := service.CurrentPrompt(context.Background())
	if err != nil || current != "突出决策和负责人" {
		t.Fatalf("生成链路未读取自定义要求：prompt=%q err=%v", current, err)
	}

	view, err = service.Save(context.Background(), "   ")
	if err != nil || view.Prompt != DefaultPrompt || !view.IsDefault {
		t.Fatalf("恢复默认会议纪要要求失败：view=%#v err=%v", view, err)
	}
	var storedPrompt *string
	if err := db.Raw("SELECT minute_prompt FROM settings WHERE singleton_key = 1").Scan(&storedPrompt).Error; err != nil || storedPrompt != nil {
		t.Fatalf("恢复默认后应保存 NULL：prompt=%v err=%v", storedPrompt, err)
	}
}

// openMinuteSettingsDatabase 创建带 settings singleton 的隔离数据库。
func openMinuteSettingsDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "minutes-settings.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移数据库失败：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开数据库失败：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	if err := db.Exec(`INSERT INTO settings (id, singleton_key, created_at, updated_at)
		VALUES ('14141414-1414-4414-8414-141414141414', 1, 0, 0)`).Error; err != nil {
		t.Fatalf("写入 settings singleton 失败：%v", err)
	}
	return db
}
