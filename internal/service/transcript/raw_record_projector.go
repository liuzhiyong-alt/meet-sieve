package transcript

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/filesystem"
	transcriptrepository "meet-sieve/internal/repository/transcript"
	"meet-sieve/models"
)

// RawRecordProjectorDependencies 描述可重建 Markdown 投影的读取与写入边界。
type RawRecordProjectorDependencies struct {
	Repository  *transcriptrepository.Repository
	WriteAtomic func(path string, content []byte, permission os.FileMode) error
	ReadFile    func(string) ([]byte, error)
	// WorkspaceRoot 是已验证的会议工作目录，用于定位固定原始记录文件。
	WorkspaceRoot string
	// Schedule 注入可取消的防抖调度；生产环境默认使用 time.AfterFunc。
	Schedule func(delay time.Duration, task func()) func()
	// Debounce 是 dirty 合并窗口，生产固定为 2 秒。
	Debounce time.Duration
}

// RawRecordProjector 从 SQLite 事实重建会议原始记录，不保存内存作为事实来源。
type RawRecordProjector struct {
	repository  *transcriptrepository.Repository
	writeAtomic func(path string, content []byte, permission os.FileMode) error
	readFile    func(string) ([]byte, error)
	workspace   string
	schedule    func(delay time.Duration, task func()) func()
	debounce    time.Duration
	mu          sync.Mutex
	cancel      map[string]func()
	states      map[string]RawRecordProjectionState
	generation  map[string]uint64
}

// RawRecordProjectionState 是可安全展示的原始记录刷新状态，不包含路径或底层错误。
type RawRecordProjectionState struct {
	State     string
	ErrorCode string
}

// NewRawRecordProjector 创建原始记录投影器；默认使用安全原子文件替换。
func NewRawRecordProjector(dependencies RawRecordProjectorDependencies) *RawRecordProjector {
	writeAtomic := dependencies.WriteAtomic
	if writeAtomic == nil {
		writeAtomic = filesystem.WriteAtomic
	}
	readFile := dependencies.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	schedule := dependencies.Schedule
	if schedule == nil {
		schedule = scheduleAfter
	}
	debounce := dependencies.Debounce
	if debounce <= 0 {
		debounce = 2 * time.Second
	}
	return &RawRecordProjector{
		repository: dependencies.Repository, writeAtomic: writeAtomic, readFile: readFile,
		workspace: dependencies.WorkspaceRoot, schedule: schedule, debounce: debounce,
		cancel: make(map[string]func()), states: make(map[string]RawRecordProjectionState),
		generation: make(map[string]uint64),
	}
}

// MarkDirty 合并同一会议的连续事件，并在防抖窗口结束后从 SQLite 重建原始记录。
func (projector *RawRecordProjector) MarkDirty(meetingID string) error {
	if projector == nil || projector.repository == nil || projector.workspace == "" || projector.schedule == nil || meetingID == "" {
		return fmt.Errorf("标记原始记录刷新：依赖或参数无效")
	}
	projector.mu.Lock()
	if cancel := projector.cancel[meetingID]; cancel != nil {
		cancel()
	}
	projector.generation[meetingID]++
	generation := projector.generation[meetingID]
	projector.cancel[meetingID] = projector.schedule(projector.debounce, func() {
		projector.runScheduled(meetingID, generation)
	})
	projector.states[meetingID] = RawRecordProjectionState{State: "dirty"}
	projector.mu.Unlock()
	return nil
}

// Flush 取消待执行防抖，并同步把当前 SQLite 快照刷新到固定 Markdown 文件。
func (projector *RawRecordProjector) Flush(ctx context.Context, meetingID string) error {
	if projector == nil || projector.repository == nil || projector.workspace == "" || meetingID == "" {
		return fmt.Errorf("强制刷新原始记录：依赖或参数无效")
	}
	projector.mu.Lock()
	if cancel := projector.cancel[meetingID]; cancel != nil {
		cancel()
		delete(projector.cancel, meetingID)
	}
	projector.generation[meetingID]++
	projector.mu.Unlock()
	projector.setState(meetingID, RawRecordProjectionState{State: "writing"})
	err := projector.rebuildMeeting(ctx, meetingID)
	projector.finishRefresh(meetingID, err)
	return err
}

