package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"meet-sieve/internal/app/buildinfo"
	"meet-sieve/internal/app/health"
	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/database"
	"meet-sieve/internal/infra/filesystem"

	"gorm.io/gorm"
)

const (
	diagnosticSchema = 1
	redactorVersion  = "1"
	maximumLogBytes  = int64(10 << 20)
)

var (
	exportCredential  = regexp.MustCompile(`(?i)(password|passwd|token|secret|authorization|api[_-]?key)(\s*[:=]\s*)([^\s,;\"]+)`)
	exportUnixPath    = regexp.MustCompile(`/[^\s\"']+(?:/[^\s\"']+)+`)
	exportWindowsPath = regexp.MustCompile(`(?i)[a-z]:\\[^\s\"']+`)
	exportHash        = regexp.MustCompile(`\b[0-9a-fA-F]{32,128}\b`)
	exportUUID        = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F-]{27,}\b`)
)

// TaskSummary 是诊断允许输出的无参数后台任务摘要。
type TaskSummary struct {
	Kind  string `json:"kind"`
	State string `json:"state"`
	Count int    `json:"count"`
}

// ExportDependencies 是诊断收集所需的显式白名单来源。
type ExportDependencies struct {
	Reader        *gorm.DB
	Health        *health.Registry
	WorkspaceRoot string
	LogRoot       string
	Tasks         func(context.Context) []TaskSummary
	Now           func() time.Time
}

// ExportService 创建固定条目、二次脱敏且原子提交的诊断 ZIP。
type ExportService struct {
	reader    *gorm.DB
	health    *health.Registry
	workspace string
	logRoot   string
	tasks     func(context.Context) []TaskSummary
	now       func() time.Time
}

// ExportResult 是不暴露目标绝对路径的导出结果。
type ExportResult struct {
	FileName  string
	SizeBytes int64
	SHA256    string
	Entries   []string
}

// NewExportService 创建诊断导出服务。
func NewExportService(dependencies ExportDependencies) *ExportService {
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	return &ExportService{reader: dependencies.Reader, health: dependencies.Health, workspace: dependencies.WorkspaceRoot, logRoot: dependencies.LogRoot, tasks: dependencies.Tasks, now: now}
}

// ExportGlobal 导出不含业务正文的全局诊断包。
func (service *ExportService) ExportGlobal(ctx context.Context, targetPath string) (ExportResult, error) {
	return service.export(ctx, targetPath, "")
}

// ExportMeeting 在全局白名单上增加本场状态与 seq/文件大小摘要。
func (service *ExportService) ExportMeeting(ctx context.Context, meetingID string, targetPath string) (ExportResult, error) {
	if meetingID == "" {
		return ExportResult{}, apperr.Biz(apperr.CodeInvalidRequest, apperr.WithOp("diagnostics.export.meeting.validate"))
	}
	return service.export(ctx, targetPath, meetingID)
}

// export 收集固定条目、写入 ZIP 并通过同目录临时文件原子提交。
func (service *ExportService) export(ctx context.Context, targetPath string, meetingID string) (ExportResult, error) {
	if err := service.validateTarget(targetPath); err != nil {
		return ExportResult{}, err
	}
	entries, err := service.collect(ctx, meetingID)
	if err != nil {
		return ExportResult{}, err
	}
	data, names, err := buildDiagnosticZIP(entries, service.now())
	if err != nil {
		return ExportResult{}, apperr.Sys(err, apperr.WithOp("diagnostics.export.zip"))
	}
	if err := filesystem.WriteAtomic(targetPath, data, 0o600); err != nil {
		return ExportResult{}, apperr.Sys(err, apperr.WithOp("diagnostics.export.commit"))
	}
	sum := sha256.Sum256(data)
	return ExportResult{FileName: filepath.Base(targetPath), SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), Entries: names}, nil
}

// collect 只读取明确登记的系统、健康、空间、任务、日志和可选会议摘要。
func (service *ExportService) collect(ctx context.Context, meetingID string) (map[string][]byte, error) {
	total, available, err := filesystem.VolumeBytes(service.workspace)
	if err != nil {
		return nil, err
	}
	entries := make(map[string][]byte)
	entries["system.json"] = mustJSON(map[string]any{"version": buildinfo.Current().Version, "platform": runtime.GOOS, "architecture": runtime.GOARCH, "schema": database.CurrentSchemaVersion})
	healthSnapshot := health.Snapshot{}
	if service.health != nil {
		healthSnapshot = service.health.Get()
	}
	entries["health.json"] = mustJSON(map[string]any{"status": healthSnapshot.Status, "error_code": healthSnapshot.ErrorCode})
	entries["workspace.json"] = mustJSON(map[string]any{"writable": isWritable(service.workspace), "volume_total_bytes": total, "volume_available_bytes": available})
	tasks := []TaskSummary{}
	if service.tasks != nil {
		tasks = service.tasks(ctx)
	}
	entries["tasks.json"] = mustJSON(tasks)
	for name, content := range service.collectLogs(ctx) {
		entries[name] = content
	}
	if meetingID != "" {
		meeting, err := service.meetingSummary(ctx, meetingID)
		if err != nil {
			return nil, err
		}
		entries["meeting.json"] = mustJSON(meeting)
	}
	return entries, nil
}

// collectLogs 只读取最近七天直接子级 jsonl，并对每行再次脱敏。
func (service *ExportService) collectLogs(ctx context.Context) map[string][]byte {
	result := make(map[string][]byte)
	if service.logRoot == "" {
		return result
	}
	entries, err := os.ReadDir(service.logRoot)
	if err != nil {
		return result
	}
	cutoff := service.now().Add(-7 * 24 * time.Hour)
	for index, entry := range entries {
		if ctx.Err() != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().Before(cutoff) || info.Size() > maximumLogBytes {
			continue
		}
		content, err := os.ReadFile(filepath.Join(service.logRoot, entry.Name()))
		if err != nil {
			continue
		}
		result[fmt.Sprintf("logs/%03d.jsonl", index+1)] = []byte(redactExport(string(content)))
	}
	return result
}

// meetingSummary 读取严格白名单状态、seq 范围和文件聚合，不读取正文。
func (service *ExportService) meetingSummary(ctx context.Context, meetingID string) (map[string]any, error) {
	var state struct {
		ID, LifecycleState, LocalSaveState, RealtimeASRState, GapState, AgentState, MinuteState, LANState string
	}
	err := service.reader.WithContext(ctx).Table("meetings").Select("id", "lifecycle_state", "local_save_state", "realtime_asr_state", "gap_state", "agent_state", "minute_state", "lan_state").Where("id = ?", meetingID).Take(&state).Error
	if err != nil {
		return nil, apperr.Biz(apperr.CodeMeetingNotFound, apperr.WithOp("diagnostics.export.meeting"))
	}
	var seq struct{ MinSeq, MaxSeq *int64 }
	_ = service.reader.WithContext(ctx).Table("meeting_events").Select("MIN(seq) AS min_seq", "MAX(seq) AS max_seq").Where("meeting_id = ?", meetingID).Scan(&seq).Error
	var files struct {
		Count int64
		Bytes int64
	}
	_ = service.reader.WithContext(ctx).Table("audio_assets").Select("COUNT(*) AS count", "COALESCE(SUM(size_bytes), 0) AS bytes").Where("meeting_id = ?", meetingID).Scan(&files).Error
	return map[string]any{"meeting_id": state.ID, "lifecycle_state": state.LifecycleState, "local_save_state": state.LocalSaveState, "realtime_asr_state": state.RealtimeASRState, "gap_state": state.GapState, "agent_state": state.AgentState, "minute_state": state.MinuteState, "lan_state": state.LANState, "seq_min": seq.MinSeq, "seq_max": seq.MaxSeq, "file_count": files.Count, "file_bytes": files.Bytes}, nil
}

// validateTarget 只接受已存在父目录中的 zip 目标；系统对话框负责覆盖确认。
func (service *ExportService) validateTarget(targetPath string) error {
	if service == nil || service.reader == nil || !filepath.IsAbs(service.workspace) || !filepath.IsAbs(targetPath) || strings.ToLower(filepath.Ext(targetPath)) != ".zip" {
		return apperr.Biz(apperr.CodeDiagnosticTargetInvalid, apperr.WithOp("diagnostics.export.target"))
	}
	if info, err := os.Stat(filepath.Dir(targetPath)); err != nil || !info.IsDir() {
		return apperr.Biz(apperr.CodeDiagnosticTargetInvalid, apperr.WithOp("diagnostics.export.parent"))
	}
	return nil
}

// buildDiagnosticZIP 生成固定 manifest 和排序后的白名单条目。
func buildDiagnosticZIP(entries map[string][]byte, now time.Time) ([]byte, []string, error) {
	names := make([]string, 0, len(entries))
	hashes := make(map[string]string, len(entries))
	for name, content := range entries {
		names = append(names, name)
		sum := sha256.Sum256(content)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	sort.Strings(names)
	manifest := mustJSON(map[string]any{"schema": diagnosticSchema, "generated_at": now.UnixMilli(), "redactor_version": redactorVersion, "entries": hashes})
	allNames := append([]string{"manifest.json"}, names...)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range allNames {
		content := entries[name]
		if name == "manifest.json" {
			content = manifest
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Unix(0, 0).UTC())
		file, err := writer.CreateHeader(header)
		if err != nil {
			return nil, nil, err
		}
		if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
			return nil, nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, nil, err
	}
	return buffer.Bytes(), allNames, nil
}

// redactExport 对日志执行凭据、路径、长哈希和 UUID 二次脱敏。
func redactExport(value string) string {
	value = exportCredential.ReplaceAllString(value, `${1}${2}[REDACTED]`)
	value = exportUnixPath.ReplaceAllString(value, "[PATH]")
	value = exportWindowsPath.ReplaceAllString(value, "[PATH]")
	value = exportHash.ReplaceAllString(value, "[HASH]")
	return exportUUID.ReplaceAllString(value, "[ID]")
}

// isWritable 使用当前目录权限和真实临时文件验证可写性。
func isWritable(path string) bool {
	file, err := os.CreateTemp(path, ".diagnostic-write-check-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

// mustJSON 序列化内部固定白名单结构。
func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
