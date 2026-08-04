// Package diagnostics 提供只读存储扫描和白名单诊断导出。
package diagnostics

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"meet-sieve/internal/infra/apperr"
	"meet-sieve/internal/infra/filesystem"

	"gorm.io/gorm"
)

// StorageStage 是存储扫描可展示的固定阶段。
type StorageStage string

const (
	StageIdle              StorageStage = "idle"
	StageCheckingVolume    StorageStage = "checking-volume"
	StageScanningMeetings  StorageStage = "scanning-meetings"
	StageScanningDatabase  StorageStage = "scanning-database-backups"
	StageScanningDerived   StorageStage = "scanning-derived-temp"
	StageFinalizingSummary StorageStage = "finalizing-summary"
	StageCompleted         StorageStage = "completed"
	StageFailed            StorageStage = "failed"
)

// StorageCategories 保存真实文件字节分类。
type StorageCategories struct {
	Recordings      int64
	Attachments     int64
	DatabaseBackups int64
	DerivedTemp     int64
	Logs            int64
	VoiceModels     int64
}

// MeetingStorage 是 Top 20 会议占用的安全投影。
type MeetingStorage struct {
	MeetingID string
	MeetingNo string
	Subject   string
	Bytes     int64
}

// StorageSnapshot 是可从后台扫描恢复的不可变结果。
type StorageSnapshot struct {
	Stage          StorageStage
	Running        bool
	ScannedItems   int64
	TotalBytes     uint64
	AvailableBytes uint64
	WorkspaceBytes int64
	Categories     StorageCategories
	TopMeetings    []MeetingStorage
	Warnings       []string
	ScannedAt      int64
	ErrorCode      string
}

type meetingStorageFact struct {
	ID          string `gorm:"column:id"`
	MeetingNo   string `gorm:"column:meeting_no"`
	Subject     string `gorm:"column:subject"`
	RelativeDir string `gorm:"column:relative_dir"`
}

// StorageScanService 保证同进程只有一个只读扫描 owner。
type StorageScanService struct {
	mu        sync.Mutex
	reader    *gorm.DB
	workspace string
	logRoot   string
	modelRoot string
	state     StorageSnapshot
}

// NewStorageScanService 创建扫描服务；构造阶段不遍历目录。
func NewStorageScanService(reader *gorm.DB, workspace string, logRoot string, modelRoot string) *StorageScanService {
	return &StorageScanService{reader: reader, workspace: workspace, logRoot: logRoot, modelRoot: modelRoot, state: StorageSnapshot{Stage: StageIdle}}
}

// Start 启动一次后台扫描，已有扫描时返回稳定冲突。
func (service *StorageScanService) Start(ctx context.Context) (StorageSnapshot, error) {
	if service == nil || service.reader == nil || !filepath.IsAbs(service.workspace) {
		return StorageSnapshot{}, fmt.Errorf("存储扫描服务依赖无效")
	}
	service.mu.Lock()
	if service.state.Running {
		service.mu.Unlock()
		return StorageSnapshot{}, apperr.Biz(apperr.CodeStorageScanRunning, apperr.WithOp("diagnostics.storage.start"))
	}
	service.state = StorageSnapshot{Stage: StageCheckingVolume, Running: true}
	snapshot := cloneStorage(service.state)
	service.mu.Unlock()
	go service.run(ctx)
	return snapshot, nil
}

// Get 返回当前阶段或最近一次成功/失败结果的副本。
func (service *StorageScanService) Get() StorageSnapshot {
	if service == nil {
		return StorageSnapshot{Stage: StageFailed, ErrorCode: apperr.CodeStorageScanFailed.ErrorCode}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	return cloneStorage(service.state)
}

// run 执行真实容量读取、规范目录遍历和汇总。
func (service *StorageScanService) run(ctx context.Context) {
	result, err := service.scan(ctx)
	service.mu.Lock()
	defer service.mu.Unlock()
	if err != nil {
		service.state.Running = false
		service.state.Stage = StageFailed
		service.state.ErrorCode = apperr.CodeStorageScanFailed.ErrorCode
		return
	}
	result.Running = false
	result.Stage = StageCompleted
	result.ScannedAt = time.Now().UnixMilli()
	service.state = result
}

// scan 汇总工作目录、日志和模型目录；遍历不跟随符号链接。
func (service *StorageScanService) scan(ctx context.Context) (StorageSnapshot, error) {
	total, available, err := filesystem.VolumeBytes(service.workspace)
	if err != nil {
		return StorageSnapshot{}, err
	}
	result := StorageSnapshot{Stage: StageScanningMeetings, Running: true, TotalBytes: total, AvailableBytes: available}
	facts, err := service.loadMeetingFacts(ctx)
	if err != nil {
		return StorageSnapshot{}, err
	}
	meetingByDir := make(map[string]meetingStorageFact, len(facts))
	for _, fact := range facts {
		meetingByDir[filepath.Clean(filepath.FromSlash(fact.RelativeDir))] = fact
	}
	if err := scanTree(ctx, service.workspace, func(relative string, size int64, warning string) {
		result.ScannedItems++
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
			return
		}
		result.WorkspaceBytes += size
		classifyWorkspaceBytes(&result.Categories, relative, size)
		addMeetingBytes(&result.TopMeetings, meetingByDir, relative, size)
	}); err != nil {
		return StorageSnapshot{}, err
	}
	service.setStage(StageScanningDatabase, result.ScannedItems)
	result.Categories.Logs, _ = scanSize(ctx, service.logRoot, &result.ScannedItems, &result.Warnings)
	service.setStage(StageScanningDerived, result.ScannedItems)
	result.Categories.VoiceModels, _ = scanSize(ctx, service.modelRoot, &result.ScannedItems, &result.Warnings)
	service.setStage(StageFinalizingSummary, result.ScannedItems)
	sort.Slice(result.TopMeetings, func(left, right int) bool { return result.TopMeetings[left].Bytes > result.TopMeetings[right].Bytes })
	if len(result.TopMeetings) > 20 {
		result.TopMeetings = result.TopMeetings[:20]
	}
	return result, nil
}

