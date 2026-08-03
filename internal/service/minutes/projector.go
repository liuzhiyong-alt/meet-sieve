package minutes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"meet-sieve/internal/infra/filesystem"
	minutesrepository "meet-sieve/internal/repository/minutes"
	"meet-sieve/models"
)

// ProjectionState 描述 SQLite 当前版本到 Markdown 文件的独立投影状态。
type ProjectionState struct {
	State     string
	ErrorCode string
}

// MinuteProjector 从 SQLite current 版本重建固定纪要草稿文件。
type MinuteProjector struct {
	repository *minutesrepository.Repository
	workspace  string
	write      func(string, []byte, os.FileMode) error
	mu         sync.Mutex
	states     map[string]ProjectionState
}

// NewMinuteProjector 创建使用安全原子替换的纪要投影器。
func NewMinuteProjector(repository *minutesrepository.Repository, workspace string) *MinuteProjector {
	return &MinuteProjector{repository: repository, workspace: workspace, write: filesystem.WriteAtomic, states: make(map[string]ProjectionState)}
}

// Flush 从 SQLite 读取 current 并重建会议纪要草稿.md。
func (projector *MinuteProjector) Flush(ctx context.Context, meeting models.Meeting) error {
	if projector == nil || projector.repository == nil || projector.workspace == "" || meeting.ID == "" || meeting.RelativeDir == "" {
		return fmt.Errorf("刷新纪要投影：参数无效")
	}
	version, err := projector.repository.GetCurrent(ctx, meeting.ID)
	if err != nil {
		projector.setState(meeting.ID, ProjectionState{State: "failed", ErrorCode: "MINUTES_PROJECTION_FAILED"})
		return err
	}
	target, err := trustedMinutePath(projector.workspace, meeting.RelativeDir)
	if err != nil {
		projector.setState(meeting.ID, ProjectionState{State: "failed", ErrorCode: "MINUTES_PROJECTION_FAILED"})
		return err
	}
	content := renderVersionProjection(version)
	if err := projector.write(target, content, 0o600); err != nil {
		projector.setState(meeting.ID, ProjectionState{State: "failed", ErrorCode: "MINUTES_PROJECTION_FAILED"})
		return fmt.Errorf("写入纪要投影失败：%w", err)
	}
	projector.setState(meeting.ID, ProjectionState{State: "current"})
	return nil
}

// State 返回指定会议的安全投影状态。
func (projector *MinuteProjector) State(meetingID string) ProjectionState {
	if projector == nil {
		return ProjectionState{State: "idle"}
	}
	projector.mu.Lock()
	defer projector.mu.Unlock()
	state, exists := projector.states[meetingID]
	if !exists {
		return ProjectionState{State: "idle"}
	}
	return state
}

// setState 在短临界区内更新投影状态。
func (projector *MinuteProjector) setState(meetingID string, state ProjectionState) {
	projector.mu.Lock()
	projector.states[meetingID] = state
	projector.mu.Unlock()
}

// trustedMinutePath 拒绝绝对路径和上跳路径，只返回固定文件名。
func trustedMinutePath(workspace string, relativeDirectory string) (string, error) {
	if filepath.IsAbs(relativeDirectory) || strings.Contains(filepath.ToSlash(relativeDirectory), "../") {
		return "", fmt.Errorf("会议目录不可信")
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	directory, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(relativeDirectory)))
	if err != nil || (directory != root && !strings.HasPrefix(directory, root+string(filepath.Separator))) {
		return "", fmt.Errorf("会议目录逃逸工作目录")
	}
	return filepath.Join(directory, "会议纪要草稿.md"), nil
}

// renderVersionProjection 为不可变正文增加可审计版本头。
func renderVersionProjection(version models.MinuteVersion) []byte {
	label := "AI 草稿"
	if version.Source == "human" {
		label = "人工草稿"
	}
	if version.State == "confirmed" {
		label = "已确认"
	}
	header := fmt.Sprintf("<!-- MeetSieve 纪要投影：%s；版本 %d；创建时间 %s -->\n\n", label, version.VersionNo, time.UnixMilli(version.CreatedAt).UTC().Format(time.RFC3339))
	return []byte(header + strings.TrimLeft(version.ContentMarkdown, "\n"))
}