// runScheduled 执行一次后台刷新；失败保留旧文件，结束流程仍会通过 Flush 返回错误。
func (projector *RawRecordProjector) runScheduled(meetingID string, generation uint64) {
	projector.mu.Lock()
	if projector.generation[meetingID] != generation {
		projector.mu.Unlock()
		return
	}
	delete(projector.cancel, meetingID)
	projector.mu.Unlock()
	projector.setState(meetingID, RawRecordProjectionState{State: "writing"})
	projector.finishRefresh(meetingID, projector.rebuildMeeting(context.Background(), meetingID))
}

// State 返回指定会议当前的安全投影状态。
func (projector *RawRecordProjector) State(meetingID string) RawRecordProjectionState {
	if projector == nil || meetingID == "" {
		return RawRecordProjectionState{State: "idle"}
	}
	projector.mu.Lock()
	defer projector.mu.Unlock()
	state, exists := projector.states[meetingID]
	if !exists {
		return RawRecordProjectionState{State: "idle"}
	}
	return state
}

// finishRefresh 只记录稳定错误码，底层文件错误仍由同步 Flush 返回给结束流程。
func (projector *RawRecordProjector) finishRefresh(meetingID string, err error) {
	if err != nil {
		projector.setState(meetingID, RawRecordProjectionState{State: "failed", ErrorCode: apperr.CodeRawRecordRefreshFailed.ErrorCode})
		return
	}
	projector.setState(meetingID, RawRecordProjectionState{State: "current"})
}

// setState 在短临界区内更新状态，不持锁执行文件 I/O。
func (projector *RawRecordProjector) setState(meetingID string, state RawRecordProjectionState) {
	projector.mu.Lock()
	projector.states[meetingID] = state
	projector.mu.Unlock()
}

// rebuildMeeting 从 repository 读取会议快照并定位固定目标文件。
func (projector *RawRecordProjector) rebuildMeeting(ctx context.Context, meetingID string) error {
	meeting, err := projector.repository.GetMeetingSnapshot(ctx, meetingID)
	if err != nil {
		return err
	}
	return projector.Rebuild(ctx, meeting, DefaultRawRecordPath(projector.workspace, meeting))
}

// scheduleAfter 使用标准 timer 返回幂等取消函数。
func scheduleAfter(delay time.Duration, task func()) func() {
	timer := time.AfterFunc(delay, task)
	return func() { timer.Stop() }
}

// Rebuild 从当前 SQLite 快照生成目标文件；内容无变化时不替换旧文件。
func (projector *RawRecordProjector) Rebuild(ctx context.Context, meeting models.Meeting, targetPath string) error {
	if projector == nil || projector.repository == nil || projector.writeAtomic == nil || projector.readFile == nil || meeting.ID == "" || meeting.StartedAt == nil || targetPath == "" {
		return fmt.Errorf("原始记录投影器依赖或输入无效")
	}
	content, err := projector.render(ctx, meeting)
	if err != nil {
		return apperr.Dependency(apperr.CodeRawRecordRefreshFailed, err, apperr.WithOp("transcript.raw_record.render"))
	}
	previous, readErr := projector.readFile(targetPath)
	if readErr == nil && string(previous) == string(content) {
		return nil
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return apperr.Dependency(apperr.CodeRawRecordRefreshFailed, readErr, apperr.WithOp("transcript.raw_record.read"))
	}
	if err = projector.writeAtomic(targetPath, content, 0o600); err != nil {
		return apperr.Dependency(apperr.CodeRawRecordRefreshFailed, err, apperr.WithOp("transcript.raw_record.write"))
	}
	return nil
}

