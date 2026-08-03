package minutes_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"meet-sieve/internal/infra/database"
	minutesrepository "meet-sieve/internal/repository/minutes"
	"meet-sieve/models"

	"gorm.io/gorm"
)

const (
	meetingID = "81818181-8181-4818-8818-818181818181"
	sessionID = "82828282-8282-4828-8828-828282828282"
)

// TestRepository_AIAndHumanCurrentRules 验证 AI 候选不能覆盖人工 current。
func TestRepository_AIAndHumanCurrentRules(t *testing.T) {
	db, repository := openMinutesDatabase(t)
	ctx := context.Background()
	firstTurn := beginTurn(t, repository, "83838383-8383-4838-8838-838383838383", "minutes:first")
	first := commitAI(t, repository, firstTurn, "84848484-8484-4848-8848-848484848484", "第一版")
	if !first.IsCurrent || first.VersionNo != 1 {
		t.Fatalf("首个 AI 版本应成为 current：%#v", first)
	}

	human, err := repository.SaveHumanMinute(ctx, minutesrepository.SaveHumanMinuteInput{
		VersionID: "85858585-8585-4858-8858-858585858585", MeetingID: meetingID,
		BaseVersionID: first.ID, ContentMarkdown: "人工版", UpdatedAt: 30,
	})
	if err != nil || !human.IsCurrent || human.Source != "human" || human.ParentVersionID == nil || *human.ParentVersionID != first.ID {
		t.Fatalf("人工版本提交错误：version=%#v err=%v", human, err)
	}

	secondTurn := beginTurn(t, repository, "86868686-8686-4868-8868-868686868686", "minutes:second")
	candidate := commitAI(t, repository, secondTurn, "87878787-8787-4878-8878-878787878787", "第二版 AI")
	if candidate.IsCurrent {
		t.Fatal("人工 current 存在时新 AI 版本只能成为候选")
	}
	current, err := repository.GetCurrent(ctx, meetingID)
	if err != nil || current.ID != human.ID {
		t.Fatalf("人工 current 被错误替换：current=%#v err=%v", current, err)
	}
	var meeting models.Meeting
	if err := db.Where("id = ?", meetingID).Take(&meeting).Error; err != nil || meeting.MinuteState != "draft" {
		t.Fatalf("候选 AI 存在时 minute_state 应为 draft：meeting=%#v err=%v", meeting, err)
	}
}

// TestRepository_ConfirmAndRestoreKeepHistoryImmutable 验证确认不新建版本，恢复只复制历史。
func TestRepository_ConfirmAndRestoreKeepHistoryImmutable(t *testing.T) {
	db, repository := openMinutesDatabase(t)
	ctx := context.Background()
	turn := beginTurn(t, repository, "88888888-8888-4888-8888-888888888888", "minutes:confirm")
	first := commitAI(t, repository, turn, "89898989-8989-4898-8898-898989898989", "不可变正文")
	if err := repository.ConfirmCurrentMinute(ctx, minutesrepository.ConfirmMinuteInput{
		MeetingID: meetingID, VersionID: first.ID, ConfirmedAt: 40,
	}); err != nil {
		t.Fatalf("确认 current 失败：%v", err)
	}
	// 重复确认应幂等，不能创建新版本。
	if err := repository.ConfirmCurrentMinute(ctx, minutesrepository.ConfirmMinuteInput{
		MeetingID: meetingID, VersionID: first.ID, ConfirmedAt: 41,
	}); err != nil {
		t.Fatalf("重复确认应幂等：%v", err)
	}
	restored, err := repository.RestoreMinuteVersion(ctx, minutesrepository.RestoreMinuteInput{
		VersionID: "90909090-9090-4909-8909-909090909090", MeetingID: meetingID,
		SourceVersionID: first.ID, UpdatedAt: 50,
	})
	if err != nil || restored.Source != "restored" || !restored.IsCurrent || restored.State != "confirmed" {
		t.Fatalf("恢复历史失败：version=%#v err=%v", restored, err)
	}
	var original models.MinuteVersion
	if err := db.Where("id = ?", first.ID).Take(&original).Error; err != nil || original.ContentMarkdown != "不可变正文" || original.IsCurrent {
		t.Fatalf("历史版本被修改：version=%#v err=%v", original, err)
	}
}