// loadMeetingFacts 读取 Top 20 展示所需的最小会议元数据。
func (service *StorageScanService) loadMeetingFacts(ctx context.Context) ([]meetingStorageFact, error) {
	var facts []meetingStorageFact
	if err := service.reader.WithContext(ctx).Table("meetings").Select("id", "meeting_no", "subject", "relative_dir").Find(&facts).Error; err != nil {
		return nil, fmt.Errorf("读取会议存储关系失败: %w", err)
	}
	return facts, nil
}

// setStage 更新扫描阶段和已扫描项目数，不伪造百分比。
func (service *StorageScanService) setStage(stage StorageStage, scanned int64) {
	service.mu.Lock()
	service.state.Stage = stage
	service.state.ScannedItems = scanned
	service.mu.Unlock()
}

// scanTree 使用 WalkDir/Lstat 遍历普通文件，符号链接和特殊文件只产生安全 warning。
func scanTree(ctx context.Context, root string, visit func(string, int64, string)) error {
	if root == "" {
		return nil
	}
	if _, err := os.Lstat(root); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			visit(relative, 0, "发现符号链接，未跟随")
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			visit(relative, 0, "发现特殊文件，未读取")
			return nil
		}
		visit(relative, info.Size(), "")
		return nil
	})
}

// classifyWorkspaceBytes 按已确认规范目录分类真实字节。
func classifyWorkspaceBytes(categories *StorageCategories, relative string, size int64) {
	normalized := filepath.ToSlash(relative)
	switch {
	case strings.Contains(normalized, "/audio/") || strings.HasSuffix(normalized, "/audio"):
		categories.Recordings += size
	case strings.Contains(normalized, "/resources/"):
		categories.Attachments += size
	case strings.HasPrefix(normalized, "data/") || strings.HasPrefix(normalized, "backups/"):
		categories.DatabaseBackups += size
	default:
		categories.DerivedTemp += size
	}
}

// addMeetingBytes 按最长登记会议目录前缀聚合会议占用。
func addMeetingBytes(items *[]MeetingStorage, facts map[string]meetingStorageFact, relative string, size int64) {
	clean := filepath.Clean(relative)
	for directory, fact := range facts {
		if clean == directory || strings.HasPrefix(clean, directory+string(filepath.Separator)) {
			for index := range *items {
				if (*items)[index].MeetingID == fact.ID {
					(*items)[index].Bytes += size
					return
				}
			}
			*items = append(*items, MeetingStorage{MeetingID: fact.ID, MeetingNo: fact.MeetingNo, Subject: fact.Subject, Bytes: size})
			return
		}
	}
}

// scanSize 统计外部受控目录普通文件字节，不因目录不存在失败。
func scanSize(ctx context.Context, root string, scanned *int64, warnings *[]string) (int64, error) {
	var total int64
	err := scanTree(ctx, root, func(_ string, size int64, warning string) {
		*scanned++
		if warning != "" {
			*warnings = append(*warnings, warning)
			return
		}
		total += size
	})
	return total, err
}

// cloneStorage 深复制切片，避免 UI 或测试修改服务内部状态。
func cloneStorage(snapshot StorageSnapshot) StorageSnapshot {
	snapshot.TopMeetings = append([]MeetingStorage(nil), snapshot.TopMeetings...)
	snapshot.Warnings = append([]string(nil), snapshot.Warnings...)
	return snapshot
}