// DefaultRawRecordPath 返回会议目录中固定且不依赖用户输入的原始记录文件路径。
func DefaultRawRecordPath(workspaceRoot string, meeting models.Meeting) string {
	return filepath.Join(workspaceRoot, filepath.FromSlash(meeting.RelativeDir), "会议原始记录.md")
}

// render 读取仅包含 Step 4 实体的事件快照并转换为固定模板输入。
func (projector *RawRecordProjector) render(ctx context.Context, meeting models.Meeting) ([]byte, error) {
	rows, err := projector.repository.LoadRawRecordRows(ctx, meeting.ID)
	if err != nil {
		return nil, err
	}
	sessions, err := projector.repository.LoadSessions(ctx, meeting.ID)
	if err != nil {
		return nil, err
	}
	hasCorrections, err := projector.repository.HasCorrections(ctx, meeting.ID)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(meeting.LocalTimezone)
	if err != nil {
		return nil, fmt.Errorf("加载会议时区失败：%w", err)
	}
	entries, gapCount := convertRawRecordRows(rows, sessions)
	return RenderRawRecord(RawRecordInput{Subject: meeting.Subject, MeetingNo: meeting.MeetingNo, StartedAt: time.UnixMilli(*meeting.StartedAt).In(location), RealtimeState: rawRecordStateText(meeting.RealtimeASRState), GapCount: gapCount, HasCorrections: hasCorrections, Entries: entries})
}

// convertRawRecordRows 以 session 开始顺序匿名编号；gap 无 session 也不会泄漏内部标识。
func convertRawRecordRows(rows []transcriptrepository.RawRecordRow, sessions []models.ASRSession) ([]RawRecordEntry, int) {
	sessionOrders := make(map[string]int, len(sessions))
	for index, session := range sessions {
		sessionOrders[session.ID] = index + 1
	}
	entries := make([]RawRecordEntry, 0, len(rows))
	gapCount := 0
	for _, row := range rows {
		entry := RawRecordEntry{
			Seq: row.Seq, Kind: rawRecordKind(row), OccurredAt: row.OccurredAt,
			StartSample: row.StartSample, EndSample: row.EndSample,
			Text: row.CurrentText, Speaker: resolveRawRecordSpeaker(row), SessionOrder: sessionOrders[row.ASRSessionID],
			DisplayName: row.GuestDisplayName, URL: row.SourceURL, OriginalName: row.OriginalName,
			MediaType: row.MediaType, SizeBytes: row.SizeBytes, SHA256: row.SHA256, Description: row.Description,
		}
		if strings.HasPrefix(row.Kind, "ai.") {
			entry.Text = row.AgentText
		}
		if row.Kind == "asr.gap" {
			gapCount++
		}
		entries = append(entries, entry)
	}
	return entries, gapCount
}

// rawRecordKind 把 resource.created 根据已完成实体字段区分为链接或附件。
func rawRecordKind(row transcriptrepository.RawRecordRow) string {
	if row.Kind != "resource.created" {
		return row.Kind
	}
	if row.SourceURL != "" {
		return "resource.link"
	}
	if row.OriginalName != "" {
		return "resource.attachment"
	}
	return "resource.incomplete"
}

// resolveRawRecordSpeaker 按当前 participant、unknown cluster、session fallback 的优先级返回展示名。
func resolveRawRecordSpeaker(row transcriptrepository.RawRecordRow) string {
	if strings.TrimSpace(row.ParticipantDisplayName) != "" {
		return row.ParticipantDisplayName
	}
	if row.ClusterDisplayNo > 0 {
		return fmt.Sprintf("未知说话人 %d", row.ClusterDisplayNo)
	}
	return ""
}

// rawRecordStateText 避免把内部状态码直接暴露到用户原始记录。
func rawRecordStateText(state string) string {
	switch state {
	case "streaming":
		return "进行中"
	case "stopped":
		return "已停止"
	case "unavailable":
		return "不可用"
	case "reconnecting":
		return "正在重连"
	case "connecting":
		return "正在连接"
	default:
		return "未启用"
	}
}