// TestRepository_ConcurrentHumanVersionsHaveSingleWinner 验证版本号/current 切换不会并发分叉。
func TestRepository_ConcurrentHumanVersionsHaveSingleWinner(t *testing.T) {
	_, repository := openMinutesDatabase(t)
	turn := beginTurn(t, repository, "91919191-9191-4919-8919-919191919191", "minutes:race")
	base := commitAI(t, repository, turn, "92929292-9292-4929-8929-929292929292", "基础")
	inputs := []minutesrepository.SaveHumanMinuteInput{
		{VersionID: "93939393-9393-4939-8939-939393939393", MeetingID: meetingID, BaseVersionID: base.ID, ContentMarkdown: "甲", UpdatedAt: 60},
		{VersionID: "94949494-9494-4949-8949-949494949494", MeetingID: meetingID, BaseVersionID: base.ID, ContentMarkdown: "乙", UpdatedAt: 60},
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, input := range inputs {
		input := input
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := repository.SaveHumanMinute(context.Background(), input)
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, minutesrepository.ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("并发保存返回非预期错误：%v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("并发保存结果错误：success=%d conflict=%d", successes, conflicts)
	}
}

// TestRepository_FailMinutesTurnPreservesPreviousVersion 验证生成失败不会创建或覆盖版本。
func TestRepository_FailMinutesTurnPreservesPreviousVersion(t *testing.T) {
	db, repository := openMinutesDatabase(t)
	turn := beginTurn(t, repository, "95959595-9595-4959-8959-959595959595", "minutes:fail")
	if err := repository.FailMinutesTurn(context.Background(), minutesrepository.FailMinutesTurnInput{
		TurnID: turn.ID, State: "failed", ErrorCode: "MINUTES_PROVIDER_FAILED", UpdatedAt: 70,
	}); err != nil {
		t.Fatalf("收敛失败 turn：%v", err)
	}
	var count int64
	if err := db.Model(&models.MinuteVersion{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("失败生成不应创建版本：count=%d err=%v", count, err)
	}
}

// beginTurn 创建一个合法的 minutes turn。
func beginTurn(t *testing.T, repository *minutesrepository.Repository, id string, key string) models.AgentTurn {
	t.Helper()
	turn := models.AgentTurn{ID: id, MeetingID: meetingID, AgentSessionID: sessionID, Kind: "minutes", State: "pending", IdempotencyKey: key, CreatedAt: 10, UpdatedAt: 10}
	result, err := repository.BeginMinutesTurn(context.Background(), turn)
	if err != nil {
		t.Fatalf("开始 minutes turn：%v", err)
	}
	return result.Turn
}

// commitAI 把测试 turn 切到 running 后提交 AI 版本。
func commitAI(t *testing.T, repository *minutesrepository.Repository, turn models.AgentTurn, versionID string, content string) models.MinuteVersion {
	t.Helper()
	if err := repository.MarkMinutesTurnRunning(context.Background(), turn.ID, "provider-"+turn.ID, 20); err != nil {
		t.Fatalf("启动 minutes turn：%v", err)
	}
	version, err := repository.CommitGeneratedMinute(context.Background(), minutesrepository.CommitGeneratedMinuteInput{
		VersionID: versionID, TurnID: turn.ID, ProviderTurnID: "provider-" + turn.ID,
		ContentMarkdown: content, UpdatedAt: 25,
	})
	if err != nil {
		t.Fatalf("提交 AI 纪要：%v", err)
	}
	return version
}

// openMinutesDatabase 创建具备 ended/saved 会议信息的隔离数据库。
func openMinutesDatabase(t *testing.T) (*gorm.DB, *minutesrepository.Repository) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "minutes.db")
	if err := database.Migrate(path); err != nil {
		t.Fatalf("迁移纪要数据库：%v", err)
	}
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("打开纪要数据库：%v", err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	statements := []string{
		`INSERT INTO meetings (id, meeting_no, subject, relative_dir, local_timezone, started_at, ended_at, lifecycle_state, local_save_state, realtime_asr_state, gap_state, agent_state, minute_state, lan_state, created_at, updated_at) VALUES ('81818181-8181-4818-8818-818181818181', 'MS-1', '纪要', 'meetings/m', 'Asia/Shanghai', 0, 1, 'ended', 'saved', 'stopped', 'none', 'available', 'not_generated', 'stopped', 0, 1)`,
		`INSERT INTO agent_sessions (id, meeting_id, provider, thread_id, cwd_relative_path, state, started_at, created_at, updated_at) VALUES ('82828282-8282-4828-8828-828282828282', '81818181-8181-4818-8818-818181818181', 'codex', 'thread-1', 'meetings/m', 'available', 0, 0, 0)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("准备纪要测试事实：%v", err)
		}
	}
	return db, minutesrepository.NewRepository(db, database.NewTransactionManager(db))
}
